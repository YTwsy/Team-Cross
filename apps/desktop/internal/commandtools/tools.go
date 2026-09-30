// Package commandtools manages only the App's terminal launcher. It neither
// starts Core nor elevates the desktop process or business runtime.
package commandtools

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"teamcross/internal/cliinstall"
	"teamcross/internal/problem"
)

type Manager struct {
	Helper, Directory, SearchPath string
}

func (m Manager) Status() cliinstall.Status {
	return cliinstall.Inspect(m.Helper, m.Directory, m.SearchPath)
}

func (m Manager) Change(ctx context.Context, remove, administrator bool) (cliinstall.Status, error) {
	if !administrator {
		if remove {
			return cliinstall.Uninstall(m.Helper, m.Directory, m.SearchPath)
		}
		return cliinstall.Install(m.Helper, m.Directory, m.SearchPath)
	}
	command, err := m.adminCommand(remove)
	if err != nil {
		return cliinstall.Status{}, err
	}
	// The AppleScript program is constant. Paths remain quoted shell arguments
	// passed in argv, including spaces, apostrophes and Unicode.
	output, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", "on run argv\nreturn do shell script (item 1 of argv) with administrator privileges\nend run", command).Output()
	if err != nil {
		return cliinstall.Status{}, errors.New("command change was not confirmed")
	}
	var failure problem.Error
	if json.Unmarshal(output, &failure) == nil && failure.Code != "" {
		return cliinstall.Status{}, &failure
	}
	var status cliinstall.Status
	if err := json.Unmarshal(output, &status); err != nil || status.Target == "" {
		return cliinstall.Status{}, errors.New("invalid command status")
	}
	return status, nil
}

func (m Manager) adminCommand(remove bool) (string, error) {
	if !filepath.IsAbs(m.Helper) || !strings.HasSuffix(m.Helper, ".app/Contents/Resources/teamcross") || !filepath.IsAbs(m.Directory) {
		return "", errors.New("invalid App command path")
	}
	operation := "install-cli"
	if remove {
		operation = "uninstall-cli"
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	return quote(m.Helper) + " " + operation + " --json --cli-dir " + quote(m.Directory), nil
}
