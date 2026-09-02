package platform

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const AppName = "Team Cross"

type Paths struct {
	Root      string
	Database  string
	Objects   string
	Worktrees string
	Contexts  string
}

func DataPaths(override string) (Paths, error) {
	root := override
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user home: %w", err)
		}
		if runtime.GOOS == "darwin" {
			root = filepath.Join(home, "Library", "Application Support", AppName)
		} else {
			config, err := os.UserConfigDir()
			if err != nil {
				return Paths{}, fmt.Errorf("resolve config directory: %w", err)
			}
			root = filepath.Join(config, "teamcross")
		}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve data directory: %w", err)
	}
	paths := Paths{
		Root:      absolute,
		Database:  filepath.Join(absolute, "teamcross.db"),
		Objects:   filepath.Join(absolute, "objects"),
		Worktrees: filepath.Join(absolute, "worktrees"),
		Contexts:  filepath.Join(absolute, "contexts"),
	}
	for _, directory := range []string{paths.Root, paths.Objects, paths.Worktrees, paths.Contexts} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return Paths{}, fmt.Errorf("create %s: %w", directory, err)
		}
	}
	return paths, nil
}

func OpenBrowser(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}

func WaitForHTTP(address string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 150*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		time.Sleep(30 * time.Millisecond)
	}
	return errors.New("listener did not become ready")
}
