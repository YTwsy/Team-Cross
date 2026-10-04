package service

import (
	"encoding/json"
	"net/http"
	"sync"
	"teamcross/internal/problem"
)

// Admission closes the gap between checking idleness and shutting down. An
// update never interrupts an admitted request, nor admits a write after draining.
type Admission struct {
	mu       sync.RWMutex
	draining bool
}

func (a *Admission) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		defer a.mu.RUnlock()
		if a.draining {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(problem.New("core_updating", "本机服务正在更新", "请稍后重新读取状态；不要自动重放写入"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Admission) DrainIfIdle(idle func() bool) bool {
	if !a.mu.TryLock() {
		return false
	}
	defer a.mu.Unlock()
	if a.draining || !idle() {
		return false
	}
	a.draining = true
	return true
}
