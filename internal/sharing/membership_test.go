package sharing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func membershipRequest(h http.Handler, method, path, credential string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+credential)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestConcurrentJoinResetAndCloseLinkPreserveMembers(t *testing.T) {
	r := &Runtime{Invitation: Invitation{Secret: NewCredential(), ReadOnly: true}}
	defer r.Close()
	h := r.authorize(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { w.WriteHeader(200) }))
	link, err := r.IssueInvitation("initial")
	if err != nil {
		t.Fatal(err)
	}
	secret := r.Invitation.Secret
	credentials := []string{NewCredential(), NewCredential(), NewCredential()}
	var wg sync.WaitGroup
	for _, credential := range credentials {
		wg.Add(1)
		go func(credential string) {
			defer wg.Done()
			for range 2 {
				w := membershipRequest(h, "POST", "/v2/join", secret, map[string]string{"credential": credential, "name": "同名成员"})
				if w.Code != 200 {
					t.Errorf("join/retry: %d %s", w.Code, w.Body.String())
				}
			}
		}(credential)
	}
	wg.Wait()
	if len(r.Members()) != 3 {
		t.Fatal("retries duplicated or concurrent joins collapsed members")
	}
	for _, m := range r.Members() {
		if m.ExecutionAccess {
			t.Fatal("read-only link granted execution")
		}
	}
	reset, err := r.ResetInvitation("reset")
	if err != nil || reset.ID == link.ID || reset.Token == link.Token {
		t.Fatal("reset failed", err)
	}
	retry, err := r.ResetInvitation("reset")
	if err != nil || retry.Token != reset.Token {
		t.Fatal("reset retry rotated twice", err)
	}
	if w := membershipRequest(h, "POST", "/v2/join", secret, map[string]string{"credential": NewCredential()}); w.Code != 410 {
		t.Fatal("old link still accepts joins")
	}
	if !r.RevokeInvitation(reset.ID) {
		t.Fatal("close failed")
	}
	for _, credential := range credentials {
		if w := membershipRequest(h, "GET", "/v2/status", credential, nil); w.Code != 200 {
			t.Fatal("link reset/close revoked member")
		}
	}
	current, _ := r.IssueInvitation("")
	if current.Token != "" || current.State != "revoked" {
		t.Fatal("closed link exposed token")
	}
}

func TestExecutionGrantRevocationCancelsOnlyExecution(t *testing.T) {
	r := &Runtime{Invitation: Invitation{Secret: NewCredential(), ReadOnly: true}}
	defer r.Close()
	credential := NewCredential()
	started, stopped := make(chan struct{}), make(chan struct{})
	captured := make(chan context.Context, 1)
	h := r.authorize(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/execution" {
			ctx, cancel, allowed := ExecutionContext(req.Context(), r)
			defer cancel()
			if !allowed {
				t.Error("grant not applied")
				return
			}
			captured <- ctx
			close(started)
			<-ctx.Done()
			if ExecutionAuthorized(ctx, r) {
				t.Error("revoked execution still authorized")
			}
			close(stopped)
		}
	}))
	membershipRequest(h, "POST", "/v2/join", r.Invitation.Secret, map[string]string{"credential": credential})
	id := r.Members()[0].ID
	if !r.SetExecutionAccess(id, true) {
		t.Fatal("grant failed")
	}
	go membershipRequest(h, "GET", "/execution", credential, nil)
	<-started
	r.SetExecutionAccess(id, false)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("native request survived revocation")
	}
	if !r.HasMember(id) || r.HasExecutionAccess(id) {
		t.Fatal("grant and membership conflated")
	}
	if w := membershipRequest(h, "GET", "/v2/status", credential, nil); w.Code != 200 {
		t.Fatal("space access lost")
	}
	r.SetExecutionAccess(id, true)
	old := context.WithoutCancel(<-captured)
	if ExecutionAuthorized(old, r) {
		t.Fatal("regrant revived the old request grant")
	}
	fresh, cancel, allowed := ExecutionContext(old, r)
	defer cancel()
	if !allowed || !ExecutionAuthorized(fresh, r) {
		t.Fatal("new request did not receive the current grant")
	}
}

func TestInvitationOnlyLimitsFirstAdmission(t *testing.T) {
	r := &Runtime{Invitation: Invitation{Secret: NewCredential(), ExpiresAt: time.Now().Add(time.Hour).Round(0)}}
	defer r.Close()
	h := r.authorize(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !Authorized(req.Context(), r) {
			t.Error("missing membership binding")
		}
		w.WriteHeader(200)
	}))
	credential := NewCredential()
	if w := membershipRequest(h, "GET", "/v2/status", r.Invitation.Secret, nil); w.Code != 401 {
		t.Fatal("invitation accessed shared data", w.Code)
	}
	if w := membershipRequest(h, "GET", "/v2/invitation", r.Invitation.Secret, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := membershipRequest(h, "POST", "/v2/join", r.Invitation.Secret, map[string]string{"credential": credential}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	// Advance the host's wall-clock deadline past a sleep/reconnect boundary.
	r.mu.Lock()
	r.Invitation.ExpiresAt = time.Now().Add(-time.Hour)
	r.mu.Unlock()
	if r.InvitationState() != "expired" {
		t.Fatal("expired link remained open")
	}
	if w := membershipRequest(h, "GET", "/v2/status", credential, nil); w.Code != 200 {
		t.Fatal("member lost access after TTL", w.Code)
	}
	if w := membershipRequest(h, "POST", "/v2/join", r.Invitation.Secret, map[string]string{"credential": credential}); w.Code != 200 {
		t.Fatal("lost join response not recoverable", w.Code)
	}
	if w := membershipRequest(h, "POST", "/v2/join", r.Invitation.Secret, map[string]string{"credential": NewCredential()}); w.Code != 410 {
		t.Fatal("expired invite admitted a new member", w.Code)
	}
	if w := membershipRequest(h, "GET", "/v2/status", r.Invitation.Secret, nil); w.Code != 401 {
		t.Fatal("used invite accessed data", w.Code)
	}
	r.Revoke()
	if w := membershipRequest(h, "GET", "/v2/status", credential, nil); w.Code != 410 {
		t.Fatal("end did not revoke member", w.Code)
	}
}

func TestUnusedExpiredInvitationCannotJoin(t *testing.T) {
	r := &Runtime{Invitation: Invitation{Secret: NewCredential(), ExpiresAt: time.Now().Add(-time.Second)}}
	h := r.authorize(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unauthorized request reached collaboration") }))
	if r.InvitationState() != "expired" {
		t.Fatal("missing admission expiry")
	}
	w := membershipRequest(h, "POST", "/v2/join", r.Invitation.Secret, map[string]string{"credential": NewCredential()})
	if w.Code != 410 || !bytes.Contains(w.Body.Bytes(), []byte("invitation_expired")) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestLeaveInvalidatesMemberWithoutClosingLink(t *testing.T) {
	r := &Runtime{Invitation: Invitation{Secret: NewCredential(), ExpiresAt: time.Now().Add(time.Hour)}}
	defer r.Close()
	var saved context.Context
	h := r.authorize(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		saved = req.Context()
		if req.URL.Path == "/v2/leave" && !r.Leave(req.Context()) {
			t.Error("leave rejected")
		}
		w.WriteHeader(200)
	}))
	credential := NewCredential()
	membershipRequest(h, "POST", "/v2/join", r.Invitation.Secret, map[string]string{"credential": credential})
	if w := membershipRequest(h, "POST", "/v2/leave", credential, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if Authorized(saved, r) || r.InvitationState() != "active" {
		t.Fatal("stale authorized request survived leave")
	}
	if w := membershipRequest(h, "GET", "/v2/status", credential, nil); w.Code != 410 {
		t.Fatal("left member reconnected")
	}
	if w := membershipRequest(h, "POST", "/v2/join", r.Invitation.Secret, map[string]string{"credential": credential}); w.Code != 410 {
		t.Fatal("leave silently rejoined")
	}
}

func TestReusableInvitationAndStaleRequestCancellation(t *testing.T) {
	r := &Runtime{Invitation: Invitation{Secret: NewCredential(), ExpiresAt: time.Now().Add(time.Hour)}}
	defer r.Close()
	started, revoked := make(chan struct{}), make(chan struct{})
	h := r.authorize(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/pending" {
			close(started)
			<-req.Context().Done()
			if Authorized(req.Context(), r) {
				t.Error("revoked request remained authorized")
			}
			close(revoked)
		}
	}))
	first, _ := r.IssueInvitation("B")
	second, _ := r.IssueInvitation("C")
	if first.ID != second.ID || first.Token != second.Token {
		t.Fatal("link changed for another participant")
	}
	// Test admission directly; the transport-independent envelope isn't complete here.
	r.mu.Lock()
	bs := r.invitations[first.ID].invitation.Secret
	cs := r.invitations[second.ID].invitation.Secret
	r.mu.Unlock()
	b, c := NewCredential(), NewCredential()
	for _, row := range [][2]string{{bs, b}, {cs, c}} {
		if w := membershipRequest(h, "POST", "/v2/join", row[0], map[string]string{"credential": row[1]}); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	go membershipRequest(h, "GET", "/pending", b, nil)
	<-started
	members := r.Members()
	if len(members) != 2 {
		t.Fatal(members)
	}
	r.RevokeMember(members[0].ID)
	select {
	case <-revoked:
	case <-time.After(time.Second):
		t.Fatal("pending request survived member revocation")
	}
	if w := membershipRequest(h, "GET", "/v2/status", c, nil); w.Code != 200 {
		t.Fatal("C lost access", w.Code)
	}
	other := &Runtime{Invitation: Invitation{Secret: NewCredential(), ExpiresAt: time.Now().Add(time.Hour)}}
	defer other.Close()
	if w := membershipRequest(other.authorize(http.NotFoundHandler()), "GET", "/v2/status", c, nil); w.Code != 401 {
		t.Fatal("credential crossed space", w.Code)
	}
}
