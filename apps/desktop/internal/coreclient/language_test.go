package coreclient

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestNativeLanguageUsesPublicPreferenceWithoutCredential(t *testing.T) {
	var calls atomic.Int32
	c, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.Path != "/api/ui-language" || string(body) != `{"mode":"en"}` || r.Header.Get("Authorization") != "" || r.Header.Get("Origin") != "" {
			t.Error("invalid language request")
		}
		io.WriteString(w, `{"mode":"en","resolved":"en"}`)
	})
	if err := c.SetLanguage(context.Background(), "invalid"); err == nil || calls.Load() != 0 {
		t.Fatal(err, calls.Load())
	}
	if err := c.SetLanguage(context.Background(), "en"); err != nil || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
}

func TestNativeLanguageDoesNotReplayLostResponse(t *testing.T) {
	var calls atomic.Int32
	c, _, _ := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	})
	if err := c.SetLanguage(context.Background(), "en"); err == nil || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
}
