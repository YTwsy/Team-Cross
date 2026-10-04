package service

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestUpgradeAdmissionDoesNotInterruptOrReplayRequests(t *testing.T) {
	a := &Admission{}
	entered, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	h := a.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-finish
	}))
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/create", nil))
		close(done)
	}()
	<-entered
	if a.DrainIfIdle(func() bool { return true }) {
		t.Fatal("interrupted an in-flight request")
	}
	close(finish)
	<-done
	if a.DrainIfIdle(func() bool { return false }) {
		t.Fatal("ignored newly active collaboration")
	}
	if !a.DrainIfIdle(func() bool { return true }) {
		t.Fatal("idle service did not drain")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/create", nil))
	if w.Code != http.StatusServiceUnavailable || calls.Load() != 1 {
		t.Fatal(w.Code, calls.Load())
	}
}
