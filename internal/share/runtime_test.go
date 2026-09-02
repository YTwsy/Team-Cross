package share

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"teamcross/internal/transport"
)

func TestRuntimeRoundsExpiryWithoutDivergingFromInvitation(t *testing.T) {
	now := time.Unix(1_800_000_000, 900_000_000)
	runtime, err := Start(context.Background(), RuntimeConfig{
		ExpiresAt:         now.Add(50 * time.Millisecond),
		Now:               func() time.Time { return now },
		Tailnet:           offlineTailnet{},
		ExtraLANEndpoints: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	invitation := runtime.Invitation()
	if got, want := invitation.ExpiresAt, now.Truncate(time.Second).Add(time.Second).Unix(); got != want {
		t.Fatalf("invitation expiry = %d, want %d", got, want)
	}
}

func TestTailnetCandidatesMatchIPv4Listener(t *testing.T) {
	got := tailnetIPv4Endpoints([]netip.Addr{
		netip.MustParseAddr("fd7a:115c:a1e0::1"),
		netip.MustParseAddr("100.64.0.2"),
		netip.MustParseAddr("::ffff:100.64.0.2"),
	}, 43123)
	want := []string{"100.64.0.2:43123"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tailnet endpoints = %v, want %v", got, want)
	}
}

type offlineTailnet struct{}

func (offlineTailnet) Status(context.Context) (transport.TailnetStatus, error) {
	return transport.TailnetStatus{Running: false}, nil
}

func TestRuntimeAndSelectorHandshake(t *testing.T) {
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/share/v1/ping" {
			http.NotFound(response, request)
			return
		}
		_, _ = io.WriteString(response, "pong")
	})
	runtime, err := Start(context.Background(), RuntimeConfig{
		ExpiresAt:         time.Now().Add(time.Hour),
		Handler:           handler,
		Tailnet:           offlineTailnet{},
		ExtraLANEndpoints: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	selector := transport.NewSelector()
	selector.Browse = func(context.Context, string) ([]string, error) { return nil, nil }
	selector.Tailnet = offlineTailnet{}
	selection, err := selector.Dial(context.Background(), runtime.Invitation())
	if err != nil {
		t.Fatal(err)
	}
	defer selection.Close()
	if selection.Transport != transport.KindLAN {
		t.Fatalf("transport = %q, want LAN", selection.Transport)
	}
	client := selection.HTTPClient()
	request, err := http.NewRequest(http.MethodGet, "https://teamcross.invalid/share/v1/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	selection.Authorize(request)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "pong" {
		t.Fatalf("response = %d %q", response.StatusCode, body)
	}
}

func TestRuntimeRevokeIsTerminal(t *testing.T) {
	runtime, err := Start(context.Background(), RuntimeConfig{
		ExpiresAt:         time.Now().Add(time.Hour),
		Tailnet:           offlineTailnet{},
		ExtraLANEndpoints: []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	runtime.Revoke()
	selector := transport.NewSelector()
	selector.Browse = func(context.Context, string) ([]string, error) { return nil, nil }
	selector.Tailnet = offlineTailnet{}
	_, err = selector.Dial(context.Background(), runtime.Invitation())
	if !transport.IsTerminal(err) {
		t.Fatalf("Dial error = %v, want terminal protocol error", err)
	}
}
