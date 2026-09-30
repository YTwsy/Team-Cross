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
	_, resolved := localLanguagePreference(directory)
	return resolved
}

func localLanguagePreference(directory string) (string, string) {
	var settings struct {
		UILanguage string `json:"uiLanguage"`
	}
	if f, err := os.Open(filepath.Join(directory, "settings.json")); err == nil {
		defer f.Close()
		_ = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&settings)
	}
	mode := uilanguage.Mode(settings.UILanguage)
	return mode, uilanguage.Resolve(mode)
}
