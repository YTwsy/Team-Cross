package sharing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if r.InvitationState() != "joined" {
		t.Fatal("joined member expired")
	}
	if w := membershipRequest(h, "GET", "/v2/status", credential, nil); w.Code != 200 {
		t.Fatal("member lost access after TTL", w.Code)
	}
	if w := membershipRequest(h, "POST", "/v2/join", r.Invitation.Secret, map[string]string{"credential": credential}); w.Code != 200 {
		t.Fatal("lost join response not recoverable", w.Code)
	}
	if w := membershipRequest(h, "POST", "/v2/join", r.Invitation.Secret, map[string]string{"credential": NewCredential()}); w.Code != 410 {
		t.Fatal("invite admitted a second member", w.Code)
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

func TestLeaveInvalidatesOpenRequestsAndOriginalInvitation(t *testing.T) {
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
	if Authorized(saved, r) || r.InvitationState() != "left" {
		t.Fatal("stale authorized request survived leave")
	}
	if w := membershipRequest(h, "GET", "/v2/status", credential, nil); w.Code != 410 {
		t.Fatal("left member reconnected")
	}
	if w := membershipRequest(h, "POST", "/v2/join", r.Invitation.Secret, map[string]string{"credential": credential}); w.Code != 410 {
		t.Fatal("leave silently rejoined")
	}
}

func TestIndependentAdmissionsAndStaleRequestCancellation(t *testing.T) {
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
	if first.ID == second.ID || first.Token == second.Token {
		t.Fatal("invitations reused")
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
