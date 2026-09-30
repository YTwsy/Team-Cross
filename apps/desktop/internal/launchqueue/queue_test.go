package launchqueue

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"teamcross/apps/desktop/internal/appinstance"
)

func TestInvitationURLAllowsOnlyInvitationSchemes(t *testing.T) {
	for _, scheme := range []string{"teamcross", "teamcross-desktop-preview"} {
		got, ok := InvitationURL(scheme + "://join?invite=test%2Bsecret")
		if !ok || got != "teamcross://join?invite=test%2Bsecret" {
			t.Fatal(got, ok)
		}
	}
	for _, raw := range []string{"", "https://join?invite=test", "teamcross://other?invite=test", "teamcross://user@join?invite=test", "teamcross://join?invite=a&invite=b", "teamcross://join?invite=test&command=stop", "teamcross://join?invite=", "teamcross://join?invite=test#secret", "teamcross://join/stop?invite=test", "teamcross://join?invite=%zz", strings.Repeat("x", 65537)} {
		if got, ok := InvitationURL(raw); ok || got != "" {
			t.Fatal(raw[:min(len(raw), 60)])
		}
	}
}

func TestQueueAtomicBatchBoundAndStableIdentity(t *testing.T) {
	now := time.Now()
	q := &Queue{}
	first := appinstance.NewRequest([]string{"one"})
	if !q.Add(first, now) {
		t.Fatal("first request refused")
	}
	first.URLs[0] = "changed"
	r, ok := q.Front(now)
	if !ok || r.URLs[0] != "one" {
		t.Fatal("caller modified queued request")
	}
	r.URLs[0] = "changed"
	for i := 0; i < 3; i++ {
		retry, _ := q.Front(now)
		if retry.ID != first.ID || retry.URLs[0] != "one" {
			t.Fatal("retry identity changed")
		}
	}
	urls := make([]string, 32)
	if q.Add(appinstance.NewRequest(urls), now) {
		t.Fatal("oversized batch partially accepted")
	}
	q.Done("unknown")
	if r, _ := q.Front(now); r.ID != first.ID {
		t.Fatal("unrelated receipt removed request")
	}
	q.Done(first.ID)
	if _, ok := q.Front(now); ok {
		t.Fatal("completed request retained")
	}
	if !q.Add(appinstance.NewRequest(urls), now) {
		t.Fatal("capacity not released")
	}
}

func TestQueueConcurrentCapacityAndExpiration(t *testing.T) {
	now := time.Now()
	q := &Queue{}
	var count atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < 64; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if q.Add(appinstance.NewRequest([]string{"test-secret"}), now) {
				count.Add(1)
			}
		}()
	}
	workers.Wait()
	if count.Load() != 32 {
		t.Fatal(count.Load())
	}
	front, _ := q.Front(now)
	if !q.Live(front.ID, now.Add(9*time.Minute)) || q.Live(front.ID, now.Add(10*time.Minute)) {
		t.Fatal("in-flight batch expiration changed")
	}
	if _, ok := q.Front(now.Add(10 * time.Minute)); ok {
		t.Fatal("expired request retained")
	}
	if !q.Add(appinstance.NewRequest(nil), now.Add(10*time.Minute)) {
		t.Fatal("expired entries consume capacity")
	}
	q.Close()
	if _, ok := q.Front(now); ok {
		t.Fatal("closed queue retained secrets")
	}
	if q.Add(appinstance.NewRequest(nil), now) {
		t.Fatal("closed queue accepted request")
	}
}

func TestQueueDuplicateAndLateArrivalDuringForwarding(t *testing.T) {
	now := time.Now()
	q := &Queue{}
	first := appinstance.NewRequest([]string{"one"})
	second := appinstance.NewRequest([]string{"two"})
	q.Add(first, now)
	if !q.Add(first, now) || !q.Add(second, now) {
		t.Fatal("enqueue failed")
	}
	q.Done(first.ID)
	r, ok := q.Front(now)
	if !ok || r.ID != second.ID {
		t.Fatal("new launch event lost during forwarding")
	}
	q.Done(second.ID)
	if _, ok := q.Front(now); ok {
		t.Fatal("duplicate enqueued twice")
	}
}
