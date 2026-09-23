// Package service is the shared user-session launcher used by the CLI, App and MCP.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"teamcross/internal/buildinfo"
	"teamcross/internal/problem"
	"time"
)

type Connection struct {
	URL      string `json:"url"`
	PID      int    `json:"pid"`
	Instance string `json:"instance"`
	Token    string `json:"token,omitempty"`
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Protocol int    `json:"protocol"`
	DataDir  string `json:"dataDir"`
}
type Status struct {
	Connection
	Running          bool   `json:"running"`
	Active           int    `json:"active"`
	UILanguage       string `json:"uiLanguage,omitempty"`
	ResolvedLanguage string `json:"resolvedLanguage,omitempty"`
}

func Normalize(path string) (string, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	// Resolve existing ancestors too, including when a new data directory is requested.
	if resolved, e := filepath.EvalSymlinks(abs); e == nil {
		return resolved, nil
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return abs, nil
	}
	resolved, e := Normalize(parent)
	if e != nil {
		return "", e
	}
	return filepath.Join(resolved, filepath.Base(abs)), nil
}
func Read(data string) (Connection, error) {
	var c Connection
	b, e := os.ReadFile(filepath.Join(data, "connection.json"))
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	u, e := url.Parse(c.URL)
	if e != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("本地连接文件无效")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || u.Port() == "" {
		return c, fmt.Errorf("本地连接地址无效")
	}
	if c.Instance == "" || c.Token == "" || c.DataDir != data {
		return c, problem.New("instance_mismatch", "本机服务连接信息不匹配", "请退出旧版服务后重新启动")
	}
	return c, nil
}
func (c Connection) Call(ctx context.Context, method, path string, input, out any) error {
	var body io.Reader
	if input != nil {
		b, e := json.Marshal(input)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, c.URL+"/api/"+path, body)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	client := &http.Client{Timeout: 50 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var p problem.Error
		if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&p) != nil || p.Message == "" {
			return fmt.Errorf("本机服务返回 %d", res.StatusCode)
		}
		return &p
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(out)
}
func Probe(ctx context.Context, data string) (Status, error) {
	var s Status
	c, e := Read(data)
	if e != nil {
		return s, e
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	if e = c.Call(ctx, "GET", "control/status", nil, &s); e != nil {
		return s, e
	}
	if s.Instance != c.Instance || s.DataDir != data || s.PID != c.PID {
		return Status{}, problem.New("instance_mismatch", "连接的不是记录中的 Team Cross 实例", "请检查诊断信息")
	}
	if s.Protocol != buildinfo.ControlProtocol {
		return s, problem.New("version_incompatible", "正在运行的 Team Cross 控制协议不兼容", "请从原版本退出服务，再打开当前版本")
	}
	s.Token = c.Token
	return s, nil
}
func Save(data string, c Connection) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(data, "connection-*.tmp")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filepath.Join(data, "connection.json"))
}
func Lock(data, name string) (*os.File, error) {
	f, e := os.OpenFile(filepath.Join(data, name), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
func Ensure(ctx context.Context, data, executable string, args []string) (Status, error) {
	data, e := Normalize(data)
	if e != nil {
		return Status{}, e
	}
	if e = os.MkdirAll(data, 0700); e != nil {
		return Status{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var lock *os.File
	for {
		lock, e = Lock(data, "start.lock")
		if e == nil {
			break
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) {
			return Status{}, e
		}
		select {
		case <-ctx.Done():
			return Status{}, fmt.Errorf("等待服务启动超时")
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer lock.Close()
	if s, e := Probe(ctx, data); e == nil {
		return s, nil
	} else {
		var p *problem.Error
		if errors.As(e, &p) {
			return Status{}, e
		}
	}
	if executable == "" {
		executable, e = os.Executable()
		if e != nil {
			return Status{}, e
		}
	}
	log, e := os.OpenFile(filepath.Join(data, "core.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return Status{}, e
	}
	defer log.Close()
	argv := append([]string{"serve", "--foreground", "--no-open", "--data-dir", data}, args...)
	cmd := exec.Command(executable, argv...)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Dir = data
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if e = cmd.Start(); e != nil {
		return Status{}, e
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	for {
		if s, e := Probe(ctx, data); e == nil {
			return s, nil
		}
		select {
		case e := <-exited:
			return Status{}, fmt.Errorf("服务未能启动 (%v)，请查看 %s", e, filepath.Join(data, "core.log"))
		case <-ctx.Done():
			return Status{}, fmt.Errorf("服务启动超时，请查看 %s", filepath.Join(data, "core.log"))
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// StableExecutable avoids persisting a versioned Homebrew Cellar path in Codex config.
func StableExecutable(executable string) string {
	resolved := executable
	if target, e := filepath.EvalSymlinks(executable); e == nil {
		resolved = target
	}
	for _, formula := range []string{"teamcross", "teamcross-rc"} {
		marker := "/Cellar/" + formula + "/"
		if i := strings.Index(resolved, marker); i >= 0 {
			p := filepath.Join(resolved[:i], "opt", formula, "bin/teamcross")
			if _, e := os.Stat(p); e == nil {
				return p
			}
		}
	}
	if strings.HasSuffix(resolved, ".app/Contents/Resources/teamcross") {
		return resolved
	}
	return executable
}

// InstalledVersion checks our own executable on disk, not an arbitrary PATH command.
func InstalledVersion(ctx context.Context, executable string) string {
	if filepath.Base(executable) != "teamcross" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, executable, "version", "--json").Output()
	var value struct {
		Version string `json:"version"`
	}
	if err != nil || json.Unmarshal(out, &value) != nil {
		return ""
	}
	return value.Version
}
