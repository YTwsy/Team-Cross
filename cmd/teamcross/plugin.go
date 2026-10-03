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
		return fmt.Errorf("Usage: teamcross plugin export|install|upgrade|status|remove|connection-status|connect|sync|disconnect [--plugin-dir DIR] [--data-dir DIR] [--codex-bin PATH]")
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
	home, err := pluginpack.NativeHome()
	if err != nil {
		return err
	}
	explicitRoot := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "plugin-dir" {
			explicitRoot = true
		}
	})
	// Preserve manual installation behavior, while commands targeting an opted-in
	// package use the same lock, binding and load receipt as Settings.
	if action != "export" {
		managedRoot, enabled, e := pluginpack.ManagedPackage(home)
		if e != nil {
			return e
		}
		if !explicitRoot && managedRoot != "" {
			*root = managedRoot
		}
		absolute, e := filepath.Abs(*root)
		if e != nil {
			return e
		}
		if explicitRoot && managedRoot != "" && absolute != managedRoot && (action == "connect" || action == "sync" || action == "disconnect" || action == "connection-status") {
			return fmt.Errorf("This profile already manages another plugin source")
		}
		if enabled && absolute == managedRoot {
			switch action {
			case "install", "upgrade":
				action = "connect"
			case "remove":
				action = "disconnect"
			}
		}
	}
	if action == "connection-status" || action == "connect" || action == "sync" || action == "disconnect" {
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		binary = service.StableExecutable(binary)
		dataDir, err := service.Normalize(*data)
		if err != nil {
			return err
		}
		pluginRoot, err := filepath.Abs(*root)
		if err != nil {
			return err
		}
		if !explicitRoot {
			pluginRoot = filepath.Join(home, "teamcross-plugin", "package")
		}
		cli, err := pluginpack.CLIForConnection(*codex)
		if err != nil {
			return err
		}
		c := pluginpack.Connection{Home: home, Codex: cli, Binary: binary, Root: pluginRoot, DataDir: dataDir, Version: buildinfo.Version}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if action == "connection-status" {
			return printJSON(c.Status(ctx))
		}
		result, err := c.Apply(ctx, action)
		if err != nil {
			return err
		}
		return printJSON(result)
	}
	switch action {
	case "export", "install", "upgrade", "status", "remove":
	default:
		return fmt.Errorf("Unknown plugin action %s", action)
	}
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
