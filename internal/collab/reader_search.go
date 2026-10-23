package collab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"teamcross/internal/materialstore"
)

type readerSearchRequest struct {
	Query        string `json:"query"`
	IncludeTools bool   `json:"includeTools,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
}
type readerMatch struct {
	TurnID      string `json:"turnId"`
	ItemID      string `json:"itemId"`
	StartOffset int    `json:"startOffset"`
	EndOffset   int    `json:"endOffset"`
	Quote       string `json:"quote"`
	Snippet     string `json:"snippet"`
	PageCursor  string `json:"pageCursor,omitempty"`
}
type readerSearchResponse struct {
	Matches    []readerMatch `json:"matches"`
	NextCursor string        `json:"nextCursor"`
	Complete   bool          `json:"complete"`
}
type readerSearchCursor struct {
	Key    string `json:"key"`
	Source string `json:"source,omitempty"`
	Hash   string `json:"hash,omitempty"`
	Turn   int    `json:"turn"`
	Item   int    `json:"item"`
	Offset int    `json:"offset"`
}

func searchCursor(in readerSearchRequest, identity string) (readerSearchCursor, *regexp.Regexp, error) {
	query := strings.TrimSpace(in.Query)
	if query == "" || materialstore.UTF16Length(query) > 256 {
		return readerSearchCursor{}, nil, fmt.Errorf("搜索词需要 1–256 个字符")
	}
	key := contentHash([]any{"reader-search/v1", identity, query, in.IncludeTools})
	cursor := readerSearchCursor{Key: key}
	if in.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err != nil || len(data) > 8192 || json.Unmarshal(data, &cursor) != nil || cursor.Key != key || cursor.Turn < 0 || cursor.Item < 0 || cursor.Offset < 0 {
			return cursor, nil, fmt.Errorf("搜索游标不属于当前内容或关键词")
		}
	}
	pattern, err := regexp.Compile("(?i)" + regexp.QuoteMeta(query))
	return cursor, pattern, err
}
func encodeSearchCursor(cursor readerSearchCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

// Bounded batches cover the whole object without loading every message into
// the browser. Counts remain partial until Complete; cancellation is checked
// between bodies and the next request reauthorizes the resource.
func searchManifest(ctx context.Context, read materialBodyReader, manifest materialstore.Manifest, in readerSearchRequest, pattern *regexp.Regexp, cursor *readerSearchCursor, pageCursor string) (*readerSearchResponse, error) {
	out := &readerSearchResponse{Matches: []readerMatch{}}
	scanned := 0
	for cursor.Turn < len(manifest.Turns) {
		turn := manifest.Turns[cursor.Turn]
		for cursor.Item < len(turn.Items) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			item := turn.Items[cursor.Item]
			if !in.IncludeTools && item.Type != "userMessage" && item.Type != "agentMessage" {
				cursor.Item++
				cursor.Offset = 0
				continue
			}
			text, err := read(item)
			if err != nil {
				return nil, err
			}
			if cursor.Offset > len(text) {
				return nil, fmt.Errorf("搜索正文已变化，请重新搜索")
			}
			base := cursor.Offset
			prefix, previous := materialstore.UTF16Length(text[:base]), base
			matches := pattern.FindAllStringIndex(text[base:], 65-len(out.Matches))
			for _, m := range matches {
				start, end := base+m[0], base+m[1]
				if len(out.Matches) >= 64 {
					cursor.Offset = start
					out.NextCursor = encodeSearchCursor(*cursor)
					return out, nil
				}
				prefix += materialstore.UTF16Length(text[previous:start])
				previous = start
				quote := text[start:end]

				out.Matches = append(out.Matches, readerMatch{TurnID: turn.ID, ItemID: item.ID, StartOffset: prefix, EndOffset: prefix + materialstore.UTF16Length(quote), Quote: quote, Snippet: searchSnippet(text, start, end), PageCursor: pageCursor})
			}
			scanned += len(text)
			cursor.Item++
			cursor.Offset = 0
			if scanned >= 1<<20 {
				out.NextCursor = encodeSearchCursor(*cursor)
				return out, nil
			}
		}
		cursor.Turn++
		cursor.Item = 0
	}
	out.Complete = true
	return out, nil
}

func searchSnippet(text string, start, end int) string {
	before, after := start, end
	for n := 0; n < 40 && before > 0; n++ {
		_, size := utf8.DecodeLastRuneInString(text[:before])
		before -= size
	}
	for n := 0; n < 60 && after < len(text); n++ {
		_, size := utf8.DecodeRuneInString(text[after:])
		after += size
	}
	return text[before:after]
}

func searchMaterial(ctx context.Context, read materialBodyReader, manifest materialstore.Manifest, hash, identity string, in readerSearchRequest) (*readerSearchResponse, error) {
	cursor, pattern, err := searchCursor(in, identity+":"+hash)
	if err != nil {
		return nil, err
	}
	return searchManifest(ctx, read, manifest, in, pattern, &cursor, "")
}

func (s *Session) searchHistory(ctx context.Context, record Record, process Runtime, in readerSearchRequest) (*readerSearchResponse, error) {
	cursor, pattern, err := searchCursor(in, record.SessionID)
	if err != nil {
		return nil, err
	}
	_, turns, older, err := s.loadHistoryPage(ctx, record, process, cursor.Source)
	if err != nil {
		return nil, err
	}
	hash := historyPageHash(record.SessionID, cursor.Source, turns)
	if cursor.Hash != "" && cursor.Hash != hash {
		return nil, fmt.Errorf("搜索期间对话已更新，请重新搜索")
	}
	cursor.Hash = hash
	pageCursor := encodeMaterialCursor(materialCursor{Scope: "stream", MaterialID: record.SessionID, Hash: hash, SourceCursor: cursor.Source})
	out, err := searchManifest(ctx, func(item materialstore.Item) (string, error) { return item.Body.Text, nil }, transientHistoryManifest(turns), in, pattern, &cursor, pageCursor)
	if err != nil {
		return nil, err
	}
	if out.Complete && older != "" {
		if older == cursor.Source {
			return nil, fmt.Errorf("历史搜索分页没有推进")
		}
		cursor.Source = older
		cursor.Hash = ""
		cursor.Turn = 0
		cursor.Item = 0
		cursor.Offset = 0
		out.Complete = false
		out.NextCursor = encodeSearchCursor(cursor)
	}
	return out, nil
}

// Used when matching targets against UTF-16 source offsets in tests and UI.
func readerQuote(text string, start, end int) string {
	r := utf16.Encode([]rune(text))
	if start < 0 || end > len(r) || start > end {
		return ""
	}
	return string(utf16.Decode(r[start:end]))
}
