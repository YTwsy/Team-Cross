package collab

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/materialstore"
)

func readerPNG(t *testing.T, shade uint8) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{R: shade, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestReaderImageVersionAndRemoteScope(t *testing.T) {
	ctx := context.Background()
	a, f, _ := fixture(t)
	s, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "Image review"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "native.png")
	original := readerPNG(t, 10)
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"id": "turn", "status": "completed", "items": []any{map[string]any{"id": "question", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": "Original material 🙂"}, map[string]any{"type": "localImage", "path": path}}}}})
	turn, err := exportTurn(raw)
	if err != nil || len(turn.Items[0].Attachments) != 1 {
		t.Fatal(turn, err)
	}
	attachment := turn.Items[0].Attachments[0]
	if !strings.Contains(readerQuote(turn.Items[0].Text, attachment.StartOffset, attachment.EndOffset), "native.png") {
		t.Fatal("image anchor does not map to source")
	}
	draft, err := a.saveDraft(PublicationDraft{ID: uuid.NewString(), FrozenAt: time.Now(), MaterialContent: MaterialContent{Title: "Images", Provider: "codex", SourceID: f.source.ID, StartTurnID: "turn", EndTurnID: "turn", Turns: []MaterialTurn{turn}}})
	if err != nil {
		t.Fatal(err)
	}
	// Changing/deleting the provider file cannot change an already frozen draft.
	if err = os.WriteFile(path, readerPNG(t, 200), 0600); err != nil {
		t.Fatal(err)
	}
	updated, err := exportTurn(raw)
	if err != nil {
		t.Fatal(err)
	}
	second := draft
	second.ID = uuid.NewString()
	second.Turns = []MaterialTurn{updated}
	second, err = a.saveDraft(second)
	if err != nil {
		t.Fatal(err)
	}
	out, err := a.Publish(ctx, s.record.ID, PublishInput{PreviewID: draft.ID, PreviewHash: draft.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	mid := out.(map[string]any)["materialId"].(string)
	if _, err = a.Publish(ctx, s.record.ID, PublishInput{PreviewID: second.ID, PreviewHash: second.Hash, RequestID: uuid.NewString(), MaterialID: mid, BaseVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	read := MaterialRead{MaterialID: mid, Version: 1, TurnID: "turn", ItemID: "question", ReaderRequest: ReaderRequest{AttachmentID: attachment.ID}}
	value, err := s.readMaterial(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	if value.(materialReadResponse).Attachment.Data != base64.StdEncoding.EncodeToString(original) {
		t.Fatal("version image was not frozen")
	}
	read.Version = 2
	value, err = s.readMaterial(ctx, read)
	if err != nil || value.(materialReadResponse).Attachment.Data != base64.StdEncoding.EncodeToString(readerPNG(t, 200)) {
		t.Fatal("updated image was not isolated in v2", err)
	}
	read.Version = 1
	read.AttachmentID = "not-in-version"
	if _, err = s.readMaterial(ctx, read); err == nil {
		t.Fatal("unreferenced attachment readable")
	}
	read.AttachmentID = attachment.ID
	if err = s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	b, _, _ := fixture(t)
	guest, err := b.Join(ctx, s.sharedRuntime().Token())
	if err != nil {
		t.Fatal(err)
	}
	var remote materialReadResponse
	if err = guest.request(ctx, "POST", "/v2/read-material", read, &remote); err != nil || remote.Attachment == nil {
		t.Fatal(remote, err)
	}
	// A contributing member negotiates and uploads image blobs through the same
	// scoped publication protocol as text, without exposing a bare blob route.
	contribution := second
	contribution.ID = uuid.NewString()
	contribution.Turns = []MaterialTurn{{ID: "guest-turn", Status: "completed", Items: []MaterialItem{{ID: "guest-item", Type: "userMessage", Text: "[guest image]", Attachments: []MaterialImage{{ID: "image-1", Name: "guest.png", MIME: "image/png", Data: base64.StdEncoding.EncodeToString(readerPNG(t, 100)), StartOffset: 0, EndOffset: 13}}}}}}
	contribution.StartTurnID, contribution.EndTurnID = "guest-turn", "guest-turn"
	contribution, err = b.saveDraft(contribution)
	if err != nil {
		t.Fatal(err)
	}
	contributed, err := b.Publish(ctx, guest.ID, PublishInput{PreviewID: contribution.ID, PreviewHash: contribution.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	contributedID := contributed.(map[string]any)["materialId"].(string)
	value, err = s.readMaterial(ctx, MaterialRead{MaterialID: contributedID, Version: 1, TurnID: "guest-turn", ItemID: "guest-item", ReaderRequest: ReaderRequest{AttachmentID: "image-1"}})
	if err != nil || value.(materialReadResponse).Attachment.Data != contribution.Turns[0].Items[0].Attachments[0].Data {
		t.Fatal("remote image upload missing", err)
	}
	if _, err = s.withdrawMaterial(ctx, mid); err != nil {
		t.Fatal(err)
	}
	if err = guest.request(ctx, "POST", "/v2/read-material", read, &remote); err == nil {
		t.Fatal("withdrawn image readable")
	}
	read.ReaderRequest = ReaderRequest{Search: &readerSearchRequest{Query: "Original"}}
	if err = guest.request(ctx, "POST", "/v2/read-material", read, &remote); err == nil {
		t.Fatal("withdrawn material searchable")
	}
}

func TestReaderSearchPaginatesExactUnicodeAndToolScope(t *testing.T) {
	text := "前缀🙂 KELVIN K 中文 " + strings.Repeat("目标🙂 ", 140)
	manifest := transientHistoryManifest([]MaterialTurn{{ID: "turn", Status: "completed", Items: []MaterialItem{{ID: "answer", Type: "agentMessage", Text: text}, {ID: "tool", Type: "toolResult", Text: "目标🙂"}}}})
	read := func(item materialstore.Item) (string, error) { return item.Body.Text, nil }
	in := readerSearchRequest{Query: "目标🙂"}
	count := 0
	for n := 0; n < 10; n++ {
		page, err := searchMaterial(context.Background(), read, manifest, "hash", "material", in)
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range page.Matches {
			if readerQuote(text, hit.StartOffset, hit.EndOffset) != hit.Quote {
				t.Fatal("UTF-16 mapping lost", hit)
			}
			count++
		}
		if page.Complete {
			break
		}
		if page.NextCursor == in.Cursor {
			t.Fatal("cursor did not advance")
		}
		in.Cursor = page.NextCursor
	}
	if count != 140 {
		t.Fatal(count)
	}
	if _, err := searchMaterial(context.Background(), read, manifest, "another-version", "material", in); err == nil {
		t.Fatal("cross-version cursor accepted")
	}
	page, err := searchMaterial(context.Background(), read, manifest, "hash", "material", readerSearchRequest{Query: "kelvin K"})
	if err != nil || len(page.Matches) != 1 {
		t.Fatal(page, err)
	}
	page, err = searchMaterial(context.Background(), read, manifest, "hash", "material", readerSearchRequest{Query: "目标🙂", IncludeTools: true})
	if err != nil || page.Complete {
		t.Fatal(page, err)
	}
	toolManifest := transientHistoryManifest([]MaterialTurn{{ID: "t", Status: "completed", Items: []MaterialItem{{ID: "tool", Type: "toolResult", Text: "only-tool"}}}})
	page, _ = searchMaterial(context.Background(), read, toolManifest, "h", "m", readerSearchRequest{Query: "only-tool"})
	if len(page.Matches) != 0 {
		t.Fatal("tools enabled implicitly")
	}
	page, _ = searchMaterial(context.Background(), read, toolManifest, "h", "m", readerSearchRequest{Query: "only-tool", IncludeTools: true})
	if len(page.Matches) != 1 {
		t.Fatal("tool search missing")
	}
}

func TestReaderImageRejectsRemoteAndDisguisedContent(t *testing.T) {
	for _, value := range []string{"https://example.invalid/image.png", "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("<svg onload='alert(1)'/>"))} {
		if _, err := sourceImage(value, "", false); err == nil {
			t.Fatal("unsupported input accepted")
		}
	}
	if err := materialstore.ValidateImage("image/jpeg", base64.StdEncoding.EncodeToString(readerPNG(t, 1))); err == nil {
		t.Fatal("MIME mismatch accepted")
	}
}

func TestReaderSearchCancellationAndImageSegmentBoundary(t *testing.T) {
	manifest := transientHistoryManifest([]MaterialTurn{{ID: "t", Items: []MaterialItem{{ID: "i", Type: "agentMessage", Text: "x"}}}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := searchMaterial(ctx, func(item materialstore.Item) (string, error) { return item.Body.Text, nil }, manifest, "hash", "id", readerSearchRequest{Query: "x"}); err != context.Canceled {
		t.Fatal(err)
	}
	text := "🙂 prefix [image] suffix"
	item := materialstore.Item{Attachments: []materialstore.Attachment{{ID: "i", StartOffset: 10, EndOffset: 17}}}
	first := imageMetadata(item, text, 0, 12)
	if len(first) != 1 || first[0].Quote != "[image]" {
		t.Fatal(first)
	}
	if len(imageMetadata(item, text, 12, 24)) != 0 {
		t.Fatal("image repeated across text pages")
	}
}
