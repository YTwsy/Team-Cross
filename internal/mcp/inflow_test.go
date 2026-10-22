package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type brokenInFlowWriter struct{}

func (brokenInFlowWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestInFlowStdioCommitsOnlySuccessfulBoundedOutput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		output    io.Writer
		body      string
		toolError bool
		delivered bool
		attach    bool
	}{
		{"success", &bytes.Buffer{}, `{"value":1}`, false, true, true},
		{"failed output", brokenInFlowWriter{}, `{}`, false, false, true},
		{"over budget", &bytes.Buffer{}, `{"value":"` + strings.Repeat("x", 65000) + `"}`, false, false, false},
		{"tool error", &bytes.Buffer{}, `{}`, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, finished := 0, 0
			ctx := context.WithValue(context.Background(), inFlowKey{}, inFlowPrepare(func(context.Context) *inFlowAttachment {
				prepared++
				return &inFlowAttachment{Text: strings.Repeat("note", 500), Finish: func(ok bool) {
					finished++
					if ok != tc.delivered {
						t.Errorf("delivered=%v", ok)
					}
				}}
			}))
			input := `{"id":1,"method":"tools/call","params":{"name":"read_context","arguments":{}}}` + "\n"
			err := serve(ctx, strings.NewReader(input), tc.output, nil, "", func(context.Context, string, map[string]any, string) (json.RawMessage, error) {
				if tc.toolError {
					return nil, errors.New("failed")
				}
				return json.RawMessage(tc.body), nil
			})
			if tc.name == "failed output" {
				if err == nil {
					t.Fatal("expected output error")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tc.toolError {
				if prepared != 0 {
					t.Fatal("prepared on failed tool")
				}
				return
			}
			if prepared != 1 || finished != 1 {
				t.Fatal(prepared, finished)
			}
			if buf, ok := tc.output.(*bytes.Buffer); ok {
				var reply struct {
					Result struct {
						Content []any `json:"content"`
					} `json:"result"`
				}
				if err = json.Unmarshal(buf.Bytes(), &reply); err != nil {
					t.Fatal(err)
				}
				want := 1
				if tc.attach {
					want = 2
				}
				if len(reply.Result.Content) != want {
					t.Fatal(len(reply.Result.Content))
				}
			}
		})
	}
}

func TestInFlowSkipsAppOnlyAndMissingIdentity(t *testing.T) {
	for _, name := range []string{"open_teamcross", "teamcross_ui_read", "teamcross_ui_write", mentionSearchTool} {
		if passiveTool(name) {
			t.Fatal(name)
		}
	}
	ctx := withInFlow(context.Background(), t.TempDir(), "", "")
	if ctx.Value(inFlowKey{}).(inFlowPrepare)(ctx) != nil {
		t.Fatal("unidentified caller received update")
	}
}
