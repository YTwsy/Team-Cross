package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"teamcross/internal/platform"
	"teamcross/internal/server"
)

var (
	version = "0.1.0-dev"
	commit  = "none"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "teamcross:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return flag.ErrHelp
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "join":
		return runJoin(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("teamcross %s (%s)\n", version, commit)
		return nil
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage() {
	fmt.Print(`Team Cross — portable local development handoffs

Usage:
  teamcross serve --repo .
  teamcross join <tcx1.invitation>
  teamcross doctor
  teamcross version

The host WebGUI captures Git state into an isolated worktree. A join proxy
automatically connects over LAN, then Tailnet, then Tailcat.
`)
}

func runServe(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	repo := flags.String("repo", ".", "Git repository to expose to the local host UI")
	listen := flags.String("listen", "127.0.0.1:43110", "loopback WebGUI listen address")
	dataDir := flags.String("data-dir", "", "override the Team Cross application data directory")
	devWeb := flags.String("dev-web", "", "proxy WebGUI assets to a Vite development server")
	noOpen := flags.Bool("no-open", false, "do not open the browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("serve accepts flags only")
	}
	if !isLoopback(*listen) {
		return fmt.Errorf("local administrator API must listen on loopback, got %s", *listen)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := server.Open(ctx, server.Config{Repo: *repo, DataDir: *dataDir, Version: version, DevWeb: *devWeb, Logger: slog.Default()})
	if err != nil {
		return err
	}
	defer app.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	httpServer := &http.Server{
		Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- httpServer.Serve(listener) }()
	url := "http://" + listener.Addr().String()
	fmt.Printf("Team Cross host: %s\nRepository: %s\n", url, *repo)
	if !*noOpen {
		if err := platform.OpenBrowser(url); err != nil {
			slog.Warn("open browser", "error", err)
		}
	}
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdown)
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runJoin(args []string) error {
	flags := flag.NewFlagSet("join", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:0", "local join-proxy listen address")
	name := flags.String("name", defaultName(), "collaborator name shown to the host")
	devWeb := flags.String("dev-web", "", "proxy WebGUI assets to a Vite development server")
	noOpen := flags.Bool("no-open", false, "do not open the browser")
	var invitation string
	flagArgs := args
	// Keep the documented `teamcross join <invite> [flags]` form while also
	// accepting the conventional `teamcross join [flags] <invite>` form.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		invitation = args[0]
		flagArgs = args[1:]
	}
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	remaining := flags.NArg()
	if invitation == "" && remaining == 1 {
		invitation = flags.Arg(0)
		remaining = 0
	}
	if invitation == "" || remaining != 0 {
		return fmt.Errorf("join requires exactly one tcx1 invitation")
	}
	if !isLoopback(*listen) {
		return fmt.Errorf("join proxy must listen on loopback, got %s", *listen)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	proxy, err := server.StartJoinProxy(ctx, server.JoinConfig{Invitation: invitation, Name: *name, ListenAddress: *listen, DevWeb: *devWeb})
	if err != nil {
		return err
	}
	defer proxy.Close()
	fmt.Printf("Connected via %s\nTeam Cross join proxy: %s\n", proxy.Transport(), proxy.URL())
	if !*noOpen {
		if err := platform.OpenBrowser(proxy.URL()); err != nil {
			slog.Warn("open browser", "error", err)
		}
	}
	<-ctx.Done()
	return nil
}

func runDoctor(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository used for configuration context")
	dataDir := flags.String("data-dir", "", "override application data directory")
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON")
	probeTailcat := flags.Bool("tailcat", true, "perform a live ephemeral Tailcat initialization")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	app, err := server.Open(ctx, server.Config{Repo: *repo, DataDir: *dataDir, Version: version})
	if err != nil {
		return err
	}
	defer app.Close()
	checks := app.Doctor(ctx, *probeTailcat)
	if *jsonOutput {
		encoded, _ := json.MarshalIndent(checks, "", "  ")
		fmt.Println(string(encoded))
	} else {
		for _, check := range checks {
			mark := "✓"
			if check.Status == "warning" {
				mark = "!"
			}
			if check.Status == "error" {
				mark = "×"
			}
			fmt.Printf("%s %-26s %s\n", mark, check.Label, check.Detail)
		}
	}
	for _, check := range checks {
		if check.Status == "error" {
			return fmt.Errorf("required doctor checks failed")
		}
	}
	return nil
}

func isLoopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func defaultName() string {
	if current, err := user.Current(); err == nil && strings.TrimSpace(current.Name) != "" {
		return current.Name
	}
	if host, err := os.Hostname(); err == nil {
		return host
	}
	return "Collaborator"
}
