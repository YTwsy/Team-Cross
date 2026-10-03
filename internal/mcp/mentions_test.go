package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMentionDiscoveryAndEmptyQuery(t *testing.T) {
	meta := mentionsTool()["_meta"].(map[string]any)
	if _, ok := meta["openai/extensions"].(map[string]any)["mentions/search"]; !ok || string(mustJSON(meta["ui"])) != `{"visibility":["app"]}` {
		t.Fatal(meta)
	}
	for _, tool := range append(Tools(), RuntimeTools()...) {
		if tool["name"] == mentionSearchTool {
			t.Fatal("composer search leaked into ordinary or shared MCP")
		}
	}
	for _, q := range []string{"", " 中文 🧭 v1 ", strings.Repeat("字", 256)} {
		if err := validateMentionArgs(map[string]any{"query": q}); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range []map[string]any{nil, {"query": nil}, {"query": 1}, {"query": "", "id": "space"}, {"query": strings.Repeat("字", 257)}} {
		if err := validateMentionArgs(args); err == nil {
			t.Fatal(args)
		}
	}
	var output bytes.Buffer
	input := `{"id":1,"method":"tools/call","params":{"name":"teamcross_search_mentions","arguments":{"query":""}}}`
	err := serveWithResources(context.Background(), strings.NewReader(input), &output, UITools(), "", func(_ context.Context, name string, args map[string]any, _ string) (json.RawMessage, error) {
		if name != mentionSearchTool || validateMentionArgs(args) != nil {
			t.Fatal(name, args)
		}
		return json.RawMessage(`{"items":[]}`), nil
	}, uiResources(t.TempDir()))
	if err != nil || !strings.Contains(output.String(), `"structuredContent":{"items":[]}`) || !strings.Contains(output.String(), `"content":[]`) {
		t.Fatal(output.String(), err)
	}
}

func TestMentionURIBindsCoreAndExactVersion(t *testing.T) {
	dir := t.TempDir()
	scope, err := mentionScope(dir)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	aliased, err := mentionScope(alias)
	if err != nil || aliased != scope {
		t.Fatal(aliased, scope, err)
	}
	refs := []mentionReference{{Kind: "material", SpaceID: "space", MaterialID: "material", Version: 1}, {Kind: "annotation", SpaceID: "space", AnnotationID: "note"}}
	for _, ref := range refs {
		got, err := parseMentionURI(ref.uri(scope), scope)
		if err != nil || got != ref {
			t.Fatal(got, err)
		}
		if _, err := parseMentionURI(ref.uri(scope), "another-core"); err == nil {
			t.Fatal("cross-Core URI accepted")
		}
	}
	base := "teamcross://" + scope
	for _, uri := range []string{
		"file:///etc/passwd", "https://localhost/", base + "/context/space/history", base + "/annotation/../x", base + "/annotation/a%2Fb/c",
		base + "/material/s/m/v/0", base + "/material/s/m/v/01", base + "/material/s/m/v/1.5", base + "/material/s/m/v/-1", base + "/material/s/m/v/9999999999999999999999",
		base + "/annotation/s/a?", base + "/annotation/s/a#", base + "/annotation/s/a?token=x", base + "/annotation/s/a#fragment", base + "/annotation/s/a/", base + "/annotation/s/汉字",
		"teamcross://user@" + scope + "/annotation/s/a",
	} {
		if _, err := parseMentionURI(uri, scope); err == nil {
			t.Fatal("invalid URI accepted", uri)
		}
	}
}

func TestMentionSearchAndReadUseOnlyScopedReaders(t *testing.T) {
	scope := "abcdef"
	withdrawn := false
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local" {
			t.Error("missing local credential")
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/library/mentions":
			if r.URL.Query().Get("query") != "调查 & v1" {
				t.Error(r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"language":"zh-CN","resources":[{"title":"调查","spaceTitle":"空间","author":"作者","reference":{"kind":"material","spaceId":"s","materialId":"m","version":1}},{"title":"核对样本","spaceTitle":"空间","author":"作者","reference":{"kind":"annotation","spaceId":"s","annotationId":"a"}}]}`)
		case "/api/collaborations/s/read-material":
			var args map[string]any
			json.NewDecoder(r.Body).Decode(&args)
			if args["materialId"] != "m" || args["version"] != float64(1) || args["view"] != "answers" || args["toolOutputs"] != "none" {
				t.Error(args)
			}
			if withdrawn {
				http.Error(w, "material withdrawn", http.StatusForbidden)
				return
			}
			fmt.Fprint(w, `{"marker":"fixed-v1","nextCursor":"page-2"}`)
		case "/api/collaborations/s/context":
			q := r.URL.Query()
			if q.Get("annotationId") != "a" || q.Get("kind") != "annotations" || q.Get("compact") != "true" {
				t.Error(q)
			}
			fmt.Fprint(w, `{"text":"selected-annotation","nextRead":{"annotationId":"a","offset":10}}`)
		default:
			t.Error("unrelated route", r.URL)
		}
	}))
	defer server.Close()
	b := Backend{URL: server.URL, Token: "local", Client: server.Client()}
	result, err := b.searchMentions(context.Background(), scope, "调查 & v1")
	if err != nil || !bytes.Contains(result, []byte("材料 · 空间 · 作者")) || !bytes.Contains(result, []byte("调查 · v1")) {
		t.Fatal(string(result), err)
	}
	var search struct {
		Items []struct {
			URI string `json:"uri"`
		} `json:"items"`
	}
	json.Unmarshal(result, &search)
	if len(search.Items) != 2 {
		t.Fatal(string(result))
	}
	for _, item := range search.Items {
		ref, err := parseMentionURI(item.URI, scope)
		if err != nil {
			t.Fatal(err)
		}
		read, err := b.readMention(context.Background(), item.URI, ref)
		if err != nil || !bytes.Contains(mustJSON(read), []byte(`next`)) || !bytes.Contains(mustJSON(read), []byte(`reference`)) {
			t.Fatal(read, err)
		}
	}
	withdrawn = true
	ref, _ := parseMentionURI(search.Items[0].URI, scope)
	if _, err := b.readMention(context.Background(), search.Items[0].URI, ref); err == nil {
		t.Fatal("returned cached content after withdrawal")
	}
	if len(calls) != 4 {
		t.Fatal(calls)
	}
}

func TestInvalidMentionCannotStartCore(t *testing.T) {
	input := `{"id":1,"method":"resources/read","params":{"uri":"teamcross://wrong/annotation/s/a"}}
{"id":2,"method":"tools/call","params":{"name":"teamcross_search_mentions","arguments":{"query":null}}}`
	var output bytes.Buffer
	dir := t.TempDir()
	if err := ServeUI(context.Background(), dir, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"error"`) || !strings.Contains(output.String(), `"isError":true`) {
		t.Fatal(output.String())
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatal("invalid request started Core", files, err)
	}
}
