package pluginpack

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func connectionFixture(t *testing.T) (Connection, string) {
	t.Helper()
	dir := t.TempDir()
	c := Connection{Home: filepath.Join(dir, "home"), Root: filepath.Join(dir, "package"), Binary: filepath.Join(dir, "teamcross"), Codex: filepath.Join(dir, "codex"), DataDir: filepath.Join(dir, "data"), Version: "1.0-dev"}
	os.MkdirAll(c.Home, 0700)
	os.WriteFile(c.Binary, []byte("#!/bin/sh\nexit 0\n"), 0700)
	jsonFile(filepath.Join(dir, "market"), map[string]any{"marketplaces": []any{}})
	jsonFile(filepath.Join(dir, "installed"), map[string]any{"installed": []any{}})
	setCandidate(t, dir, c.Root, true)
	// Test-only native CLI model: registration persists independently of the
	// package. Failure after remove reproduces an interrupted refresh.
	script := `#!/bin/sh
set -eu
cd '` + dir + `'
printf '%s\n' "$*" >> calls
case "$1 $2 $3" in
  'plugin marketplace list') cat market;;
  'plugin marketplace add') cp candidate-market market; printf '{}';;
  'plugin marketplace remove') printf '{"marketplaces":[]}' > market; printf '{}';;
  'plugin list --marketplace') cat installed;;
  'plugin add teamcross@teamcross-local')
    if test -f fail-add; then printf 'injected add failure' >&2; exit 1; fi
    cp candidate-installed installed; printf '{}';;
  'plugin remove teamcross@teamcross-local') printf '{"installed":[]}' > installed; printf '{}';;
  *) exit 1;;
esac
`
	os.WriteFile(c.Codex, []byte(script), 0700)
	return c, dir
}

func setCandidate(t *testing.T, dir, root string, enabled bool) {
	t.Helper()
	if err := jsonFile(filepath.Join(dir, "candidate-market"), map[string]any{"marketplaces": []any{map[string]any{"name": Marketplace, "root": root, "marketplaceSource": map[string]string{"sourceType": "local", "source": root}}}}); err != nil {
		t.Fatal(err)
	}
	if err := jsonFile(filepath.Join(dir, "candidate-installed"), map[string]any{"installed": []any{map[string]any{"pluginId": Selector, "enabled": enabled, "version": "1.0-dev", "source": map[string]string{"path": filepath.Join(root, "plugins", Name)}}}}); err != nil {
		t.Fatal(err)
	}
}

func copyFixture(t *testing.T, dir, from, to string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, from))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, to), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionOptInUpgradeAndUILoadReceipt(t *testing.T) {
	c, dir := connectionFixture(t)
	ctx := context.Background()
	v, err := c.Apply(ctx, "sync")
	if err != nil || v.State != "not_enabled" {
		t.Fatal(v, err)
	}
	if _, err = os.Stat(c.dir()); !os.IsNotExist(err) {
		t.Fatal("startup wrote before opt-in")
	}
	if _, err = os.Stat(filepath.Join(dir, "calls")); !os.IsNotExist(err) {
		t.Fatal("startup ran native CLI before opt-in")
	}
	v, err = c.Apply(ctx, "connect")
	if err != nil || !v.Installed || !v.AutoUpdate || !v.ReloadRequired {
		t.Fatal(v, err)
	}
	first, _ := c.read()
	t.Setenv("TEAMCROSS_PLUGIN_PROFILE", c.Home)
	t.Setenv("TEAMCROSS_PLUGIN_GENERATION", first.Generation)
	if err = MarkUILoaded("/other-data"); err != nil {
		t.Fatal(err)
	}
	if !c.Status(ctx).ReloadRequired {
		t.Fatal("wrong Core acknowledged")
	}
	if err = MarkUILoaded(c.DataDir); err != nil {
		t.Fatal(err)
	}
	if v = c.Status(ctx); v.State != "installed" || v.ReloadRequired {
		t.Fatal(v)
	}
	// Same marketing version, different bytes: App update still refreshes.
	os.WriteFile(c.Binary, []byte("#!/bin/sh\nexit 2\n"), 0700)
	if v = c.Status(ctx); v.State != "update_available" {
		t.Fatal(v)
	}
	v, err = c.Apply(ctx, "sync")
	if err != nil || !v.ReloadRequired {
		t.Fatal(v, err)
	}
	second, _ := c.read()
	if first.Generation == second.Generation {
		t.Fatal("generation was reused")
	}
	MarkUILoaded(c.DataDir) // the old cached process still has its old environment
	if !c.Status(ctx).ReloadRequired {
		t.Fatal("old UI acknowledged new install")
	}
	t.Setenv("TEAMCROSS_PLUGIN_GENERATION", second.Generation)
	MarkUILoaded(c.DataDir)
	if c.Status(ctx).ReloadRequired {
		t.Fatal("new UI was not observed")
	}
	before, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if _, err = c.Apply(ctx, "sync"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(after[len(before):]), "plugin add") || strings.Contains(string(after[len(before):]), "plugin remove") {
		t.Fatal("unchanged startup rewrote plugin")
	}
	if _, err = c.Apply(ctx, "disconnect"); err != nil {
		t.Fatal(err)
	}
	if v = c.Status(ctx); v.AutoUpdate || v.Installed {
		t.Fatal(v)
	}
	if _, err = Inspect(c.Root); err != nil {
		t.Fatal("disconnect removed package", err)
	}
}

func TestConnectionAdoptsExistingSourceAndBindingAndRepairsFailure(t *testing.T) {
	c, dir := connectionFixture(t)
	customRoot, customData := filepath.Join(dir, "custom package"), filepath.Join(dir, "original data")
	if _, err := Export(customRoot, c.Binary, customData, c.Version); err != nil {
		t.Fatal(err)
	}
	setCandidate(t, dir, customRoot, true)
	copyFixture(t, dir, "candidate-market", "market")
	copyFixture(t, dir, "candidate-installed", "installed")
	v, err := c.Apply(context.Background(), "connect")
	if err != nil || v.Root != customRoot || v.DataDir != customData || !v.DifferentData {
		t.Fatal(v, err)
	}
	os.WriteFile(c.Binary, []byte("#!/bin/sh\nexit 1\n"), 0700)
	os.WriteFile(filepath.Join(dir, "fail-add"), nil, 0600)
	if _, err = c.Apply(context.Background(), "sync"); err == nil {
		t.Fatal("failure was hidden")
	}
	if v = c.Status(context.Background()); v.State != "error" || v.Installed {
		t.Fatal(v)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "calls"))
	c.Apply(context.Background(), "sync")
	after, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Contains(string(after[len(before):]), "plugin add") {
		t.Fatal("auto reinstalled removed plugin")
	}
	os.Remove(filepath.Join(dir, "fail-add"))
	v, err = c.Apply(context.Background(), "connect")
	if err != nil || !v.Installed || v.DataDir != customData || v.Root != customRoot {
		t.Fatal(v, err)
	}
}

func TestConnectionRejectsConflictsBeforeExportAndDoesNotReenable(t *testing.T) {
	c, dir := connectionFixture(t)
	foreign := filepath.Join(dir, "foreign")
	os.MkdirAll(foreign, 0700)
	os.WriteFile(filepath.Join(foreign, "keep"), []byte("untouched"), 0600)
	setCandidate(t, dir, foreign, true)
	copyFixture(t, dir, "candidate-market", "market")
	if _, err := c.Apply(context.Background(), "connect"); err == nil {
		t.Fatal("foreign source accepted")
	}
	if _, err := os.Stat(c.Root); !os.IsNotExist(err) {
		t.Fatal("export preceded conflict check")
	}
	if _, err := os.Stat(filepath.Join(foreign, marker)); !os.IsNotExist(err) {
		t.Fatal("foreign source modified")
	}
	jsonFile(filepath.Join(dir, "market"), map[string]any{"marketplaces": []any{}})
	setCandidate(t, dir, c.Root, true)
	if _, err := c.Apply(context.Background(), "connect"); err != nil {
		t.Fatal(err)
	}
	setCandidate(t, dir, c.Root, false)
	copyFixture(t, dir, "candidate-installed", "installed")
	v, err := c.Apply(context.Background(), "sync")
	if err != nil || v.State != "disconnected" || v.PluginEnabled {
		t.Fatal(v, err)
	}
	setCandidate(t, dir, foreign, true)
	copyFixture(t, dir, "candidate-market", "market")
	if _, err = c.Apply(context.Background(), "sync"); err == nil {
		t.Fatal("auto followed replaced source")
	}
}

func TestConnectionLockSymlinkAndManagementSource(t *testing.T) {
	c, _ := connectionFixture(t)
	if _, err := c.Apply(context.Background(), "connect"); err != nil {
		t.Fatal(err)
	}
	p, _ := Inspect(c.Root)
	if got, err := ManagementBinary(c.Home, p.Binary); err != nil || got != c.Binary {
		t.Fatal(got, err)
	}
	f, err := os.OpenFile(filepath.Join(c.dir(), "lifecycle.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Apply(context.Background(), "connect"); err == nil {
		t.Fatal("concurrent write accepted")
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	os.Remove(filepath.Join(c.dir(), "connection.json"))
	out := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(out, []byte("keep"), 0600)
	os.Symlink(out, filepath.Join(c.dir(), "connection.json"))
	if _, err = c.Apply(context.Background(), "connect"); err == nil {
		t.Fatal("state symlink accepted")
	}
	b, _ := os.ReadFile(out)
	if string(b) != "keep" {
		t.Fatal("outside state changed")
	}
}

func TestConnectionManifestUsesGenerationAndProfile(t *testing.T) {
	c, _ := connectionFixture(t)
	if _, err := c.Apply(context.Background(), "connect"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(c.Root, "plugins/teamcross/.mcp.json"))
	var manifest struct {
		Servers map[string]struct{ Env map[string]string } `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	s, _ := c.read()
	if manifest.Servers["teamcross-ui"].Env["TEAMCROSS_PLUGIN_GENERATION"] != s.Generation || manifest.Servers["teamcross-ui"].Env["TEAMCROSS_PLUGIN_PROFILE"] != c.Home {
		t.Fatal(string(b))
	}
}

func TestConnectionStatusDoesNotCreateMissingProfile(t *testing.T) {
	c, dir := connectionFixture(t)
	c.Home = filepath.Join(dir, "never-opened-desktop")
	if v := c.Status(context.Background()); v.State != "not_installed" || !v.Available {
		t.Fatal(v)
	}
	if _, err := os.Stat(c.Home); !os.IsNotExist(err) {
		t.Fatal("status created a profile")
	}
	if _, err := os.Stat(filepath.Join(dir, "calls")); !os.IsNotExist(err) {
		t.Fatal("inventory invoked on missing profile")
	}
}
