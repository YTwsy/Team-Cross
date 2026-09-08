package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"teamcross/internal/collab"
	"teamcross/internal/mcp"
	"teamcross/internal/webassets"
	"time"
)

func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(args []string) error {
	command := "serve"
	if len(args) > 0 && (!strings.HasPrefix(args[0], "-") || args[0] == "--version") {
		command = args[0]
		args = args[1:]
	}
	if command == "version" || command == "--version" {
		fmt.Println("Team Cross 0.2.0-experimental")
		return nil
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	data := flags.String("data-dir", collab.DefaultDataDir(), "独立数据目录")
	repo := flags.String("repo", ".", "默认来源仓库")
	listen := flags.String("listen", "127.0.0.1:43210", "本机 Web 地址")
	noOpen := flags.Bool("no-open", false, "不自动打开浏览器")
	dev := flags.String("dev-web", "", "前端开发地址")
	loopback := flags.Bool("test-loopback", false, "邀请包含同机测试地址")
	binary := flags.String("codex-bin", "", "Codex CLI 路径")
	desktop := flags.String("desktop-app", "", "Codex Desktop 应用路径")
	if e := flags.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if command == "mcp" {
		return mcp.Serve(ctx, *data, os.Stdin, os.Stdout)
	}
	if command == "join" {
		if len(flags.Args()) != 1 {
			return fmt.Errorf("用法: teamcross join [--data-dir PATH] <邀请>")
		}
		backend, e := mcp.Connect(*data)
		if e != nil {
			return e
		}
		out, e := backend.Call(ctx, "POST", "join", map[string]string{"invitation": flags.Args()[0]})
		if e != nil {
			return e
		}
		var joined struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(out, &joined)
		target := backend.URL + "/#/collaborations/" + joined.ID
		fmt.Println(target)
		if !*noOpen {
			return exec.Command("open", target).Run()
		}
		return nil
	}
	if command != "serve" {
		return fmt.Errorf("用法: teamcross serve | join | mcp | version")
	}
	host, _, e := net.SplitHostPort(*listen)
	if e != nil {
		return e
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("管理服务必须监听 loopback 地址")
	}
	if e = os.MkdirAll(*data, 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(*data, "core.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return fmt.Errorf("该数据目录已有 Team Cross 在运行")
	}
	app, e := collab.Open(collab.Config{DataDir: *data, Repo: *repo, Loopback: *loopback, Binary: *binary, DesktopApp: *desktop})
	if e != nil {
		return e
	}
	defer app.Close()
	assets, _ := fs.Sub(webassets.Dist, "dist")
	var web http.Handler = http.FileServer(http.FS(assets))
	if *dev != "" {
		u, e := url.Parse(*dev)
		if e != nil {
			return e
		}
		web = httputil.NewSingleHostReverseProxy(u)
	}
	listener, e := net.Listen("tcp4", *listen)
	if e != nil {
		return e
	}
	app.URL = "http://" + listener.Addr().String()
	server := &http.Server{Handler: app.Handler(web), ReadHeaderTimeout: 10 * time.Second}
	connection, _ := json.Marshal(map[string]any{"url": app.URL, "pid": os.Getpid()})
	if e = os.WriteFile(filepath.Join(*data, "connection.json"), connection, 0600); e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Printf("Team Cross · %s\n数据目录: %s\n", app.URL, app.Config.DataDir)
	if !*noOpen {
		_ = exec.Command("open", app.URL).Start()
	}
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		return nil
	case e := <-done:
		return e
	}
}
