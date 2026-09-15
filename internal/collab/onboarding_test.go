package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPresenceAndInputRequest(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx := context.Background()
	if e := s.Action(ctx, "share"); e != nil {
		t.Fatal(e)
	}
	b, e := Open(Config{DataDir: t.TempDir(), Binary: "/missing-codex"})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	j, e := b.Join(ctx, s.view()["invitation"].(string))
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(3 * time.Second)
	for s.view()["participantOnline"] != true && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.view()["participantOnline"] != true {
		t.Fatal("Core heartbeat did not establish presence")
	}
	if e = j.request(ctx, "POST", "/v2/request_input", map[string]uint64{"epoch": s.epoch}, nil); e != nil {
		t.Fatal(e)
	}
	if s.view()["inputRequested"] != true || s.view()["writer"] != "owner" {
		t.Fatal(s.view())
	}
	if e = j.request(ctx, "POST", "/v2/request_input", map[string]uint64{"epoch": 999}, nil); e == nil {
		t.Fatal("accepted stale request")
	}
	if e = j.request(ctx, "POST", "/v2/cancel_input", map[string]uint64{"epoch": s.epoch}, nil); e != nil {
		t.Fatal(e)
	}
	if s.view()["inputRequested"] != false {
		t.Fatal("request not cancelled")
	}
	j.close()
	if s.view()["participantOnline"] != false {
		t.Fatal("leave did not clear presence")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, method := range f.calls {
		if method == "turn/start" {
			t.Fatal("onboarding sent model input")
		}
	}
}
func TestPendingInvitationIsLocalAndPreviewOnly(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	if e := s.Action(context.Background(), "share"); e != nil {
		t.Fatal(e)
	}
	token := s.view()["invitation"].(string)
	handler := a.Handler(http.NotFoundHandler())
	post := func(path, body, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://127.0.0.1:1/api/"+path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	body, _ := json.Marshal(map[string]string{"invitation": token})
	if w := post("invitations/pending", string(body), ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	w := post("invitations/pending", string(body), "Bearer "+a.Token)
	var pending struct {
		ID string `json:"id"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &pending); e != nil || pending.ID == "" {
		t.Fatal(w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(token)) {
		t.Fatal("secret in pending response")
	}
	w = post("invitations/preview", `{"pendingId":"`+pending.ID+`"}`, "")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("tcx3.")) {
		t.Fatal(w.Body.String())
	}
	a.mu.Lock()
	n := len(a.joined)
	a.mu.Unlock()
	if n != 0 {
		t.Fatal("preview joined automatically")
	}
	a.mu.Lock()
	a.pending[pending.ID] = pendingInvite{Token: token, Expires: time.Now().Add(-time.Second)}
	a.mu.Unlock()
	w = post("invitations/preview", `{"pendingId":"`+pending.ID+`"}`, "")
	if w.Code != 400 {
		t.Fatal("expired preview accepted")
	}
}
