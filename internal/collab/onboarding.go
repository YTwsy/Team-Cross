package collab

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"teamcross/internal/problem"
	"teamcross/internal/service"
	"teamcross/internal/sharing"
	"time"
)

type pendingInvite struct {
	Token   string
	Expires time.Time
}

func (a *App) Active() int {
	a.mu.Lock()
	ss := []*Session{}
	js := []*Joined{}
	for _, s := range a.sessions {
		ss = append(ss, s)
	}
	for _, j := range a.joined {
		js = append(js, j)
	}
	a.mu.Unlock()
	n := 0
	for _, s := range ss {
		s.mu.Lock()
		if s.online || s.starting || s.share != nil {
			n++
		}
		s.mu.Unlock()
	}
	for _, j := range js {
		j.mu.Lock()
		if !j.left && !j.ended && j.confirmed {
			n++
		}
		j.mu.Unlock()
	}
	return n
}
func (a *App) onboarding(w http.ResponseWriter, r *http.Request, path string) bool {
	switch {
	case path == "invitations/pending" && r.Method == "POST":
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.Token)) != 1 {
			w.WriteHeader(403)
			return true
		}
		var in struct {
			Invitation string `json:"invitation"`
		}
		if !decode(w, r, &in) {
			return true
		}
		inv, e := sharing.Decode(in.Invitation)
		if e != nil {
			respond(w, nil, e)
			return true
		}
		if time.Now().After(inv.ExpiresAt) {
			respond(w, nil, problem.New("invitation_expired", "邀请已到期", "请向发起者获取新邀请"))
			return true
		}
		id := uuid.NewString()
		a.mu.Lock()
		if a.pending == nil {
			a.pending = map[string]pendingInvite{}
		}
		for k, v := range a.pending {
			if time.Now().After(v.Expires) {
				delete(a.pending, k)
			}
		}
		if len(a.pending) >= 32 {
			a.mu.Unlock()
			respond(w, nil, problem.New("too_many_invitations", "待确认邀请过多", "请先处理已打开的邀请"))
			return true
		}
		a.pending[id] = pendingInvite{Token: in.Invitation, Expires: time.Now().Add(10 * time.Minute)}
		a.mu.Unlock()
		respond(w, map[string]string{"id": id}, nil)
		return true
	case path == "invitations/preview" && r.Method == "POST":
		var in struct {
			Invitation string `json:"invitation"`
			PendingID  string `json:"pendingId"`
		}
		if !decode(w, r, &in) {
			return true
		}
		token, e := a.invitation(in.Invitation, in.PendingID)
		if e != nil {
			respond(w, nil, e)
			return true
		}
		inv, e := sharing.Decode(token)
		if e == nil && time.Now().After(inv.ExpiresAt) {
			e = problem.New("invitation_expired", "邀请已到期", "请获取新邀请")
		}
		respond(w, map[string]any{"title": inv.Title, "host": inv.Host, "expiresAt": inv.ExpiresAt, "transport": inv.Transport}, e)
		return true
	case path == "mcp/observed" && r.Method == "POST":
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.Token)) != 1 {
			w.WriteHeader(403)
			return true
		}
		var in struct {
			Provider string `json:"provider"`
		}
		if !decode(w, r, &in) {
			return true
		}
		// This is client-reported diagnostic evidence, never an authorization.
		if in.Provider != "codex" && in.Provider != "claude" {
			respond(w, map[string]bool{"ok": false}, nil)
			return true
		}
		a.mu.Lock()
		if a.mcpObserved == nil {
			a.mcpObserved = map[string]time.Time{}
		}
		a.mcpObserved[in.Provider] = time.Now()
		a.mu.Unlock()
		respond(w, map[string]bool{"ok": true}, nil)
		return true
	case path == "mcp/probe" && r.Method == "POST":
		executable, _ := os.Executable()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, service.StableExecutable(executable), "mcp", "--data-dir", a.Config.DataDir)
		cmd.Stdin = strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n")
		out, e := cmd.Output()
		ready := false
		if e == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) == 2 {
				var v struct {
					Result struct {
						Tools []any `json:"tools"`
					} `json:"result"`
				}
				e = json.Unmarshal([]byte(lines[1]), &v)
				ready = e == nil && len(v.Result.Tools) > 0
			}
		}
		a.mu.Lock()
		a.mcpProbed = ready
		a.mu.Unlock()
		respond(w, map[string]bool{"ready": ready}, e)
		return true
	}
	return false
}
func (a *App) invitation(token, id string) (string, error) {
	if id == "" {
		return token, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.pending[id]
	if !ok || time.Now().After(v.Expires) {
		delete(a.pending, id)
		return "", problem.New("invitation_pending_expired", "邀请确认页已过期", "请重新打开邀请链接")
	}
	return v.Token, nil
}
