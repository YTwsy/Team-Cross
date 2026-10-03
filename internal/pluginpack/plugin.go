// Package pluginpack distributes the local plugin without HTTP hosting or a
// separate language runtime. Config mutations are limited to our named source.
package pluginpack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const Name = "teamcross"
const Marketplace = "teamcross-local"
const Selector = Name + "@" + Marketplace
const marker = ".teamcross-plugin.json"

type Package struct {
	Format  int    `json:"format"`
	Root    string `json:"root"`
	DataDir string `json:"dataDir"`
	Version string `json:"version"`
	Binary  string `json:"binary"`
}

func DefaultRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "TeamCross", "plugins", "local")
}
func owned(root string) (Package, error) {
	var p Package
	if e := safePath(root, marker); e != nil {
		return p, e
	}
	b, e := os.ReadFile(filepath.Join(root, marker))
	if e != nil {
		return p, e
	}
	e = json.Unmarshal(b, &p)
	if e != nil || p.Format != 1 || p.Root != root {
		return p, fmt.Errorf("Not a Team Cross plugin package: %s", root)
	}
	return p, nil
}

// Inspect reads only this package's marker, including its existing Core binding.
func Inspect(root string) (Package, error) { return owned(filepath.Clean(root)) }

// Inspect every existing descendant before writing; a package-owned path must
// never redirect an upgrade outside the package through a nested symlink.
func safePath(root, relative string) error {
	path := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Refusing plugin symlink: %s", path)
		}
	}
	return nil
}
func atomic(path string, data []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".plugin-stage-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(data); e == nil {
		e = f.Chmod(mode)
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func jsonFile(path string, value any) error {
	b, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	return atomic(path, append(b, '\n'), 0600)
}

// Export updates only the exact files in an owned package; foreign directories
// and symlink roots are rejected. The stable launcher path survives cache copies.
func Export(root, binary, dataDir, version string) (Package, error) {
	var p Package
	var e error
	root, e = filepath.Abs(root)
	if e != nil {
		return p, e
	}
	root = filepath.Clean(root)
	if info, err := os.Lstat(root); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return p, fmt.Errorf("Plugin root must be an owned directory")
		}
		if _, e = owned(root); e != nil {
			return p, e
		}
	} else if !os.IsNotExist(err) {
		return p, err
	}
	binary, e = filepath.Abs(binary)
	if e != nil {
		return p, e
	}
	source, e := os.Open(binary)
	if e != nil {
		return p, e
	}
	defer source.Close()
	info, e := source.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return p, fmt.Errorf("Plugin binary must be executable")
	}
	for _, relative := range []string{marker, "runtime/teamcross", "plugins/teamcross/plugin.json", "plugins/teamcross/mcp.json", "plugins/teamcross/.codex-plugin/plugin.json", "plugins/teamcross/.mcp.json", ".agents/plugins/marketplace.json"} {
		if e = safePath(root, relative); e != nil {
			return p, e
		}
	}
	runtime := filepath.Join(root, "runtime", "teamcross")
	p = Package{1, root, dataDir, version, runtime}
	// A failed first export remains explicitly owned and can be retried safely.
	if _, e = os.Stat(root); os.IsNotExist(e) {
		if e = jsonFile(filepath.Join(root, marker), p); e != nil {
			return p, e
		}
	}
	if e = os.MkdirAll(filepath.Join(root, "runtime"), 0700); e != nil {
		return p, e
	}
	f, e := os.CreateTemp(filepath.Join(root, "runtime"), ".teamcross-")
	if e != nil {
		return p, e
	}
	defer os.Remove(f.Name())
	_, e = io.Copy(f, source)
	if e == nil {
		e = f.Chmod(0700)
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return p, e
	}
	if e = os.Rename(f.Name(), runtime); e != nil {
		return p, e
	}
	plugin := filepath.Join(root, "plugins", Name)
	interfaceInfo := map[string]any{"displayName": "Team Cross", "shortDescription": "Your Team Cross workspace", "category": "Developer Tools", "capabilities": []string{"Read", "Write"}}
	// Use the documented compatibility package accepted by the installed native
	// desktop CLI. A portable manifest takes precedence on this version but its
	// stdio component is not loaded. Never publish an apparently installed shell.
	description := "Use the complete Team Cross WebGUI with your local Core. Mention published materials or annotations in the composer, or bring selected references into this conversation."
	server := map[string]any{"command": runtime, "args": []string{"mcp", "--ui", "--data-dir", dataDir}}
	files := map[string]any{
		filepath.Join(plugin, ".codex-plugin", "plugin.json"):         map[string]any{"name": Name, "version": version, "description": description, "mcpServers": "./.mcp.json", "interface": interfaceInfo},
		filepath.Join(plugin, ".mcp.json"):                            map[string]any{"mcpServers": map[string]any{"teamcross-ui": server}},
		filepath.Join(root, ".agents", "plugins", "marketplace.json"): map[string]any{"name": Marketplace, "interface": map[string]any{"displayName": "Team Cross Local"}, "plugins": []any{map[string]any{"name": Name, "source": map[string]any{"source": "local", "path": "./plugins/teamcross"}, "policy": map[string]any{"installation": "AVAILABLE", "authentication": "ON_INSTALL"}, "category": "Developer Tools"}}},
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		value := files[path]
		if e = jsonFile(path, value); e != nil {
			return p, e
		}
	}
	// Exact generated files from earlier exports of this owned package only.
	for _, path := range []string{filepath.Join(plugin, "plugin.json"), filepath.Join(plugin, "mcp.json")} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return p, err
		}
	}
	if e = jsonFile(filepath.Join(root, marker), p); e != nil {
		return p, e
	}
	return p, nil
}

type Manager struct {
	Codex string
	Root  string
	Home  string // Optional isolated native profile; never a project or shared-session home.
}

func (m Manager) command(ctx context.Context, args ...string) (json.RawMessage, error) {
	command := exec.CommandContext(ctx, m.Codex, append([]string{"plugin"}, append(args, "--json")...)...)
	command.Dir = m.Root
	if m.Home != "" {
		command.Dir = os.TempDir()
		command.Env = append(os.Environ(), "CODEX_HOME="+m.Home)
	} else if _, err := os.Stat(m.Root); err != nil {
		command.Dir = os.TempDir()
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	b, e := command.Output()
	if e != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = e.Error()
		}
		return nil, fmt.Errorf("Codex plugin command failed: %s", message)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("Invalid Codex plugin response")
	}
	return b, nil
}
func (m Manager) source(ctx context.Context) (bool, error) {
	b, e := m.command(ctx, "marketplace", "list")
	if e != nil {
		return false, e
	}
	var data struct {
		Marketplaces []struct {
			Name              string `json:"name"`
			Root              string `json:"root"`
			MarketplaceSource struct {
				Source string `json:"source"`
			} `json:"marketplaceSource"`
		} `json:"marketplaces"`
	}
	if e = json.Unmarshal(b, &data); e != nil {
		return false, e
	}
	for _, item := range data.Marketplaces {
		if item.Name == Marketplace {
			if item.Root != m.Root && item.MarketplaceSource.Source != m.Root {
				return false, fmt.Errorf("Marketplace name belongs to another source")
			}
			return true, nil
		}
	}
	return false, nil
}
func (m Manager) inventory(ctx context.Context) ([]json.RawMessage, error) {
	b, e := m.command(ctx, "list", "--marketplace", Marketplace)
	if e != nil {
		return nil, e
	}
	var list struct {
		Installed []json.RawMessage `json:"installed"`
	}
	e = json.Unmarshal(b, &list)
	return list.Installed, e
}
func (m Manager) verifyInstalled(items []json.RawMessage) error {
	for _, raw := range items {
		var item struct {
			PluginID string `json:"pluginId"`
			Source   struct {
				Path string `json:"path"`
			} `json:"source"`
		}
		if json.Unmarshal(raw, &item) != nil || item.PluginID != Selector || item.Source.Path != filepath.Join(m.Root, "plugins", Name) {
			return fmt.Errorf("Refusing to modify an unrelated plugin")
		}
	}
	return nil
}
func (m Manager) Install(ctx context.Context, refresh bool) (any, error) {
	if _, e := owned(m.Root); e != nil {
		return nil, e
	}
	exists, e := m.source(ctx)
	if e != nil {
		return nil, e
	}
	if !exists {
		if _, e = m.command(ctx, "marketplace", "add", m.Root); e != nil {
			return nil, e
		}
	}
	items, e := m.inventory(ctx)
	if e != nil {
		return nil, e
	}
	if e = m.verifyInstalled(items); e != nil {
		return nil, e
	}
	// The installed native CLI has no plugin upgrade subcommand. Refresh only our
	// verified ID via remove/add; no global configuration snapshot is restored.
	if refresh && len(items) > 0 {
		if _, e = m.command(ctx, "remove", Selector); e != nil {
			return nil, e
		}
	}
	return m.command(ctx, "add", Selector)
}
func (m Manager) Remove(ctx context.Context) (any, error) {
	exists, e := m.source(ctx)
	if e != nil {
		return nil, e
	}
	if !exists {
		return map[string]bool{"removed": true}, nil
	}
	items, e := m.inventory(ctx)
	if e != nil {
		return nil, e
	}
	if e = m.verifyInstalled(items); e != nil {
		return nil, e
	}
	for range items {
		if _, e = m.command(ctx, "remove", Selector); e != nil {
			return nil, e
		}
	}
	return m.command(ctx, "marketplace", "remove", Marketplace)
}
func (m Manager) Status(ctx context.Context) (any, error) {
	_, e := m.source(ctx)
	if e != nil {
		return nil, e
	}
	return m.command(ctx, "list", "--marketplace", Marketplace)
}
