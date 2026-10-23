package mcpassets

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// Bundle is served by the running Core, not the long-lived MCP executable.
// The panel and its replacement always use the same embedded, local code.
type Bundle struct {
	Protocol               int    `json:"protocol"`
	RequiredBridgeProtocol int    `json:"requiredBridgeProtocol"`
	Revision               string `json:"revision"`
	Script                 string `json:"script,omitempty"`
	Style                  string `json:"style,omitempty"`
}

func ParseBundle(panel string) Bundle {
	between := func(start, end string) string {
		_, tail, ok := strings.Cut(panel, start)
		if !ok {
			return ""
		}
		value, _, _ := strings.Cut(tail, end)
		return value
	}
	script := between(`<script type="module">`, "</script>")
	style := between(`<style id="teamcross-panel-style">`, "</style>")
	required := 0 // Historical bundles predate the bridge contract.
	if value := between(`<meta name="teamcross-bridge-protocol" content="`, `">`); value != "" {
		var err error
		required, err = strconv.Atoi(value)
		if err != nil || required < 0 {
			required = -1
		}
	}
	digest := sha256.Sum256([]byte(style + "\x00" + script))
	return Bundle{Protocol: 1, RequiredBridgeProtocol: required, Revision: hex.EncodeToString(digest[:]), Script: script, Style: style}
}

var Current = ParseBundle(Panel)
