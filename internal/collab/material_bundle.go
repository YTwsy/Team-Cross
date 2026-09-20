package collab

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"teamcross/internal/materialstore"
)

func truncateMaterialText(text string) (string, int, int) {
	sourceLength := materialstore.UTF16Length(text)
	if len([]byte(text)) <= materialstore.MaxBlobBytes {
		return text, 0, 0
	}
	// Preserve both the beginning (command and setup) and the end (errors and
	// summaries). The marker is part of the immutable stored body; omittedLength
	// describes only source text that is not present in the stored body.
	marker := "\n\n[发布时省略过长内容]\n\n"
	available := materialstore.MaxBlobBytes - len(marker) - 64
	headBudget := available / 2
	tailBudget := available - headBudget
	headEnd := headBudget
	for headEnd > 0 && !utf8.RuneStart(text[headEnd]) {
		headEnd--
	}
	tailStart := len(text) - tailBudget
	for tailStart < len(text) && !utf8.RuneStart(text[tailStart]) {
		tailStart++
	}
	omitted := 0
	for {
		omitted = materialstore.UTF16Length(text[headEnd:tailStart])
		marker = fmt.Sprintf("\n\n[发布时省略 %d 个 UTF-16 字符]\n\n", omitted)
		if len([]byte(text[:headEnd]))+len(marker)+len([]byte(text[tailStart:])) <= materialstore.MaxBlobBytes || tailStart >= len(text) {
			break
		}
		tailStart++
		for tailStart < len(text) && !utf8.RuneStart(text[tailStart]) {
			tailStart++
		}
	}
	return text[:headEnd] + marker + text[tailStart:], sourceLength, omitted
}

func appendMaterialNotice(current, addition string) string {
	if strings.TrimSpace(current) == "" {
		return addition
	}
	return strings.TrimSpace(current) + "；" + addition
}

func validateMaterial(c MaterialContent) error {
	_, _, _, err := buildMaterialBundle(c)
	return err
}

func materialTurnLabel(turn MaterialTurn) string {
	label := shortText(turn.ID, 99)
	for _, item := range turn.Items {
		if strings.TrimSpace(item.Text) != "" {
			label = shortText(item.Text, 99)
			break
		}
	}
	for _, item := range turn.Items {
		if item.Type == "userMessage" && strings.TrimSpace(item.Text) != "" {
			label = shortText(item.Text, 99)
			break
		}
	}
	return label
}

// buildMaterialBundle normalizes pathological item output once, then chooses
// inline or blob storage without changing the logical turn/item structure.
func buildMaterialBundle(content MaterialContent) (materialstore.Bundle, MaterialContent, string, error) {
	if _, err := uuid.Parse(content.SourceID); err != nil {
		return materialstore.Bundle{}, MaterialContent{}, "", fmt.Errorf("材料需要准确的来源会话身份")
	}
	normalized := content
	normalized.Title = strings.TrimSpace(content.Title)
	normalized.Turns = make([]MaterialTurn, len(content.Turns))
	bundle := materialstore.Bundle{Blobs: map[string]string{}}
	manifest := materialstore.Manifest{
		Schema:         materialstore.ManifestSchema,
		Title:          strings.TrimSpace(content.Title),
		Provider:       content.Provider,
		SourceID:       content.SourceID,
		StartTurnID:    content.StartTurnID,
		EndTurnID:      content.EndTurnID,
		ReadingStartID: content.ReadingStartID,
	}
	inlineBytes := 0
	for turnIndex, sourceTurn := range content.Turns {
		normalizedTurn := MaterialTurn{ID: sourceTurn.ID, Status: sourceTurn.Status, Items: make([]MaterialItem, len(sourceTurn.Items))}
		turn := materialstore.Turn{ID: sourceTurn.ID, Status: sourceTurn.Status, Label: materialTurnLabel(sourceTurn)}
		for itemIndex, sourceItem := range sourceTurn.Items {
			item := sourceItem
			text, sourceLength, omitted := truncateMaterialText(item.Text)
			item.Text = text
			if omitted > 0 {
				item.SourceUTF16Length = sourceLength
				item.OmittedUTF16Length = omitted
				item.Notice = appendMaterialNotice(item.Notice, fmt.Sprintf("原始内容过长，发布时省略 %d 个 UTF-16 字符；保留头尾", omitted))
			}
			var body materialstore.Body
			byteLength := len([]byte(text))
			if byteLength <= materialstore.InlineItemBytes && inlineBytes+byteLength <= materialstore.InlineManifestBytes {
				body = materialstore.InlineBody(text)
				inlineBytes += byteLength
			} else {
				body = materialstore.BlobBody(text)
				bundle.Blobs[body.Hash] = text
			}
			turn.Items = append(turn.Items, materialstore.Item{ID: item.ID, Type: item.Type, Notice: item.Notice, Body: body, SourceUTF16Length: item.SourceUTF16Length, OmittedUTF16Length: item.OmittedUTF16Length})
			normalizedTurn.Items[itemIndex] = item
		}
		normalized.Turns[turnIndex] = normalizedTurn
		manifest.Turns = append(manifest.Turns, turn)
	}
	manifest, hash, _, err := materialstore.Finalize(manifest)
	if err != nil {
		return materialstore.Bundle{}, MaterialContent{}, "", err
	}
	bundle.Manifest = manifest
	return bundle, normalized, hash, nil
}

func materializeManifest(store *materialstore.Store, manifest materialstore.Manifest) (MaterialContent, error) {
	content := MaterialContent{Title: manifest.Title, Provider: manifest.Provider, SourceID: manifest.SourceID, StartTurnID: manifest.StartTurnID, EndTurnID: manifest.EndTurnID, ReadingStartID: manifest.ReadingStartID}
	for _, storedTurn := range manifest.Turns {
		turn := MaterialTurn{ID: storedTurn.ID, Status: storedTurn.Status}
		for _, storedItem := range storedTurn.Items {
			text, err := store.ReadBody(storedItem)
			if err != nil {
				return MaterialContent{}, err
			}
			turn.Items = append(turn.Items, MaterialItem{ID: storedItem.ID, Type: storedItem.Type, Text: text, Notice: storedItem.Notice, SourceUTF16Length: storedItem.SourceUTF16Length, OmittedUTF16Length: storedItem.OmittedUTF16Length})
		}
		content.Turns = append(content.Turns, turn)
	}
	return content, nil
}

func manifestNoticeCount(manifest materialstore.Manifest) int {
	count := 0
	for _, turn := range manifest.Turns {
		for _, item := range turn.Items {
			if item.Notice != "" {
				count++
			}
		}
	}
	return count
}

func compareManifests(previous, current materialstore.Manifest) *MaterialChanges {
	before := make(map[string]string, len(previous.Turns))
	for _, turn := range previous.Turns {
		before[turn.ID] = turn.Hash
	}
	changes := &MaterialChanges{}
	for _, turn := range current.Turns {
		if hash, ok := before[turn.ID]; !ok {
			changes.Added++
		} else {
			if hash != turn.Hash {
				changes.Changed++
			}
			delete(before, turn.ID)
		}
	}
	changes.Removed = len(before)
	return changes
}
