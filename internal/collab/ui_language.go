package collab

import (
	"fmt"
	"path/filepath"

	"teamcross/internal/uilanguage"
)

func (a *App) UILanguage() (mode, resolved string) {
	a.mu.Lock()
	mode = uilanguage.Mode(a.settings.UILanguage)
	a.mu.Unlock()
	return mode, uilanguage.Resolve(mode)
}

func (a *App) SetUILanguage(mode string) error {
	if !uilanguage.Valid(mode) {
		return fmt.Errorf("invalid UI language %q", mode)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := a.settings
	next.UILanguage = mode
	if err := writeJSONFile(filepath.Join(a.Config.DataDir, "settings.json"), next); err != nil {
		return err
	}
	a.settings = next
	return nil
}
