package share

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func authorizedRequest(secret []byte) *http.Request {
	request := httptest.NewRequest(http.MethodGet, HandshakePath, nil)
	request.Header.Set("X-TeamCross-Protocol", "1")
	request.Header.Set("X-TeamCross-Share-ID", "share-1")
	request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(secret))
	return request
}

func TestAccessGateHandshake(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := bytes.Repeat([]byte{7}, 32)
	gate := AccessGate{
		ShareID:   "share-1",
		Secret:    secret,
		ExpiresAt: now.Add(time.Hour),
		Now:       func() time.Time { return now },
	}
	recorder := httptest.NewRecorder()
	gate.ServeHTTP(recorder, authorizedRequest(secret))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-TeamCross-Protocol") != "1" {
		t.Fatal("protocol response header missing")
	}
}

func TestAccessGateExpiredAndUnauthorized(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := bytes.Repeat([]byte{7}, 32)
	tests := []struct {
		name       string
		gate       AccessGate
		request    *http.Request
		wantStatus int
		wantError  string
	}{
		{
			name:    "expired",
			gate:    AccessGate{ShareID: "share-1", Secret: secret, ExpiresAt: now.Add(-time.Second), Now: func() time.Time { return now }},
			request: authorizedRequest(secret), wantStatus: http.StatusGone, wantError: "expired",
		},
		{
			name:    "unauthorized",
			gate:    AccessGate{ShareID: "share-1", Secret: secret, ExpiresAt: now.Add(time.Hour), Now: func() time.Time { return now }},
			request: authorizedRequest(bytes.Repeat([]byte{8}, 32)), wantStatus: http.StatusUnauthorized, wantError: "unauthorized",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			test.gate.ServeHTTP(recorder, test.request)
			if recorder.Code != test.wantStatus || recorder.Header().Get("X-TeamCross-Error") != test.wantError {
				t.Fatalf("status/error = %d/%q, want %d/%q", recorder.Code, recorder.Header().Get("X-TeamCross-Error"), test.wantStatus, test.wantError)
			}
		})
	}
}
