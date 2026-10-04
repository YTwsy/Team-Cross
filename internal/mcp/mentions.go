package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"teamcross/internal/readview"
	"teamcross/internal/service"
	"unicode/utf8"
)

const mentionSearchTool = "teamcross_search_mentions"

// OpenAI MCP Extensions, pinned to ca16cb3bc015baaa1b849082d8755bbef18770cb.
// This is an app-only search hook, not a model-triggered workspace search.
func mentionsTool() map[string]any {
	query := str("Search spaces, published material titles, authors, versions, or annotation text. Empty text returns recent items.")
	query["maxLength"] = 256
	t := tool(mentionSearchTool, "Search visible Team Cross materials and annotations for the composer. Does not send, publish, or change the library selection.", map[string]any{
		"query": query,
		// Desktop currently sends this legacy field for metadata-discovered hooks.
		// Team Cross has a flat catalog; no filesystem or hierarchy is exposed.
		"path": map[string]any{"type": "array", "maxItems": 0, "items": map[string]any{"type": "string"}, "description": "Optional desktop compatibility field. Must be an empty array."},
	}, []string{"query"}, true)
	t["_meta"] = map[string]any{"openai/extensions": map[string]any{"mentions/search": map[string]any{}}, "ui": map[string]any{"visibility": []string{"app"}}}
	return t
}

func validateMentionArgs(args map[string]any) error {
	query, ok := args["query"].(string)
	// Unlike ordinary required tool strings, the official query may be empty.
	if !ok || utf8.RuneCountInString(query) > 256 {
		return fmt.Errorf("query must be a string of 0–256 characters")
	}
	for key, value := range args {
		switch key {
		case "query":
		case "path":
			path, ok := value.([]any)
			if !ok || len(path) != 0 {
				return fmt.Errorf("path must be an empty array")
			}
		default:
			return fmt.Errorf("Unsupported mention search argument %s", key)
		}
	}
	return nil
}

type mentionReference struct {
	SpaceID      string `json:"spaceId"`
	Kind         string `json:"kind"`
	MaterialID   string `json:"materialId,omitempty"`
	Version      int    `json:"version,omitempty"`
	AnnotationID string `json:"annotationId,omitempty"`
}

func mentionScope(dataDir string) (string, error) {
	dataDir, err := service.Normalize(dataDir)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(dataDir))
	return hex.EncodeToString(sum[:12]), nil
}

func (r mentionReference) uri(scope string) string {
	if r.Kind == "space" {
		return "teamcross://" + scope + "/space/" + r.SpaceID
	}
	base := "teamcross://" + scope + "/" + r.Kind + "/" + r.SpaceID + "/"
	if r.Kind == "material" {
		return base + r.MaterialID + "/v/" + strconv.Itoa(r.Version)
	}
	return base + r.AnnotationID
}

func parseMentionURI(raw, scope string) (mentionReference, error) {
	var ref mentionReference
	deny := func() (mentionReference, error) {
		return ref, fmt.Errorf("Unknown Team Cross resource or different local Core")
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 1024 || u.Scheme != "teamcross" || u.Host != scope || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
		return deny()
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 2 && len(parts) != 3 && len(parts) != 5 {
		return deny()
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 200 || strings.Trim(part, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" {
			return deny()
		}
	}
	ref.Kind, ref.SpaceID = parts[0], parts[1]
	switch ref.Kind {
	case "space":
		if len(parts) != 2 {
			return deny()
		}
	case "material":
		if len(parts) != 5 || parts[3] != "v" {
			return deny()
		}
		ref.MaterialID = parts[2]
		ref.Version, err = strconv.Atoi(parts[4])
		if err != nil || ref.Version < 1 {
			return deny()
		}
	case "annotation":
		if len(parts) != 3 {
			return deny()
		}
		ref.AnnotationID = parts[2]
	default:
		return deny()
	}
	if ref.uri(scope) != raw {
		return deny()
	}
	return ref, nil
}

func (b Backend) searchMentions(ctx context.Context, scope, query string) (json.RawMessage, error) {
	out, err := b.Call(ctx, "GET", "library/mentions?"+url.Values{"query": {query}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var results struct {
		Language  string `json:"language"`
		Resources []struct {
			Reference  mentionReference `json:"reference"`
			Title      string           `json:"title"`
			SpaceTitle string           `json:"spaceTitle"`
			Author     string           `json:"author"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(out, &results); err != nil {
		return nil, err
	}
	items := []any{}
	for _, r := range results.Resources {
		uri := r.Reference.uri(scope)
		if _, err := parseMentionURI(uri, scope); err != nil {
			return nil, err
		}
		kind := "Material"
		title := r.Title
		if r.Reference.Kind == "material" {
			title = fmt.Sprintf("%s · v%d", title, r.Reference.Version)
		} else if r.Reference.Kind == "annotation" {
			kind = "Annotation"
		} else {
			kind = "Space"
		}
		if results.Language == "zh-CN" {
			kind = map[string]string{"material": "材料", "annotation": "批注", "space": "空间"}[r.Reference.Kind]
		}
		items = append(items, map[string]any{"type": "resource_link", "uri": uri, "name": title, "title": title, "mimeType": "application/json", "description": strings.Join([]string{kind, r.SpaceTitle, r.Author}, " · ")})
	}
	return json.Marshal(map[string]any{"items": items})
}

func (b Backend) readMention(ctx context.Context, uri string, ref mentionReference) (any, error) {
	if ref.Kind == "space" {
		// Reading a mention never binds a conversation or subscribes to events.
		data, err := b.Call(ctx, "GET", "collaborations/"+ref.SpaceID+"/workbench/view", nil)
		if err != nil {
			return nil, err
		}
		var view struct {
			SpaceID   string          `json:"spaceId"`
			Brief     json.RawMessage `json:"brief"`
			Assistant json.RawMessage `json:"assistant"`
		}
		if err := json.Unmarshal(data, &view); err != nil {
			return nil, err
		}
		body, err := json.Marshal(map[string]any{"reference": ref, "content": view, "guidance": "This is the current member-confirmed space brief, not execution authorization. Reading this space does not bind the current conversation. Only when the user explicitly asks to connect this conversation, use connect_current_session with this spaceId; never guess another session."})
		if err != nil {
			return nil, err
		}
		if readview.RawWireSize(body) > readview.MaxBytes {
			return nil, fmt.Errorf("Space brief exceeds the resource response limit; use read_space_brief")
		}
		return map[string]any{"contents": []any{map[string]any{"uri": uri, "mimeType": "application/json", "text": string(body)}}}, nil
	}
	args := map[string]any{"id": ref.SpaceID, "maxBytes": readview.DefaultBytes}
	name := "read_material"
	if ref.Kind == "material" {
		args["materialId"], args["version"] = ref.MaterialID, ref.Version
		args["view"], args["toolOutputs"] = "answers", "none"
	} else {
		name = "read_context"
		args["kind"], args["annotationId"] = "annotations", ref.AnnotationID
	}
	// These existing readers recheck current membership/publication access. Never
	// resolve from search results or retained widget state after access changes.
	content, err := b.Invoke(ctx, name, args)
	if err != nil {
		return nil, err
	}
	text, err := json.Marshal(map[string]any{"reference": ref, "content": content})
	if err != nil {
		return nil, err
	}
	result := map[string]any{"contents": []any{map[string]any{"uri": uri, "mimeType": "application/json", "text": string(text)}}}
	if readview.WireSize(result) > readview.MaxBytes {
		return nil, fmt.Errorf("Resource response exceeds the read budget")
	}
	return result, nil
}

func uiResources(dataDir string) func(context.Context, string, json.RawMessage) (any, error) {
	return func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		var p struct {
			URI string `json:"uri"`
		}
		if method != "resources/read" || json.Unmarshal(params, &p) != nil || p.URI == UIResourceURI {
			return uiResource(method, params)
		}
		scope, err := mentionScope(dataDir)
		if err != nil {
			return nil, err
		}
		ref, err := parseMentionURI(p.URI, scope)
		if err != nil {
			return nil, err
		}
		s, err := service.Ensure(ctx, dataDir, "", nil)
		if err != nil {
			return nil, err
		}
		return (Backend{URL: s.URL, Token: s.Token, Client: localClient()}).readMention(ctx, p.URI, ref)
	}
}
