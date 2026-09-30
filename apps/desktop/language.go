package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"teamcross/internal/uilanguage"
)

// Core owns this preference. Read it for native errors even when Core is
// unreachable; never write a separate desktop language setting.
func localLanguage(directory string) string {
	var settings struct {
		UILanguage string `json:"uiLanguage"`
	}
	if f, err := os.Open(filepath.Join(directory, "settings.json")); err == nil {
		defer f.Close()
		_ = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&settings)
	}
	return uilanguage.Resolve(uilanguage.Mode(settings.UILanguage))
}
