package storage

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"teamcross/internal/domain"
)

func identityTestRun(t *testing.T, store *Store) domain.AgentRun {
	t.Helper()
	thread := createTestThread(t, store)
	run, err := store.CreateAgentRun(context.Background(), domain.AgentRun{ID: "run", ThreadID: thread.ID, Provider: "claude", SessionID: "claude-pending-run"})
	if err != nil {
		t.Fatal(err)
	}
	if run.SessionID != "" {
		t.Fatalf("legacy placeholder exposed as native identity: %q", run.SessionID)
	}
	return run
}

func readIdentityBinding(t *testing.T, store *Store, runID string) map[string]any {
	t.Helper()
	payload, err := store.GetRunBinding(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	var binding map[string]any
	if err := json.Unmarshal(payload, &binding); err != nil {
		t.Fatal(err)
	}
	return binding
}

func assertStoredIdentity(t *testing.T, store *Store, runID, want string) {
	t.Helper()
	run, err := store.GetAgentRun(context.Background(), runID)
	if err != nil || run.SessionID != want {
		t.Fatalf("run identity=%#v err=%v; want %s", run, err, want)
	}
	binding := readIdentityBinding(t, store, runID)
	ref, ok := binding["sessionRef"].(map[string]any)
	if !ok || ref["sessionId"] != want || ref["provider"] != "claude" {
		t.Fatalf("non-canonical binding: %#v", binding)
	}
}

func TestDelayedSessionIdentityPreservesRunBindingAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	store, err := Open(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	run := identityTestRun(t, store)
	base := []byte(`{"mode":"managed","writer":"teamcross","host":"local","executionRoot":"/owned/worktree","originThreadId":"source-thread","fromRoundId":"round-4","capabilities":{"send":true,"nativeTakeControl":false},"future":{"retain":true},"sessionRef":{"provider":"claude","sessionId":"claude-pending-run","surface":"managed","providerMetadata":"keep"}}`)
	if err := store.SaveRunBinding(ctx, run.ID, base); err != nil {
		t.Fatal(err)
	}
	assertStoredIdentity(t, store, run.ID, "")
	for _, candidate := range []string{"actual-native", "", "claude-pending-run", "actual-native"} {
		got, err := store.BindAgentRunIdentity(ctx, run.ID, "claude", candidate)
		if err != nil || got != "actual-native" {
			t.Fatalf("bind %q = %q, %v", candidate, got, err)
		}
	}
	// A stale continuation payload may still carry the placeholder. Identity
	// always comes from agent_runs; omitted base/provenance fields survive.
	if err := store.SaveRunBinding(ctx, run.ID, []byte(`{"newMetadata":"new","sessionRef":{"sessionId":"claude-pending-run","provider":"stale"}}`)); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"running", "archived", "idle", "closed", "running"} {
		closedAt := time.Time{}
		if status == "closed" {
			closedAt = time.Now().UTC()
		}
		if err := store.UpdateAgentRun(ctx, run.ID, status, closedAt); err != nil {
			t.Fatal(err)
		}
	}
	for _, candidate := range []string{"different-native", "another-native"} {
		if got, err := store.BindAgentRunIdentity(ctx, run.ID, "claude", candidate); got != "actual-native" || !errors.Is(err, domain.ErrSessionIdentityConflict) {
			t.Fatalf("conflicting bind = %q, %v", got, err)
		}
	}
	if _, err := store.BindAgentRunIdentity(ctx, run.ID, "codex", "actual-native"); !errors.Is(err, domain.ErrSessionIdentityConflict) {
		t.Fatalf("mismatched Provider accepted: %v", err)
	}
	assertStoredIdentity(t, store, run.ID, "actual-native")
	binding := readIdentityBinding(t, store, run.ID)
	for key, want := range map[string]any{"originThreadId": "source-thread", "fromRoundId": "round-4", "executionRoot": "/owned/worktree", "mode": "managed", "writer": "teamcross", "host": "local", "newMetadata": "new"} {
		if binding[key] != want {
			t.Fatalf("binding lost %s: %#v", key, binding)
		}
	}
	if binding["future"] == nil || binding["capabilities"] == nil || binding["sessionRef"].(map[string]any)["providerMetadata"] != "keep" {
		t.Fatalf("extension fields lost: %#v", binding)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	assertStoredIdentity(t, store, run.ID, "actual-native")
	stored, _ := store.GetAgentRun(ctx, run.ID)
	if stored.Status != "closed" || stored.ClosedAt.IsZero() {
		t.Fatalf("stale status reopened Run: %#v", stored)
	}
}

func TestSessionIdentityBeforeBindingAndAtomicFailure(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	store.DB().SetMaxOpenConns(1)
	run := identityTestRun(t, store)
	if _, err := store.BindAgentRunIdentity(ctx, run.ID, "claude", "native-before-binding"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunBinding(ctx, run.ID, []byte(`{"sessionRef":{"sessionId":""},"fromRoundId":"round"}`)); err != nil {
		t.Fatal(err)
	}
	assertStoredIdentity(t, store, run.ID, "native-before-binding")
	// Invalid input cannot partially replace the existing durable binding.
	before, _ := store.GetRunBinding(ctx, run.ID)
	if err := store.SaveRunBinding(ctx, run.ID, []byte(`{"sessionRef":null,"fromRoundId":"wrong"}`)); err == nil {
		t.Fatal("invalid sessionRef accepted")
	}
	after, _ := store.GetRunBinding(ctx, run.ID)
	if string(before) != string(after) {
		t.Fatal("failed binding write changed metadata")
	}
	// Simulate a legacy row with a placeholder; read projections must not
	// expose it, and failure to update its binding must roll back adoption.
	if _, err := store.DB().ExecContext(ctx, "UPDATE agent_runs SET session_id='claude-pending-run' WHERE id='run'"); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetAgentRun(ctx, run.ID)
	if stored.SessionID != "" {
		t.Fatal("legacy row exposed placeholder")
	}
	if _, err := store.DB().ExecContext(ctx, "CREATE TRIGGER reject_binding BEFORE UPDATE ON run_bindings BEGIN SELECT RAISE(ABORT, 'test'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindAgentRunIdentity(ctx, run.ID, "claude", "must-roll-back"); err == nil {
		t.Fatal("binding failure ignored")
	}
	stored, _ = store.GetAgentRun(ctx, run.ID)
	if stored.SessionID != "" {
		t.Fatalf("partial identity commit: %#v", stored)
	}
}

func TestConcurrentSessionIdentityBindingAndStatusNeverRegress(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	run := identityTestRun(t, store)
	if err := store.SaveRunBinding(ctx, run.ID, []byte(`{"fromRoundId":"round","originThreadId":"origin"}`)); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsSeen := make(chan error, 60)
	var workers sync.WaitGroup
	for i := 0; i < 60; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-start
			var err error
			switch i % 5 {
			case 0:
				_, err = store.BindAgentRunIdentity(ctx, run.ID, "claude", "actual")
			case 1:
				_, err = store.BindAgentRunIdentity(ctx, run.ID, "claude", "claude-pending-run")
			case 2:
				err = store.SaveRunBinding(ctx, run.ID, []byte(`{"sessionRef":{"sessionId":"claude-pending-run"},"host":"local"}`))
			case 3:
				err = store.UpdateAgentRun(ctx, run.ID, "running", time.Time{})
			case 4:
				err = store.UpdateAgentRun(ctx, run.ID, "closed", time.Now().UTC())
			}
			if err != nil {
				errorsSeen <- err
			}
		}(i)
	}
	close(start)
	workers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Error(err)
	}
	assertStoredIdentity(t, store, run.ID, "actual")
	binding := readIdentityBinding(t, store, run.ID)
	if binding["fromRoundId"] != "round" || binding["originThreadId"] != "origin" {
		t.Fatalf("lost source: %#v", binding)
	}
	stored, _ := store.GetAgentRun(ctx, run.ID)
	if stored.Status != "closed" || stored.ClosedAt.IsZero() {
		t.Fatalf("closed lifecycle regressed: %#v", stored)
	}
}

func TestCompetingNativeIdentitiesBindExactlyOnce(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	run := identityTestRun(t, store)
	if err := store.SaveRunBinding(ctx, run.ID, []byte(`{"originThreadId":"origin"}`)); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	type result struct {
		identity string
		err      error
	}
	results := make(chan result, 2)
	for _, candidate := range []string{"first-candidate", "other-candidate"} {
		go func(candidate string) {
			<-start
			identity, err := store.BindAgentRunIdentity(ctx, run.ID, "claude", candidate)
			results <- result{identity, err}
		}(candidate)
	}
	close(start)
	first, second := <-results, <-results
	if first.identity == "" || first.identity != second.identity {
		t.Fatalf("competing results: %#v %#v", first, second)
	}
	successes := 0
	conflicts := 0
	for _, result := range []result{first, second} {
		if result.err == nil {
			successes++
		} else if errors.Is(result.err, domain.ErrSessionIdentityConflict) {
			conflicts++
		} else {
			t.Fatal(result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	assertStoredIdentity(t, store, run.ID, first.identity)
}
