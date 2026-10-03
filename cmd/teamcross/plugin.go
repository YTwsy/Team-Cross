package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"teamcross/internal/buildinfo"
	"teamcross/internal/collab"
	"teamcross/internal/pluginpack"
	"teamcross/internal/service"
	"time"
)

func runPlugin(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("Usage: teamcross plugin export|install|upgrade|status|remove [--plugin-dir DIR] [--data-dir DIR] [--codex-bin PATH]")
	}
	action, args := args[0], args[1:]
	flags := flag.NewFlagSet("plugin "+action, flag.ContinueOnError)
	root := flags.String("plugin-dir", pluginpack.DefaultRoot(), "Owned local marketplace package")
	data := flags.String("data-dir", collab.DefaultDataDir(), "Local Core data directory")
	codex := flags.String("codex-bin", "", "Native Codex CLI executable")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("Unexpected plugin arguments")
	}
	switch action {
	case "export", "install", "upgrade", "status", "remove":
	default:
		return fmt.Errorf("Unknown plugin action %s", action)
	}
	var err error
	*root, err = filepath.Abs(*root)
	if err != nil {
		return err
	}
	if action == "export" || action == "install" || action == "upgrade" {
		explicitData := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "data-dir" {
				explicitData = true
			}
		})
		if !explicitData {
			previous, e := pluginpack.Inspect(*root)
			if e == nil {
				*data = previous.DataDir
			} else if !os.IsNotExist(e) {
				return e
			}
		}
		*data, err = service.Normalize(*data)
		if err != nil {
			return err
		}
		binary, e := os.Executable()
		if e != nil {
			return e
		}
		p, e := pluginpack.Export(*root, binary, *data, buildinfo.Version)
		if e != nil {
			return e
		}
		if action == "export" {
			return printJSON(p)
		}
	}
	if *codex == "" {
		*codex, err = exec.LookPath("codex")
		if err != nil {
			const desktop = "/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"
			if _, e := os.Stat(desktop); e != nil {
				return err
			}
			*codex = desktop
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	m := pluginpack.Manager{Codex: *codex, Root: *root}
	var result any
	switch action {
	case "install", "upgrade":
		result, err = m.Install(ctx, action == "upgrade")
	case "remove":
		result, err = m.Remove(ctx)
	case "status":
		result, err = m.Status(ctx)
	default:
		return fmt.Errorf("Unknown plugin action %s", action)
	}
	if err != nil {
		return err
	}
	return printJSON(result)
}
