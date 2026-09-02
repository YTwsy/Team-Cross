package transport

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"tailscale.com/client/local"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestLocalAPITailnetStatus(t *testing.T) {
	client := &local.Client{
		OmitAuth: true,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/localapi/v0/status" || request.URL.Query().Get("peers") != "false" {
				t.Fatalf("unexpected LocalAPI request: %s", request.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
                    "BackendState":"Running",
                    "Self":{
                        "DNSName":"host.example.ts.net.",
                        "TailscaleIPs":["100.64.0.8","fd7a:115c:a1e0::8"]
                    }
                }`)),
				Request: request,
			}, nil
		}),
	}
	status, err := (LocalAPITailnetStatus{Client: client}).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Running || status.DNSName != "host.example.ts.net." {
		t.Fatalf("status = %#v", status)
	}
	want := []netip.Addr{netip.MustParseAddr("100.64.0.8"), netip.MustParseAddr("fd7a:115c:a1e0::8")}
	if len(status.IPs) != len(want) {
		t.Fatalf("IPs = %v, want %v", status.IPs, want)
	}
	for i := range want {
		if status.IPs[i] != want[i] {
			t.Fatalf("IPs = %v, want %v", status.IPs, want)
		}
	}
}

func TestLocalAPITailnetNeedsLoginIsNormalDegradation(t *testing.T) {
	client := &local.Client{
		OmitAuth: true,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"BackendState":"NeedsLogin"}`)),
				Request:    request,
			}, nil
		}),
	}
	status, err := (LocalAPITailnetStatus{Client: client}).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Running || len(status.IPs) != 0 {
		t.Fatalf("status = %#v, want normal unavailable state", status)
	}
}
