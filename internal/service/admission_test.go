package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUpgradeGraceCompletesAdmittedWriteAndRejectsNewWork(t *testing.T) {
	a := &Admission{}
	entered, finish, done, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	h := a.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); close(entered); <-finish }))
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/create", nil))
		close(done)
	}()
	<-entered
	a.BeginUpgrade(time.Second, func() bool { return false }, func() { close(stopped) })
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/create", nil))
	if w.Code != 503 || calls.Load() != 1 {
		t.Fatal(w.Code, calls.Load())
	}
	select {
	case <-stopped:
		t.Fatal("interrupted admitted write")
	default:
	}
	close(finish)
	<-done
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("did not finish early")
	}
}

func TestUpgradeDeadlineCannotBeExtendedAndCancelsRemainingWork(t *testing.T) {
	a := &Admission{}
	entered, cancelled, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	h := a.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(cancelled) }))
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/create", nil))
	<-entered
	deadline := a.BeginUpgrade(150*time.Millisecond, func() bool { return true }, func() { close(stopped) })
	for range 8 {
		time.Sleep(10 * time.Millisecond)
		if again := a.BeginUpgrade(time.Hour, func() bool { return true }, func() { t.Error("duplicate stop") }); !again.Equal(deadline) {
			t.Fatal("deadline renewed")
		}
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("activity blocked upgrade")
	}
	<-cancelled
	if time.Now().After(deadline.Add(300 * time.Millisecond)) {
		t.Fatal("deadline not enforced")
	}
}

func TestUpgradeAllowsOnlyCompletionDuringGrace(t *testing.T) {
	a := &Admission{}
	stopped := make(chan struct{})
	a.BeginUpgrade(100*time.Millisecond, func() bool { return true }, func() { close(stopped) })
	var writes atomic.Int32
	h := a.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writes.Add(1); w.WriteHeader(200) }))
	for _, tc := range []struct {
		path, body string
		code       int
	}{
		{"/api/agent-tools/call", `{"name":"finish_agent_request"}`, 200},
		{"/api/runtime-annotations/session", `{"name":"finish_agent_request"}`, 200},
		{"/api/agent-tools/call", `{"name":"send_space_request"}`, 503},
		{"/api/create", `{"name":"finish_agent_request"}`, 503},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)))
		if w.Code != tc.code {
			t.Fatal(tc, w.Code)
		}
	}
	<-stopped
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/agent-tools/call", strings.NewReader(`{"name":"finish_agent_request"}`)))
	if w.Code != 503 || writes.Load() != 2 {
		t.Fatal("accepted work after deadline", w.Code, writes.Load())
	}
}

func TestUpgradeCancelsIdlePollWithoutWaitingForGrace(t *testing.T) {
	a := &Admission{}
	entered, done, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	h := a.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(done) }))
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/agent-receivers/poll", nil))
	<-entered
	a.BeginUpgrade(time.Hour, func() bool { return false }, func() { close(stopped) })
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("idle poll blocks upgrade")
	}
	<-done
}
