package pluginpack

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Connection manages one explicit opt-in per personal native profile. Package
// files and Core data outlive disconnect; native registration uses only its CLI.
type Connection struct {
	Home, Codex, Binary, Root, DataDir, Version string
}

type connectionState struct {
	Format     int    `json:"format"`
	Enabled    bool   `json:"enabled"`
	Root       string `json:"root"`
	DataDir    string `json:"dataDir"`
	Source     string `json:"source"`
	Generation string `json:"generation"`
	Error      string `json:"error,omitempty"`
}

type ConnectionStatus struct {
	State          string `json:"state"`
	Available      bool   `json:"available"`
	AutoUpdate     bool   `json:"autoUpdate"`
	Installed      bool   `json:"installed"`
	PluginEnabled  bool   `json:"pluginEnabled"`
	ReloadRequired bool   `json:"reloadRequired"`
	Version        string `json:"version,omitempty"`
	Root           string `json:"root,omitempty"`
	DataDir        string `json:"dataDir,omitempty"`
	DifferentData  bool   `json:"differentData"`
	Error          string `json:"error,omitempty"`
}

func NativeHome() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Abs(home)
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, ".codex"), err
}

// Prefer the desktop's matching CLI over an unrelated CLI earlier in PATH.
func DesktopCLI() string {
	home, _ := os.UserHomeDir()
	for _, base := range []string{"/Applications", filepath.Join(home, "Applications")} {
		for _, name := range []string{"ChatGPT.app", "Codex.app"} {
			for _, relative := range []string{"Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex", "Contents/Resources/codex"} {
				path := filepath.Join(base, name, relative)
				if executable(path) {
					return path
				}
			}
		}
	}
	return ""
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0
}

func (c Connection) dir() string { return filepath.Join(c.Home, "teamcross-plugin") }

func (c Connection) read() (connectionState, error) {
	var s connectionState
	if err := safePath(c.dir(), "connection.json"); err != nil {
		return s, err
	}
	b, err := os.ReadFile(filepath.Join(c.dir(), "connection.json"))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if json.Unmarshal(b, &s) != nil || s.Format != 1 || !filepath.IsAbs(s.Root) || !filepath.IsAbs(s.DataDir) || !filepath.IsAbs(s.Source) {
		return s, fmt.Errorf("Invalid Team Cross plugin connection record")
	}
	return s, nil
}

func (c Connection) save(s connectionState) error {
	if err := safePath(c.dir(), "connection.json"); err != nil {
		return err
	}
	return jsonFile(filepath.Join(c.dir(), "connection.json"), s)
}

// ManagedPackage keeps the original manual commands pointed at the package
// selected in Settings. A manual-only installation has no connection record.
func ManagedPackage(home string) (root string, enabled bool, err error) {
	s, err := (Connection{Home: home}).read()
	return s.Root, s.Enabled, err
}

// ManagementBinary lets a Core started by a copied plugin runtime use the App
// which owns updates. It never accepts executable paths from HTTP input.
func ManagementBinary(home, fallback string) (string, error) {
	s, err := (Connection{Home: home}).read()
	if err != nil {
		return "", err
	}
	if s.Source != "" && fallback == filepath.Join(s.Root, "runtime", "teamcross") {
		if !executable(s.Source) {
			return "", fmt.Errorf("Team Cross installation moved; open the installed Team Cross App to reconnect")
		}
		return s.Source, nil
	}
	return fallback, nil
}

func digest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (c Connection) inspect(ctx context.Context, s connectionState) (ConnectionStatus, Manager, error) {
	v := ConnectionStatus{State: "not_installed", Available: c.Codex != "", AutoUpdate: s.Enabled}
	m := Manager{Codex: c.Codex, Home: c.Home, Root: c.Root}
	if s.Root != "" {
		m.Root = s.Root
	}
	if c.Codex == "" {
		v.State = "unavailable"
		return v, m, nil
	}
	// ChatGPT can be installed before its personal profile is created. Reading
	// status must not create that profile just to run the native inventory CLI.
	if _, err := os.Stat(c.Home); os.IsNotExist(err) {
		return v, m, nil
	} else if err != nil {
		return v, m, err
	}
	b, err := m.command(ctx, "marketplace", "list")
	if err != nil {
		return v, m, err
	}
	var sources struct {
		Marketplaces []struct {
			Name              string `json:"name"`
			MarketplaceSource struct {
				SourceType string `json:"sourceType"`
				Source     string `json:"source"`
			} `json:"marketplaceSource"`
		} `json:"marketplaces"`
	}
	if err = json.Unmarshal(b, &sources); err != nil {
		return v, m, err
	}
	found := false
	for _, item := range sources.Marketplaces {
		if item.Name != Marketplace {
			continue
		}
		root := item.MarketplaceSource.Source
		if item.MarketplaceSource.SourceType != "local" || !filepath.IsAbs(root) || (s.Root != "" && s.Root != root) {
			return v, m, fmt.Errorf("Marketplace name belongs to another source")
		}
		m.Root, found = filepath.Clean(root), true
	}
	if found {
		items, err := m.inventory(ctx)
		if err != nil {
			return v, m, err
		}
		if err = m.verifyInstalled(items); err != nil {
			return v, m, err
		}
		for _, raw := range items {
			var item struct {
				Enabled bool   `json:"enabled"`
				Version string `json:"version"`
			}
			if err = json.Unmarshal(raw, &item); err != nil {
				return v, m, err
			}
			v.Installed, v.PluginEnabled, v.Version = true, item.Enabled, item.Version
		}
	}
	p, err := owned(m.Root)
	if err != nil && (found || !os.IsNotExist(err)) {
		return v, m, err
	}
	if err == nil {
		if p.Binary != filepath.Join(m.Root, "runtime", "teamcross") || !filepath.IsAbs(p.DataDir) || (s.DataDir != "" && p.DataDir != s.DataDir) {
			return v, m, fmt.Errorf("Plugin package binding changed; inspect the original installation")
		}
		if err = safePath(m.Root, "runtime/teamcross"); err != nil {
			return v, m, err
		}
		v.Root, v.DataDir = m.Root, p.DataDir
		v.DifferentData = p.DataDir != c.DataDir
		if v.Installed {
			current, err := digest(p.Binary)
			if err != nil {
				return v, m, err
			}
			source, err := digest(c.Binary)
			if err != nil {
				return v, m, err
			}
			v.State = "installed"
			if current != source {
				v.State = "update_available"
			}
		}
	}
	if s.Enabled && (!v.Installed || !v.PluginEnabled) {
		v.State = "disconnected"
	}
	if v.Installed && !v.PluginEnabled {
		v.State = "disconnected"
	}
	if s.Generation != "" && v.Installed && v.PluginEnabled {
		var manifest struct {
			Servers map[string]struct{ Env map[string]string } `json:"mcpServers"`
		}
		b, err := os.ReadFile(filepath.Join(m.Root, "plugins/teamcross/.mcp.json"))
		if err != nil {
			return v, m, err
		}
		if json.Unmarshal(b, &manifest) != nil || manifest.Servers["teamcross-ui"].Env["TEAMCROSS_PLUGIN_GENERATION"] != s.Generation || manifest.Servers["teamcross-ui"].Env["TEAMCROSS_PLUGIN_PROFILE"] != c.Home {
			v.State = "update_available"
		}
		var receipt struct {
			Generation string `json:"generation"`
		}
		if err = safePath(c.dir(), "loaded.json"); err != nil {
			return v, m, err
		}
		b, _ = os.ReadFile(filepath.Join(c.dir(), "loaded.json"))
		_ = json.Unmarshal(b, &receipt)
		v.ReloadRequired = receipt.Generation != s.Generation
		if v.ReloadRequired && v.State == "installed" {
			v.State = "reload_required"
		}
	}
	if s.Error != "" {
		v.State, v.Error = "error", s.Error
	}
	return v, m, nil
}

func (c Connection) Status(ctx context.Context) ConnectionStatus {
	s, err := c.read()
	if err != nil {
		return ConnectionStatus{State: "error", Error: err.Error()}
	}
	v, _, err := c.inspect(ctx, s)
	if err != nil {
		v.State, v.Error = "error", err.Error()
	}
	return v
}

// Apply serializes lifecycle changes across all Core/App instances sharing a
// profile. Automatic sync never installs a removed/disabled plugin or opts in.
func (c Connection) Apply(ctx context.Context, action string) (ConnectionStatus, error) {
	if action != "connect" && action != "sync" && action != "disconnect" {
		return ConnectionStatus{}, fmt.Errorf("Unknown connection action")
	}
	s, err := c.read()
	if err != nil {
		return ConnectionStatus{}, err
	}
	if action == "sync" && !s.Enabled {
		return ConnectionStatus{State: "not_enabled"}, nil
	}
	if err = safePath(c.dir(), "lifecycle.lock"); err != nil {
		return ConnectionStatus{}, err
	}
	if err = os.MkdirAll(c.dir(), 0700); err != nil {
		return ConnectionStatus{}, err
	}
	f, err := os.OpenFile(filepath.Join(c.dir(), "lifecycle.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return ConnectionStatus{}, err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return ConnectionStatus{}, fmt.Errorf("Plugin update is in progress; check status shortly")
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	s, err = c.read()
	if err != nil {
		return ConnectionStatus{}, err
	}
	if action == "sync" && !s.Enabled {
		return ConnectionStatus{State: "not_enabled"}, nil
	}
	v, m, err := c.inspect(ctx, s)
	if err != nil {
		return v, err
	}
	if !v.Available {
		return v, fmt.Errorf("ChatGPT with local plugin support was not found")
	}
	if action == "sync" {
		if !v.Installed || !v.PluginEnabled {
			return v, nil
		}
		if s.Source != c.Binary {
			s.Error = "Plugin updates belong to another Team Cross installation; reconnect from Settings"
			if err = c.save(s); err != nil {
				return v, err
			}
			return v, fmt.Errorf("%s", s.Error)
		}
		if v.State != "update_available" && s.Error == "" {
			return v, nil
		}
	}
	if action == "disconnect" {
		// Stop future automatic writes before attempting native removal.
		if s.Format == 0 {
			data := v.DataDir
			if data == "" {
				data = c.DataDir
			}
			s = connectionState{Format: 1, Root: m.Root, DataDir: data, Source: c.Binary}
		}
		s.Enabled = false
		if err = c.save(s); err != nil {
			return v, err
		}
		_, err = m.Remove(ctx)
	} else {
		data := v.DataDir
		if data == "" {
			data = c.DataDir
		}
		s = connectionState{Format: 1, Enabled: true, Root: m.Root, DataDir: data, Source: c.Binary, Generation: randomGeneration()}
		// Persist intent before export/remove/add. An interrupted operation is
		// visible and repairable through an explicit connect, never silently done.
		s.Error = "Plugin setup did not finish; retry from Settings"
		if err = c.save(s); err != nil {
			return v, err
		}
		if _, err = Export(m.Root, c.Binary, data, c.Version); err == nil {
			err = c.writeEnvironment(m.Root, s.Generation)
		}
		if err == nil {
			_, err = m.Install(ctx, v.Installed)
		}
		if err == nil {
			items, e := m.inventory(ctx)
			err = e
			if err == nil {
				err = m.verifyInstalled(items)
			}
			if err == nil && len(items) != 1 {
				err = fmt.Errorf("Plugin registration could not be verified")
			}
			if err == nil {
				var item struct {
					Enabled bool `json:"enabled"`
				}
				if json.Unmarshal(items[0], &item) != nil || !item.Enabled {
					err = fmt.Errorf("Plugin is registered but disabled; enable it in ChatGPT and retry")
				}
			}
		}
	}
	s.Error = ""
	if err != nil {
		s.Error = err.Error()
	}
	if saveErr := c.save(s); saveErr != nil {
		return v, saveErr
	}
	return c.Status(ctx), err
}

func randomGeneration() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (c Connection) writeEnvironment(root, generation string) error {
	path := filepath.Join(root, "plugins/teamcross/.mcp.json")
	var data struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &data); err != nil {
		return err
	}
	data.Servers["teamcross-ui"]["env"] = map[string]string{"TEAMCROSS_PLUGIN_PROFILE": c.Home, "TEAMCROSS_PLUGIN_GENERATION": generation}
	return jsonFile(path, data)
}

// MarkUILoaded is called after the embedded WebGUI bootstrap succeeds, not at
// MCP initialize or during a CLI health probe. Old cached processes carry an
// older generation and cannot acknowledge a newer installation.
func MarkUILoaded(dataDir string) error {
	home, generation := os.Getenv("TEAMCROSS_PLUGIN_PROFILE"), os.Getenv("TEAMCROSS_PLUGIN_GENERATION")
	if home == "" || generation == "" {
		return nil
	}
	c := Connection{Home: home}
	s, err := c.read()
	if err != nil {
		return err
	}
	if !s.Enabled || s.Generation != generation || s.DataDir != dataDir {
		return nil
	}
	if err = safePath(c.dir(), "loaded.json"); err != nil {
		return err
	}
	return jsonFile(filepath.Join(c.dir(), "loaded.json"), map[string]any{"generation": generation, "loadedAt": time.Now()})
}

// CLIForConnection accepts a trusted explicit command, otherwise requires a
// detected desktop installation. No package-manager or download side effects.
func CLIForConnection(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return exec.LookPath(explicit)
	}
	return DesktopCLI(), nil
}
