// Package readview defines the bounded, opt-in projection used by Agent readers.
package readview

import (
	"encoding/json"
	"fmt"
)

const DefaultBytes = 24 << 10
const MaxBytes = 64 << 10

type Options struct {
	View        string `json:"view,omitempty"`
	ToolOutputs string `json:"toolOutputs,omitempty"`
	TurnLimit   int    `json:"turnLimit,omitempty"`
	MaxBytes    int    `json:"maxBytes,omitempty"`
}

func (o Options) Enabled() bool { return o != (Options{}) }

func (o Options) Normalize() (Options, error) {
	if o.View == "" {
		o.View = "answers"
	}
	if o.ToolOutputs == "" {
		o.ToolOutputs = "none"
	}
	if o.TurnLimit == 0 {
		o.TurnLimit = 3
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = DefaultBytes
	}
	if o.View != "answers" && o.View != "conversation" && o.View != "outline" && o.View != "items" {
		return o, fmt.Errorf("view 必须是 answers、conversation、outline 或 items")
	}
	if o.ToolOutputs != "none" && o.ToolOutputs != "preview" && o.ToolOutputs != "full" {
		return o, fmt.Errorf("toolOutputs 必须是 none、preview 或 full")
	}
	if o.TurnLimit < 1 || o.TurnLimit > 50 || o.MaxBytes < 8<<10 || o.MaxBytes > MaxBytes {
		return o, fmt.Errorf("turnLimit 必须为 1–50，maxBytes 必须为 8192–65536")
	}
	return o, nil
}

// WireSize includes the second JSON encoding of MCP's text content and reserves
// space for JSON-RPC framing. Text lengths alone substantially undercount this.
func WireSize(value any) int {
	b, err := json.Marshal(value)
	if err != nil {
		return MaxBytes + 1
	}
	return RawWireSize(b)
}

func RawWireSize(b []byte) int {
	encoded, _ := json.Marshal(string(b))
	return len(encoded) + 512
}

// Defaults applies only to first reads: continuations carry their own options.
func Defaults(args map[string]any) {
	if cursor, _ := args["cursor"].(string); cursor == "" || args["itemId"] != nil || args["turnId"] != nil {
		if _, ok := args["view"]; !ok {
			args["view"] = "answers"
		}
	}
}
