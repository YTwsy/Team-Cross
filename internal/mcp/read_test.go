package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"teamcross/internal/readview"
)

func TestReadResponseGuardCountsMCPEscaping(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"text": strings.Repeat("\"\\", 12000)})
	if len(body) > readview.MaxBytes || readview.RawWireSize(body) <= readview.MaxBytes {
		t.Fatal("fixture must overflow only after MCP encoding")
	}
	var out bytes.Buffer
	err := serve(context.Background(), strings.NewReader(`{"id":1,"method":"tools/call","params":{"name":"read_material","arguments":{}}}`+"\n"), &out, nil, "", func(context.Context, string, map[string]any, string) (json.RawMessage, error) { return body, nil })
	if err != nil || out.Len() > readview.MaxBytes || !bytes.Contains(out.Bytes(), []byte(`"isError":true`)) || !json.Valid(bytes.TrimSpace(out.Bytes())) {
		t.Fatal(err, out.String())
	}
}

func TestStatusProjectionNeverReturnsDiscussionOrInvitations(t *testing.T) {
	raw := json.RawMessage(`{"id":"s","epoch":3,"busy":true,"annotations":[{"text":"discussion","target":{"quote":"long quote"}}],"materials":[{"versions":[{"hash":"h"}]}],"invitation":"secret","members":[{"id":"member"}],"capabilities":{"sendInput":true}}`)
	out := compactStatus(raw)
	for _, excluded := range []string{"discussion", "long quote", "secret", "versions"} {
		if bytes.Contains(out, []byte(excluded)) {
			t.Fatal(string(out))
		}
	}
	for _, kept := range []string{`"annotationsCount":1`, `"materialsCount":1`, `"epoch":3`, `"busy":true`, `"sendInput":true`, `"members"`} {
		if !bytes.Contains(out, []byte(kept)) {
			t.Fatal(string(out))
		}
	}
}
