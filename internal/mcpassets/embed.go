package mcpassets

import _ "embed"

// Panel is a single self-contained document. No HTTP asset server is required.
//
//go:embed dist/panel.html
var Panel string
