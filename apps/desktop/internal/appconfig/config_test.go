package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"teamcross/internal/service"
)

func TestReleaseUsesExistingDirectoryAndPreviewCannotReachIt(t *testing.T) {
	home := t.TempDir()
	production := filepath.Join(home, "Library", "Application Support", "Team Cross Next")
	if err := os.MkdirAll(production, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(home, "alias")
	if err := os.Symlink(production, alias); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"", production, alias} {
		if _, err := Resolve("preview", data, home); err == nil {
			t.Fatal("preview accepted installed directory", data)
		}
		config, err := Resolve("release", data, home)
		canonical, _ := service.Normalize(production)
		if err != nil || config.Directory != canonical || config.Name != "Team Cross" {
			t.Fatal(config, err)
		}
	}
	isolation := filepath.Join(home, "test data")
	for _, profile := range []string{"preview", "release"} {
		if _, err := Resolve(profile, isolation, home); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(isolation); !os.IsNotExist(err) {
		t.Fatal("resolving configuration created data")
	}
}
