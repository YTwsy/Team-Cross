package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"teamcross/internal/problem"
	"time"
)

const UpgradeGrace = 10 * time.Second

// Admission owns a single, non-renewable upgrade deadline. New business calls
// cannot keep the old Core alive; already admitted writes may finish first.
type Admission struct {
	mu         sync.Mutex
	draining   bool
	finalizing bool
	deadline   time.Time
	next       uint64
	calls      map[uint64]admittedCall
}
type admittedCall struct {
	cancel  context.CancelFunc
	writing bool
}

func (a *Admission) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		completion := completionCall(r)
		a.mu.Lock()
		if a.draining && (a.finalizing || !completion) {
			a.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(problem.New("core_updating", "本机服务正在更新", "正在重新连接；请先核对已提交操作的结果，不要重复提交"))
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		a.next++
		id := a.next
		if a.calls == nil {
			a.calls = make(map[uint64]admittedCall)
		}
		writing := r.Method != http.MethodGet && r.Method != http.MethodHead && r.URL.Path != "/api/agent-receivers/poll"
		a.calls[id] = admittedCall{cancel, writing}
		a.mu.Unlock()
		defer func() { cancel(); a.mu.Lock(); delete(a.calls, id); a.mu.Unlock() }()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BeginUpgrade is idempotent even when callers disconnect or retry. The Core,
// rather than the requesting HTTP connection, owns the timer and shutdown.
func (a *Admission) BeginUpgrade(grace time.Duration, busy func() bool, stop func()) time.Time {
	a.mu.Lock()
	if a.draining {
		deadline := a.deadline
		a.mu.Unlock()
		return deadline
	}
	a.draining = true
	a.deadline = time.Now().Add(grace)
	deadline := a.deadline
	for _, call := range a.calls {
		if !call.writing {
			call.cancel()
		}
	}
	a.mu.Unlock()
	go func() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			active := busy()
			a.mu.Lock()
			writes := false
			for _, call := range a.calls {
				writes = writes || call.writing
			}
			if !writes && !active {
				a.finalizing = true
				a.mu.Unlock()
				break
			}
			a.mu.Unlock()
			select {
			case <-timer.C:
				goto finish
			case <-ticker.C:
			}
		}
	finish:
		a.mu.Lock()
		a.finalizing = true
		for _, call := range a.calls {
			call.cancel()
		}
		a.mu.Unlock()
		stop()
	}()
	return deadline
}
func (a *Admission) UpgradeDeadline() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deadline
}

// Only result acknowledgements can join a drain. Both the path and decoded
// method are checked; the original handler still enforces identity/permission.
func completionCall(r *http.Request) bool {
	if r.Method != http.MethodPost || r.Body == nil || (r.URL.Path != "/api/agent-tools/call" && !strings.HasPrefix(r.URL.Path, "/api/runtime-annotations/")) {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	r.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(body), r.Body), r.Body}
	if err != nil || len(body) > 1<<20 {
		return false
	}
	var call struct {
		Name string `json:"name"`
	}
	return json.Unmarshal(body, &call) == nil && call.Name == "finish_agent_request"
}
