package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"teamcross/internal/collab"
	"teamcross/internal/mcpevents"
	"teamcross/internal/service"
)

func runEvents(args []string) error {
	flags := flag.NewFlagSet("events", flag.ContinueOnError)
	data := flags.String("data-dir", collab.DefaultDataDir(), "本机 Core 数据目录")
	listen := flags.String("listen", "127.0.0.1:43211", "独立事件网关地址，仅允许回环；HTTPS 发布由指定的反向代理负责")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("事件网关只允许绑定回环地址；不能公开本机 Core")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	s, err := service.Ensure(ctx, *data, "", nil)
	if err != nil {
		return err
	}
	handler := mcpevents.Gateway(func(ctx context.Context, credential string, raw json.RawMessage) (any, int, error) {
		var out struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		}
		err := s.Call(ctx, "POST", "event-gateway", map[string]any{"credential": credential, "request": raw}, &out)
		return out.Body, out.Status, err
	})
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	fmt.Printf("Team Cross events · http://%s/mcp · scoped event credentials required\n", listener.Addr())
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(stop)
	}
}
