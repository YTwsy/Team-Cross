// Package launchqueue holds launch deliveries in memory until ownership is
// resolved. Receipts acknowledge enqueueing, never joining a collaboration.
package launchqueue

import (
	"net/url"
	"sync"
	"time"

	"teamcross/apps/desktop/internal/appinstance"
)

type entry struct {
	request appinstance.Request
	expires time.Time
}

type Queue struct {
	mu      sync.Mutex
	entries []entry
	closed  bool
}

// InvitationURL maps the isolated Preview scheme into the existing IPC/Core
// contract. Production registration remains the installation builder's concern.
func InvitationURL(raw string) (string, bool) {
	if len(raw) == 0 || len(raw) > 65536 {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "teamcross" && u.Scheme != "teamcross-desktop-preview") || u.Host != "join" || u.User != nil || u.Path != "" || u.Fragment != "" {
		return "", false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 1 || len(query["invite"]) != 1 || query.Get("invite") == "" {
		return "", false
	}
	u.Scheme = "teamcross"
	return u.String(), true
}

func (q *Queue) expire(now time.Time) {
	kept := q.entries[:0]
	for _, item := range q.entries {
		if now.Before(item.expires) {
			kept = append(kept, item)
		}
	}
	clear(q.entries[len(kept):])
	q.entries = kept
}

// Add accepts whole batches atomically. Pending URLs and batches are each
// bounded to 32; URL secrets are never persisted. The caller retains a rejected
// delivery and can retry with the same request ID.
func (q *Queue) Add(request appinstance.Request, now time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	q.expire(now)
	count := len(request.URLs)
	for _, item := range q.entries {
		if item.request.ID == request.ID {
			return true
		}
		count += len(item.request.URLs)
	}
	if count > 32 || len(q.entries) >= 32 {
		return false
	}
	request.URLs = append([]string(nil), request.URLs...)
	q.entries = append(q.entries, entry{request, now.Add(10 * time.Minute)})
	return true
}

// Front preserves the request identity across receipt timeouts. Copying prevents
// an event arriving during forwarding from modifying the in-flight batch.
func (q *Queue) Front(now time.Time) (appinstance.Request, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.expire(now)
	if len(q.entries) == 0 {
		return appinstance.Request{}, false
	}
	r := q.entries[0].request
	r.URLs = append([]string(nil), r.URLs...)
	return r, true
}

func (q *Queue) Done(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) > 0 && q.entries[0].request.ID == id {
		q.entries[0] = entry{}
		q.entries = q.entries[1:]
	}
}

// Live also bounds a batch being handled across several dialogs or slow Core
// requests. Expiration never grants a new ten-minute lifetime to its remaining
// invitations.
func (q *Queue) Live(id string, now time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return !q.closed && len(q.entries) > 0 && q.entries[0].request.ID == id && now.Before(q.entries[0].expires)
}

func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	clear(q.entries)
	q.entries = nil
	q.closed = true
}
