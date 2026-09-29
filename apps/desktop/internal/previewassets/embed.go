package previewassets

import "embed"

//go:embed index.html preview.css preview.js
var FS embed.FS
