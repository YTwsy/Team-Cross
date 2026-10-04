package mcpevents

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrRevoked = errors.New("space access revoked")

type Observation struct {
	Key       string         `json:"key"`
	Name      string         `json:"name"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data"`
}
type Source func(context.Context, string) ([]Observation, error)
type ReadTool func(context.Context, string, string, map[string]any) (any, error)

type Grant struct {
	ID      string    `json:"id"`
	SpaceID string    `json:"spaceId"`
	Name    string    `json:"name"`
	State   string    `json:"state"`
	Hash    string    `json:"hash,omitempty"`
	Expires time.Time `json:"expiresAt"`
}
type Subscription struct {
	ID             string            `json:"id"`
	GrantID        string            `json:"grantId"`
	Name           string            `json:"name"`
	Arguments      map[string]string `json:"arguments"`
	URL            string            `json:"url"`
	Secret         string            `json:"secret,omitempty"`
	PreviousSecret string            `json:"previousSecret,omitempty"`
	RotateUntil    time.Time         `json:"rotateUntil,omitempty"`
	VerifiedAt     time.Time         `json:"verifiedAt"`
	Expires        time.Time         `json:"refreshBefore"`
	State          string            `json:"state"`
	Baseline       map[string]string `json:"baseline,omitempty"`
	Generation     int               `json:"generation"`
}
type Event struct {
	CreatedAt      time.Time `json:"createdAt"`
	ID             string    `json:"eventId"`
	SubscriptionID string    `json:"subscriptionId"`
	Observation
	State       string     `json:"state"`
	Attempts    int        `json:"attempts"`
	NextAttempt time.Time  `json:"nextAttempt"`
	ReceivedAt  *time.Time `json:"receivedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	Error       string     `json:"error,omitempty"`
}
type state struct {
	Grants        map[string]Grant        `json:"grants"`
	Subscriptions map[string]Subscription `json:"subscriptions"`
	Events        map[string]Event        `json:"events"`
}
type Manager struct {
	mu       sync.Mutex
	path     string
	state    state
	source   Source
	read     ReadTool
	client   *http.Client
	inflight map[string]context.CancelFunc
	worker   sync.Mutex
}

func clone[T any](v T) T {
	raw, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}
func digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func Open(dir string, source Source, read ReadTool) (*Manager, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	m := &Manager{path: filepath.Join(dir, "subscriptions.json"), source: source, read: read, client: webhookClient(), inflight: map[string]context.CancelFunc{}, state: state{map[string]Grant{}, map[string]Subscription{}, map[string]Event{}}}
	raw, err := os.ReadFile(m.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err = json.Unmarshal(raw, &m.state); err != nil {
			return nil, err
		}
	}
	if m.state.Grants == nil {
		m.state.Grants = map[string]Grant{}
	}
	if m.state.Subscriptions == nil {
		m.state.Subscriptions = map[string]Subscription{}
	}
	if m.state.Events == nil {
		m.state.Events = map[string]Event{}
	}
	return m, nil
}

func (m *Manager) commit(next state) error {
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".events-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, m.path)
	}
	if err == nil {
		m.state = next
	}
	return err
}

func (m *Manager) CreateGrant(ctx context.Context, id, spaceID, name string) (Grant, string, error) {
	if _, err := uuid.Parse(id); err != nil || strings.TrimSpace(name) == "" || len([]rune(name)) > 80 {
		return Grant{}, "", fmt.Errorf("invalid event access request")
	}
	if _, err := m.source(ctx, spaceID); err != nil {
		return Grant{}, "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.state.Grants[id]; exists {
		return Grant{}, "", fmt.Errorf("access already created; revoke it before creating a replacement credential")
	}
	if len(m.state.Grants) >= 64 {
		return Grant{}, "", fmt.Errorf("event access limit reached")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return Grant{}, "", err
	}
	token := "TC_EVT_" + base64.RawURLEncoding.EncodeToString(key)
	g := Grant{ID: id, SpaceID: spaceID, Name: strings.TrimSpace(name), State: "active", Hash: digest(token), Expires: time.Now().Add(7 * 24 * time.Hour)}
	next := clone(m.state)
	next.Grants[id] = g
	if err := m.commit(next); err != nil {
		return Grant{}, "", err
	}
	g.Hash = ""
	return g, token, nil
}

func (m *Manager) authenticate(token string) (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hash := digest(token)
	for _, g := range m.state.Grants {
		if g.State == "active" && time.Now().Before(g.Expires) && subtle.ConstantTimeCompare([]byte(g.Hash), []byte(hash)) == 1 {
			return g, nil
		}
	}
	return Grant{}, fmt.Errorf("event credential is unavailable")
}

func (m *Manager) View() any {
	m.mu.Lock()
	defer m.mu.Unlock()
	grants := []Grant{}
	subs := []Subscription{}
	events := []Event{}
	for _, g := range m.state.Grants {
		g.Hash = ""
		grants = append(grants, g)
	}
	for _, s := range m.state.Subscriptions {
		s.Secret, s.PreviousSecret = "", ""
		s.Baseline = nil
		subs = append(subs, s)
	}
	for _, e := range m.state.Events {
		events = append(events, clone(e))
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Timestamp.After(events[j].Timestamp) })
	if len(events) > 50 {
		events = events[:50]
	}
	return map[string]any{"connections": grants, "subscriptions": subs, "events": events}
}

func (m *Manager) Change(id, kind, action string) error {
	if action != "active" && action != "paused" && action != "revoked" {
		return fmt.Errorf("invalid subscription action")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next := clone(m.state)
	if kind == "connection" {
		g, ok := next.Grants[id]
		if !ok || g.State == "revoked" {
			return fmt.Errorf("event access unavailable")
		}
		g.State = action
		next.Grants[id] = g
		for sid, s := range next.Subscriptions {
			if s.GrantID == id {
				if cancel := m.inflight[sid]; cancel != nil {
					cancel()
				}
				if action == "revoked" {
					s.State = "revoked"
					s.Secret, s.PreviousSecret = "", ""
					s.Generation++
					next.Subscriptions[sid] = s
				}
			}
		}
	} else if kind == "subscription" {
		s, ok := next.Subscriptions[id]
		if !ok || s.State == "revoked" {
			return fmt.Errorf("subscription unavailable")
		}
		s.State = action
		s.Generation++
		if action == "revoked" {
			s.Secret, s.PreviousSecret = "", ""
		}
		next.Subscriptions[id] = s
		if cancel := m.inflight[id]; cancel != nil {
			cancel()
		}
	} else {
		return fmt.Errorf("invalid subscription kind")
	}
	return m.commit(next)
}

var eventNames = []string{"space.brief.updated", "space.discussion.updated", "space.request.completed"}

func validArguments(g Grant, name string, args map[string]string) error {
	found := false
	for _, candidate := range eventNames {
		found = found || candidate == name
	}
	if !found {
		return fmt.Errorf("unknown event")
	}
	if args["spaceId"] != g.SpaceID {
		return fmt.Errorf("event access is restricted to its selected space")
	}
	for key, value := range args {
		if key != "spaceId" && (key != "annotationId" || name != "space.discussion.updated") && (key != "requestId" || name != "space.request.completed") {
			return fmt.Errorf("unsupported event filter")
		}
		if value == "" || len(value) > 256 {
			return fmt.Errorf("invalid event filter")
		}
	}
	return nil
}
func matching(s Subscription, o Observation) bool {
	if s.Name != o.Name {
		return false
	}
	for key, value := range s.Arguments {
		if o.Data[key] != value {
			return false
		}
	}
	return true
}

type subscribeInput struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments"`
	Delivery  struct {
		Mode   string `json:"mode"`
		URL    string `json:"url"`
		Secret string `json:"secret"`
	} `json:"delivery"`
	Cursor *string         `json:"cursor"`
	TTL    json.RawMessage `json:"ttlMs"`
}
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return e.Message }

func (m *Manager) subscribe(ctx context.Context, g Grant, in subscribeInput, unsubscribe bool) (any, error) {
	if err := validArguments(g, in.Name, in.Arguments); err != nil {
		return nil, err
	}
	if in.Delivery.Mode != "webhook" {
		return nil, fmt.Errorf("only webhook delivery is supported")
	}
	if err := callbackURL(in.Delivery.URL); err != nil {
		return nil, &RPCError{-32015, "Callback endpoint rejected", map[string]string{"reason": "invalid_url"}}
	}
	id := digest([]any{g.ID, in.Delivery.URL, in.Name, in.Arguments})
	m.mu.Lock()
	old := clone(m.state.Subscriptions[id])
	m.mu.Unlock()
	if unsubscribe {
		if old.ID == "" || old.State == "revoked" {
			return map[string]any{}, nil
		}
		return map[string]any{}, m.Change(id, "subscription", "revoked")
	}
	if old.State == "revoked" {
		return nil, fmt.Errorf("subscription was revoked; create a new space connection to subscribe again")
	}
	if in.Cursor != nil {
		return nil, fmt.Errorf("these event types do not support replay; cursor must be null")
	}
	if _, err := secretKey(in.Delivery.Secret); err != nil {
		return nil, err
	}
	ttl := 24 * time.Hour
	if len(in.TTL) > 0 && string(in.TTL) != "null" {
		var ms int64
		if json.Unmarshal(in.TTL, &ms) != nil || ms <= 0 {
			return nil, fmt.Errorf("invalid ttlMs")
		}
		ttl = min(7*24*time.Hour, max(time.Minute, time.Duration(min(ms, int64((7*24*time.Hour)/time.Millisecond)))*time.Millisecond))
	}
	now := time.Now()
	sub := Subscription{ID: id, GrantID: g.ID, Name: in.Name, Arguments: in.Arguments, URL: in.Delivery.URL, Secret: in.Delivery.Secret, State: "active", Expires: minTime(now.Add(ttl), g.Expires), Generation: old.Generation + 1}
	if old.State == "paused" {
		sub.State = "paused"
	}
	if old.Secret == sub.Secret {
		sub.PreviousSecret, sub.RotateUntil = old.PreviousSecret, old.RotateUntil
	}
	if old.Secret != sub.Secret && old.Secret != "" {
		sub.PreviousSecret, sub.RotateUntil = old.Secret, now.Add(5*time.Minute)
	}
	// Verification caching is bounded and belongs to this principal/callback.
	if old.State == "active" && now.Sub(old.VerifiedAt) < 5*time.Minute && old.Secret == sub.Secret {
		sub.VerifiedAt = old.VerifiedAt
	} else {
		challenge := uuid.NewString()
		body, _ := json.Marshal(map[string]string{"type": "verification", "challenge": challenge})
		status, data, err := postWebhook(ctx, m.client, sub, "verify_"+uuid.NewString(), body)
		var echo struct {
			Challenge string `json:"challenge"`
		}
		_ = json.Unmarshal(data, &echo)
		if err != nil || status < 200 || status >= 300 || subtle.ConstantTimeCompare([]byte(echo.Challenge), []byte(challenge)) != 1 {
			return nil, &RPCError{-32015, "Callback verification failed", map[string]string{"reason": "challenge_failed"}}
		}
		sub.VerifiedAt = now
	}
	observations, err := m.source(ctx, g.SpaceID)
	if err != nil {
		return nil, err
	}
	sub.Baseline = map[string]string{}
	for _, o := range observations {
		if matching(sub, o) {
			sub.Baseline[o.Key] = digest(o)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.state.Grants[g.ID]
	if current.State != "active" || time.Now().After(current.Expires) {
		return nil, fmt.Errorf("event access revoked during verification")
	}
	latest := m.state.Subscriptions[id]
	if latest.Generation != old.Generation {
		return nil, fmt.Errorf("subscription changed during verification; inspect it before retrying")
	}
	if old.State == "active" || old.State == "paused" {
		sub.Baseline = old.Baseline
	}
	if len(m.state.Subscriptions) >= 256 && old.ID == "" {
		return nil, fmt.Errorf("subscription limit reached")
	}
	next := clone(m.state)
	next.Subscriptions[id] = sub
	if err := m.commit(next); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "refreshBefore": sub.Expires, "cursor": nil, "truncated": false}, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Tick persists observations before delivery. Only webhook transport is
// retried; it never invokes a model or repeats an external write tool.
func (m *Manager) Tick(ctx context.Context) {
	m.worker.Lock()
	defer m.worker.Unlock()
	m.mu.Lock()
	// Reap expired work even when every subscription is paused or expired.
	// A renewed lease must not replay an event cancelled at the old lease boundary.
	next := clone(m.state)
	changed := false
	for id, event := range next.Events {
		sub := next.Subscriptions[event.SubscriptionID]
		grant := next.Grants[sub.GrantID]
		if event.State == "pending" && (sub.State == "revoked" || grant.State == "revoked" || time.Now().After(sub.Expires) || time.Now().After(grant.Expires)) {
			event.State, event.Error = "cancelled", "Event access revoked or expired"
			next.Events[id], changed = event, true
		}
		if event.State != "pending" && time.Since(event.CreatedAt) > 7*24*time.Hour {
			delete(next.Events, id)
			changed = true
		}
	}
	if changed {
		if err := m.commit(next); err != nil {
			m.mu.Unlock()
			return
		}
	}
	subs := clone(m.state.Subscriptions)
	m.mu.Unlock()
	for _, sub := range subs {
		if ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		g := m.state.Grants[sub.GrantID]
		m.mu.Unlock()
		if sub.State != "active" || g.State != "active" || time.Now().After(sub.Expires) || time.Now().After(g.Expires) {
			continue
		}
		observations, err := m.source(ctx, g.SpaceID)
		if err != nil {
			if errors.Is(err, ErrRevoked) {
				_ = m.Change(sub.ID, "subscription", "revoked")
			}
			continue
		}
		m.mu.Lock()
		if current := m.state.Subscriptions[sub.ID]; current.Generation != sub.Generation || current.State != "active" {
			m.mu.Unlock()
			continue
		}
		next := clone(m.state)
		live := next.Subscriptions[sub.ID]
		changed := false
		if live.Baseline == nil {
			live.Baseline = map[string]string{}
		}
		for _, o := range observations {
			if !matching(live, o) {
				continue
			}
			hash := digest(o)
			if live.Baseline[o.Key] == hash {
				continue
			}
			// Keep state bounded. Pause before dropping an undelivered event.
			if len(next.Events) >= 2048 {
				live.State = "paused"
				changed = true
				break
			}
			id := uuid.NewString()
			next.Events[id] = Event{ID: id, SubscriptionID: sub.ID, Observation: clone(o), State: "pending", NextAttempt: time.Now(), CreatedAt: time.Now()}
			live.Baseline[o.Key] = hash
			changed = true
		}
		next.Subscriptions[sub.ID] = live
		if changed {
			err = m.commit(next)
		}
		m.mu.Unlock()
		if err != nil || live.State != "active" {
			continue
		}
		m.deliver(ctx, sub.ID, g.SpaceID)
	}
}

func (m *Manager) deliver(ctx context.Context, id, spaceID string) {
	m.mu.Lock()
	events := clone(m.state.Events)
	m.mu.Unlock()
	for _, event := range events {
		if event.SubscriptionID != id || event.State != "pending" || time.Now().Before(event.NextAttempt) {
			continue
		}
		// Access is rechecked immediately before every attempt, not only when
		// an event is captured. A membership loss suppresses queued summaries.
		observations, err := m.source(ctx, spaceID)
		if err != nil {
			return
		}
		visible := false
		for _, observation := range observations {
			visible = visible || observation.Key == event.Key
		}
		m.mu.Lock()
		sub := m.state.Subscriptions[id]
		g := m.state.Grants[sub.GrantID]
		if sub.State != "active" || g.State != "active" || time.Now().After(sub.Expires) || time.Now().After(g.Expires) {
			m.mu.Unlock()
			return
		}
		if !visible {
			next := clone(m.state)
			e := next.Events[event.ID]
			e.State, e.Error = "cancelled", "Source no longer accessible"
			next.Events[e.ID] = e
			_ = m.commit(next)
			m.mu.Unlock()
			continue
		}
		call, cancel := context.WithCancel(ctx)
		m.inflight[id] = cancel
		m.mu.Unlock()
		body, _ := json.Marshal(map[string]any{"eventId": event.ID, "name": event.Name, "timestamp": event.Timestamp, "data": event.Data, "cursor": nil})
		status, _, err := postWebhook(call, m.client, sub, event.ID, body)
		cancel()
		m.mu.Lock()
		delete(m.inflight, id)
		if m.state.Subscriptions[id].Generation != sub.Generation {
			m.mu.Unlock()
			continue
		}
		next := clone(m.state)
		e := next.Events[event.ID]
		if e.State != "pending" {
			m.mu.Unlock()
			continue
		}
		e.Attempts++
		if err == nil && status >= 200 && status < 300 {
			e.State = "delivered"
			e.Error = ""
		} else {
			e.Error = "Webhook delivery not confirmed"
			if (status != 0 && status != 429 && status < 500) || e.Attempts >= 6 {
				e.State = "failed"
			} else {
				e.NextAttempt = time.Now().Add(time.Second * time.Duration(1<<e.Attempts))
			}
			if status == 410 {
				sub.State = "revoked"
				sub.Secret, sub.PreviousSecret = "", ""
				sub.Generation++
				next.Subscriptions[id] = sub
			}
		}
		next.Events[e.ID] = e
		_ = m.commit(next)
		m.mu.Unlock()
	}
}
