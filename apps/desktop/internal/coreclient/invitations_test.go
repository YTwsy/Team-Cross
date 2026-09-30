package coreclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStageInvitationKeepsSecretInNativeBody(t *testing.T) {
	const invitation = "teamcross://join?invite=dedicated-test-secret"
	const id = "7d2d9338-3ae8-4bd9-900b-19267c840d73"
	client, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.RequestURI() != "/api/invitations/pending" || r.Header.Get("Authorization") != "Bearer test-private-token" {
			t.Error("incorrect native staging request")
		}
		for _, name := range []string{"Origin", "Cookie", "Idempotency-Key"} {
			if r.Header.Get(name) != "" {
				t.Errorf("unexpected header: %s", name)
			}
		}
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["invitation"] != invitation || len(body) != 1 {
			t.Error("invitation body changed")
		}
		io.WriteString(w, `{"id":"`+id+`"}`)
	})
	got, err := client.StageInvitation(context.Background(), invitation)
	if err != nil || got != id {
		t.Fatal(got, err)
	}
	if w := request(client, "POST", "/api/invitations/pending", strings.NewReader(`{}`)); w.Code != 404 {
		t.Fatal("renderer reached private staging API")
	}
}

func TestStageInvitationDroppedResponseIsNotReplayed(t *testing.T) {
	var count atomic.Int32
	client, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	})
	id, err := client.StageInvitation(context.Background(), "dedicated-test-secret")
	if id != "" || err == nil || count.Load() != 1 || strings.Contains(err.Error(), "dedicated-test-secret") {
		t.Fatal(id, err, count.Load())
	}
}

func TestStageInvitationSanitizesUntrustedResponses(t *testing.T) {
	for _, response := range []string{`{"id":"teamcross://join?invite=secret"}`, `{"error":"secret"}`, `not json`, `{"id":"../../control/stop"}`, strings.Repeat("x", 16385)} {
		t.Run(response[:min(len(response), 25)], func(t *testing.T) {
			client, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, response) })
			id, err := client.StageInvitation(context.Background(), "secret")
			if id != "" || err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal(id, err)
			}
		})
	}
}

func TestStageInvitationBoundsInputBeforeDiscovery(t *testing.T) {
	client, _ := New(t.TempDir())
	for _, invitation := range []string{"", strings.Repeat("x", 65537)} {
		if _, err := client.StageInvitation(context.Background(), invitation); err != errInvitation {
			t.Fatal(err)
		}
	}
}
