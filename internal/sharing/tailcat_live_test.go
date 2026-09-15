package sharing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveTailcatMembership is deliberately opt-in because it bootstraps
// through Tailcat's public DERP map. It proves the Team Cross HTTPS,
// certificate pinning, admission and reconnect path on one Mac; it does not
// prove two-Mac reachability or whether Tailcat selected a direct or relay path.
func TestLiveTailcatMembership(t *testing.T) {
	if os.Getenv("TEAMCROSS_TEST_TAILCAT") != "1" {
		t.Skip("set TEAMCROSS_TEST_TAILCAT=1 to use Tailcat's network")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	runtime, err := Start(ctx, TransportTailcat, "tailcat-live", "Tailcat live smoke", "same-mac", http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v2/status" {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"transport":"tailcat"}`))
	}), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)

	if runtime.Transport() != TransportTailcat || !strings.HasPrefix(runtime.Token(), "tcx3.") || runtime.Invitation.Tailcat == nil || len(runtime.Invitation.Endpoints) != 0 {
		t.Fatalf("unexpected Tailcat invitation: %#v", runtime.Invitation)
	}

	invitationConnection, err := Connect(ctx, runtime.Invitation)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = invitationConnection.Close() })
	credential := NewCredential()
	body, _ := json.Marshal(map[string]string{"credential": credential})
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, invitationConnection.URL+"/v2/join", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+runtime.Invitation.Secret)
	request.Header.Set("Content-Type", "application/json")
	response, err := invitationConnection.Client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("join returned %s", response.Status)
	}
	if err = invitationConnection.Close(); err != nil {
		t.Fatal(err)
	}

	memberConnection, err := Connect(ctx, runtime.Invitation, credential)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = memberConnection.Close() })
	if memberConnection.Transport != TransportTailcat {
		t.Fatalf("reconnected through %q", memberConnection.Transport)
	}
	if err = memberConnection.Close(); err != nil {
		t.Fatal(err)
	}
}
