package mcpevents

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func eventFixture(t *testing.T) (*Manager, string, *int, *bool, *[]string, *int) {
	t.Helper()
	revision := 1
	revoked := false
	delivered := []string{}
	status := 202
	source := func(context.Context, string) ([]Observation, error) {
		if revoked {
			return nil, ErrRevoked
		}
		return []Observation{{Key: "brief", Name: eventNames[0], Timestamp: time.Unix(int64(revision), 0), Data: map[string]any{"spaceId": "space", "revision": revision}}}, nil
	}
	m, err := Open(t.TempDir(), source, func(_ context.Context, space, name string, _ map[string]any) (any, error) {
		return map[string]any{"spaceId": space, "name": name}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := m.CreateGrant(context.Background(), uuid.NewString(), "space", "Fixture cloud")
	if err != nil {
		t.Fatal(err)
	}
	m.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		key := bytes.Repeat([]byte{7}, 32)
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(r.Header.Get("webhook-id") + "." + r.Header.Get("webhook-timestamp") + "."))
		_, _ = mac.Write(raw)
		expect := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
		if !strings.Contains(r.Header.Get("webhook-signature"), expect) || r.Header.Get("X-MCP-Subscription-Id") == "" {
			t.Error("invalid Standard Webhooks signature")
		}
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		code := status
		response := "{}"
		if body["type"] == "verification" {
			code = 200
			b, _ := json.Marshal(map[string]any{"challenge": body["challenge"]})
			response = string(b)
		} else {
			delivered = append(delivered, body["eventId"].(string))
			if r.Header.Get("webhook-id") != body["eventId"] {
				t.Error("event id mismatch")
			}
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(response)), Header: http.Header{}}, nil
	})}
	return m, token, &revision, &revoked, &delivered, &status
}

func subscribeFixture(t *testing.T, m *Manager, token string) string {
	t.Helper()
	g, err := m.authenticate(token)
	if err != nil {
		t.Fatal(err)
	}
	in := subscribeInput{Name: eventNames[0], Arguments: map[string]string{"spaceId": "space"}}
	in.Delivery.Mode, in.Delivery.URL, in.Delivery.Secret = "webhook", "https://receiver.example/events", "whsec_"+base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	out, err := m.subscribe(context.Background(), g, in, false)
	if err != nil {
		t.Fatal(err)
	}
	return out.(map[string]any)["id"].(string)
}

func TestLifecycleRestartDedupeAndSeparateProcessingReceipt(t *testing.T) {
	m, token, revision, _, deliveries, status := eventFixture(t)
	id := subscribeFixture(t, m, token)
	if same := subscribeFixture(t, m, token); same != id {
		t.Fatal("non-idempotent subscription")
	}
	m.Tick(context.Background())
	if len(*deliveries) != 0 {
		t.Fatal("subscription replayed pre-existing content")
	}
	*revision = 2
	*status = 503
	m.Tick(context.Background())
	if len(*deliveries) != 1 {
		t.Fatal(*deliveries)
	}
	eventID := (*deliveries)[0]
	reopened, err := Open(filepath.Dir(m.path), m.source, m.read)
	if err != nil {
		t.Fatal(err)
	}
	reopened.client = m.client
	m = reopened
	if _, err = m.authenticate(token); err != nil {
		t.Fatal("grant lost on restart", err)
	}
	m.mu.Lock()
	e := m.state.Events[eventID]
	e.NextAttempt = time.Time{}
	next := clone(m.state)
	next.Events[eventID] = e
	if err = m.commit(next); err != nil {
		t.Fatal(err)
	}
	m.mu.Unlock()
	*status = 202
	m.Tick(context.Background())
	if len(*deliveries) != 2 || (*deliveries)[1] != eventID {
		t.Fatal("retry changed identity", *deliveries)
	}
	m.Tick(context.Background())
	if len(*deliveries) != 2 {
		t.Fatal("replayed accepted event")
	}
	g, _ := m.authenticate(token)
	if _, err = m.callTool(context.Background(), g, "finish_space_event", map[string]any{"eventId": eventID, "status": "completed", "summary": "done"}); err == nil {
		t.Fatal("transport receipt counted as processing")
	}
	if _, err = m.callTool(context.Background(), g, "read_space_event", map[string]any{"eventId": eventID}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.callTool(context.Background(), g, "finish_space_event", map[string]any{"eventId": eventID, "status": "completed", "summary": "done"}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.callTool(context.Background(), g, "finish_space_event", map[string]any{"eventId": eventID, "status": "completed", "summary": "changed"}); err == nil {
		t.Fatal("overwrote completion")
	}
	raw, _ := json.Marshal(m.View())
	if strings.Contains(string(raw), "whsec_") || strings.Contains(string(raw), token) || strings.Contains(string(raw), "\"hash\"") {
		t.Fatal("view leaked credentials")
	}
	stored, _ := os.ReadFile(m.path)
	if strings.Contains(string(stored), token) {
		t.Fatal("persisted plaintext access token")
	}
}

func TestPauseRevocationExpiryAndTerminalDelivery(t *testing.T) {
	for _, scenario := range []string{"pause", "revoke", "membership", "expiry", "410", "413"} {
		t.Run(scenario, func(t *testing.T) {
			m, token, revision, revoked, deliveries, status := eventFixture(t)
			id := subscribeFixture(t, m, token)
			*revision = 2
			switch scenario {
			case "pause":
				if err := m.Change(id, "subscription", "paused"); err != nil {
					t.Fatal(err)
				}
				subscribeFixture(t, m, token)
				if m.state.Subscriptions[id].State != "paused" {
					t.Fatal("refresh undid user pause")
				}
			case "revoke":
				_ = m.Change(id, "subscription", "revoked")
			case "membership":
				*revoked = true
			case "expiry":
				s := m.state.Subscriptions[id]
				s.Expires = time.Now().Add(-time.Second)
				m.state.Subscriptions[id] = s
			case "410":
				*status = 410
			case "413":
				*status = 413
			}
			m.Tick(context.Background())
			m.Tick(context.Background())
			want := 0
			if scenario == "410" || scenario == "413" {
				want = 1
			}
			if len(*deliveries) != want {
				t.Fatal(*deliveries)
			}
			if scenario == "410" && m.state.Subscriptions[id].State != "revoked" {
				t.Fatal("gone callback kept receiving")
			}
		})
	}
}

func TestAuthorizationFiltersAndCallbackFailures(t *testing.T) {
	m, token, _, _, _, _ := eventFixture(t)
	g, _ := m.authenticate(token)
	for _, args := range []map[string]string{{"spaceId": "other"}, {"spaceId": "space", "instruction": "do something"}, {"spaceId": "space", "requestId": "wrong kind"}} {
		if err := validArguments(g, eventNames[0], args); err == nil {
			t.Fatal("accepted invalid filter", args)
		}
	}
	for _, name := range []string{"send_input", "teamcross_ui_write", "read_context"} {
		if _, err := m.callTool(context.Background(), g, name, map[string]any{}); err == nil {
			t.Fatal("gateway exposed local operation", name)
		}
	}
	if _, code := m.RPC(context.Background(), "not-authorized", json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"server/discover"}`)); code != 401 {
		t.Fatal(code)
	}
	m.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"challenge":"wrong"}`))}, nil
	})}
	in := subscribeInput{Name: eventNames[0], Arguments: map[string]string{"spaceId": "space"}}
	in.Delivery.Mode = "webhook"
	in.Delivery.URL = "https://receiver.example/events"
	in.Delivery.Secret = "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	if _, err := m.subscribe(context.Background(), g, in, false); err == nil {
		t.Fatal("accepted wrong challenge")
	} else if e, ok := err.(*RPCError); !ok || e.Code != -32015 {
		t.Fatal(err)
	}
	if len(m.state.Subscriptions) != 0 {
		t.Fatal("failed verification became active")
	}
}

func TestRotationRefreshAndExpiredPendingEvent(t *testing.T) {
	m, token, revision, _, deliveries, status := eventFixture(t)
	id := subscribeFixture(t, m, token)
	g, _ := m.authenticate(token)
	in := subscribeInput{Name: eventNames[0], Arguments: map[string]string{"spaceId": "space"}}
	in.Delivery.Mode, in.Delivery.URL = "webhook", "https://receiver.example/events"
	in.Delivery.Secret = "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32))
	transport := m.client.Transport
	m.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Count(r.Header.Get("webhook-signature"), "v1,") != 2 {
			t.Error("rotation must sign with both current and previous keys")
		}
		return transport.RoundTrip(r)
	})
	for i := 0; i < 2; i++ {
		out, err := m.subscribe(context.Background(), g, in, false)
		if err != nil || out.(map[string]any)["id"] != id {
			t.Fatalf("refresh changed subscription: %v %v", out, err)
		}
		if m.state.Subscriptions[id].PreviousSecret == "" {
			t.Fatal("same-key refresh dropped rotation overlap")
		}
	}
	*revision, *status = 2, 503
	m.Tick(context.Background())
	if len(*deliveries) != 1 {
		t.Fatal(*deliveries)
	}
	eventID := (*deliveries)[0]
	sub := m.state.Subscriptions[id]
	sub.Expires = time.Now().Add(-time.Second)
	m.state.Subscriptions[id] = sub
	m.Tick(context.Background())
	if m.state.Events[eventID].State != "cancelled" || len(*deliveries) != 1 {
		t.Fatal("expired pending event was not cancelled")
	}
	reopened, err := Open(filepath.Dir(m.path), m.source, m.read)
	if err != nil || reopened.state.Events[eventID].State != "cancelled" {
		t.Fatalf("cancellation did not survive restart: %v", err)
	}
}

func TestCallbackDestinationsAndGatewayBoundary(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.169.254", "192.168.0.1", "198.18.0.1", "224.0.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "2001:db8::1", "2002:7f00:1::"} {
		if publicAddress(netip.MustParseAddr(ip)) {
			t.Fatal("public", ip)
		}
	}
	for _, ip := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
		if !publicAddress(netip.MustParseAddr(ip)) {
			t.Fatal("blocked public", ip)
		}
	}
	for _, u := range []string{"http://example.com", "https://localhost/#x", "https://user:password@example.com", "https://127.0.0.1/events"} {
		if callbackURL(u) == nil {
			t.Fatal("accepted", u)
		}
	}
	if err := webhookClient().CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatal("redirects enabled")
	}
	calls := 0
	h := Gateway(func(context.Context, string, json.RawMessage) (any, int, error) {
		calls++
		return map[string]bool{"ok": true}, 200, nil
	})
	for _, row := range []struct {
		path, token, origin string
		status              int
	}{{"/api/settings", "TC_EVT_x", "", 404}, {"/mcp", "local-core-token", "", 401}, {"/mcp", "TC_EVT_x", "https://other.example", 403}, {"/mcp", "TC_EVT_x", "", 200}} {
		r := httptest.NewRequest("POST", row.path, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer "+row.token)
		r.Header.Set("Origin", row.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != row.status {
			t.Fatal(row, w.Code)
		}
	}
	if calls != 1 {
		t.Fatal("forwarded disallowed path", calls)
	}
}
