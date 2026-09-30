package collab

import (
	"fmt"
	"path/filepath"
)

func validUITheme(mode string) bool {
	return mode == "system" || mode == "light" || mode == "dark"
}

func (a *App) UITheme() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if validUITheme(a.settings.UITheme) {
		return a.settings.UITheme
	}
	return "system"
}

func (a *App) SetUITheme(mode string) error {
	if !validUITheme(mode) {
		return fmt.Errorf("invalid UI theme")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := a.settings
	next.UITheme = mode
	if err := writeJSONFile(filepath.Join(a.Config.DataDir, "settings.json"), next); err != nil {
		return err
	}
	a.settings = next
	return nil
}
