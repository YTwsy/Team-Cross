package server

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/domain"
)

const nativeOpenTestID = "e4243c5a-2878-4df1-8a3f-5b0123456789"

func nativeOpenFixture() domain.SessionSnapshot {
	snapshot := reviewFixture()
	snapshot.ID = uuid.NewString()
	snapshot.Source.SessionID = nativeOpenTestID
	snapshot.Source.ProviderVersion = "synthetic-version"
	snapshot.Source.NativeIDs = map[string]string{"threadId": nativeOpenTestID, "sessionId": "other-session-family"}
	return snapshot
}

func createNativeOpenReview(t *testing.T, app *App, snapshot domain.SessionSnapshot) domain.Thread {
	t.Helper()
	thread, err := app.store.CreateSessionReviewThread(context.Background(), "Open test", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return thread
}

func nativeOpenPath(threadID string) string {
	return "/api/v1/threads/" + threadID + "/sessions/open"
}

func TestNativeOpenRequestsOnlyExactDesktopURLAfterFreshCapability(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	snapshot := nativeOpenFixture()
	// Old evidence is only a source hint. A newly verified local capability may
	// be true even though this immutable capture predates its validation.
	snapshot.Capabilities.Open = false
	thread := createNativeOpenReview(t, app, snapshot)
	reads, opens := 0, 0
	app.snapshotReader = func(_ context.Context, provider, id string) (domain.SessionSnapshot, error) {
		reads++
		if provider != "codex" || id != nativeOpenTestID {
			t.Fatalf("wrong targeted read: %s %s", provider, id)
		}
		fresh := snapshot
		fresh.Capabilities.Open = true // Synthetic gate fixture, not native acceptance.
		return fresh, nil
	}
	app.nativeOpener = func(ctx context.Context, link string) error {
		opens++
		if link != "codex://threads/"+nativeOpenTestID {
			t.Fatalf("unexpected link: %s", link)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Fatal("OS request must be time bounded")
		}
		command, err := nativeDesktopOpenCommand(ctx, link)
		if err != nil || command.Path != "/usr/bin/open" || !reflect.DeepEqual(command.Args, []string{"/usr/bin/open", "-b", "com.openai.codex", link}) {
			t.Fatalf("opener must use fixed argv: %#v %v", command, err)
		}
		// Deliberately never run the command.
		return nil
	}
	response := requestJSON(t, app.Handler(), "POST", nativeOpenPath(thread.ID), nativeOpenRequest{SnapshotID: snapshot.ID, ConfirmOpen: true}, nil)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"target":"codex-desktop"`) || !strings.Contains(response.Body.String(), `"status":"requested"`) || !strings.Contains(response.Body.String(), "尚未验证") {
		t.Fatalf("request-only outcome: %d %s", response.Code, response.Body.String())
	}
	if reads != 1 || opens != 1 || strings.Contains(response.Body.String(), "SECRET") {
		t.Fatalf("unexpected access: reads=%d opens=%d body=%s", reads, opens, response.Body.String())
	}
	runs, _ := app.store.ListAgentRuns(context.Background(), thread.ID)
	rounds, _ := app.store.ListRounds(context.Background(), thread.ID)
	snapshots, _ := app.store.ListSessionSnapshots(context.Background(), thread.ID)
	if len(runs) != 0 || len(rounds) != 1 || len(snapshots) != 1 || app.bridge != nil {
		t.Fatal("open created execution or changed immutable evidence")
	}
}

func TestNativeOpenRejectsUnconfirmedOrInjectedRequestFields(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	snapshot := nativeOpenFixture()
	thread := createNativeOpenReview(t, app, snapshot)
	app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) {
		t.Fatal("invalid request must not read Provider history")
		return domain.SessionSnapshot{}, nil
	}
	app.nativeOpener = func(context.Context, string) error { t.Fatal("invalid request opened an app"); return nil }
	for _, extra := range []string{"url", "provider", "sessionId", "cwd", "prompt", "hostId", "fork"} {
		t.Run(extra, func(t *testing.T) {
			response := requestJSON(t, app.Handler(), "POST", nativeOpenPath(thread.ID), map[string]any{"snapshotId": snapshot.ID, "confirmOpen": true, extra: "injected"}, nil)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("extra field accepted: %d %s", response.Code, response.Body.String())
			}
		})
	}
	for _, input := range []nativeOpenRequest{{SnapshotID: snapshot.ID}, {ConfirmOpen: true}} {
		response := requestJSON(t, app.Handler(), "POST", nativeOpenPath(thread.ID), input, nil)
		if response.Code != 422 {
			t.Fatalf("missing confirmation accepted: %s", response.Body.String())
		}
	}
}

func TestNativeOpenRejectsInvalidSourceBeforeProviderRead(t *testing.T) {
	for _, kind := range []string{"provider", "identity-kind", "managed", "unknown-surface", "non-uuid", "nil-uuid", "urn", "query", "path", "native-alias"} {
		t.Run(kind, func(t *testing.T) {
			app := newIntegrationApp(t, t.TempDir())
			snapshot := nativeOpenFixture()
			switch kind {
			case "provider":
				snapshot.Source.Provider = "claude"
			case "identity-kind":
				snapshot.Source.IdentityKind = "sessionId"
			case "managed":
				snapshot.Source.Surface = "managed"
			case "unknown-surface":
				snapshot.Source.Surface = "unknown"
			case "non-uuid":
				snapshot.Source.SessionID = "native-fake"
			case "nil-uuid":
				snapshot.Source.SessionID = uuid.Nil.String()
			case "urn":
				snapshot.Source.SessionID = "urn:uuid:" + nativeOpenTestID
			case "query":
				snapshot.Source.SessionID += "?prompt=RUN"
			case "path":
				snapshot.Source.SessionID += "/new"
			case "native-alias":
				snapshot.Source.NativeIDs["threadId"] = uuid.NewString()
			}
			thread := createNativeOpenReview(t, app, snapshot)
			app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) {
				t.Fatal("invalid source was read")
				return snapshot, nil
			}
			app.nativeOpener = func(context.Context, string) error { t.Fatal("invalid source was opened"); return nil }
			response := requestJSON(t, app.Handler(), "POST", nativeOpenPath(thread.ID), nativeOpenRequest{SnapshotID: snapshot.ID, ConfirmOpen: true}, nil)
			if response.Code != 422 {
				t.Fatalf("invalid source accepted: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestNativeOpenNeverTrustsStaleOrMismatchedCapability(t *testing.T) {
	for _, kind := range []string{"open-false", "read-false", "version", "identity", "surface", "session", "native-alias", "reader-error"} {
		t.Run(kind, func(t *testing.T) {
			app := newIntegrationApp(t, t.TempDir())
			snapshot := nativeOpenFixture()
			snapshot.Capabilities.Open = true
			thread := createNativeOpenReview(t, app, snapshot)
			app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) {
				fresh := snapshot
				switch kind {
				case "open-false":
					fresh.Capabilities.Open = false
				case "read-false":
					fresh.Capabilities.Read = false
				case "version":
					fresh.Source.ProviderVersion = "new-unverified-version"
				case "identity":
					fresh.Source.IdentityKind = "sessionId"
				case "surface":
					fresh.Source.Surface = "managed"
				case "session":
					fresh.Source.SessionID = uuid.NewString()
				case "native-alias":
					fresh.Source.NativeIDs = map[string]string{"threadId": uuid.NewString()}
				case "reader-error":
					return fresh, errors.New("PRIVATE DIAGNOSTIC")
				}
				return fresh, nil
			}
			app.nativeOpener = func(context.Context, string) error { t.Fatal("unverified source was opened"); return nil }
			response := requestJSON(t, app.Handler(), "POST", nativeOpenPath(thread.ID), nativeOpenRequest{SnapshotID: snapshot.ID, ConfirmOpen: true}, nil)
			if response.Code != 422 || strings.Contains(response.Body.String(), "PRIVATE") {
				t.Fatalf("stale gate accepted or leaked: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestNativeOpenIsHostOnlyAndSnapshotBound(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	snapshot := nativeOpenFixture()
	thread := createNativeOpenReview(t, app, snapshot)
	other := createNativeOpenReview(t, app, nativeOpenFixture())
	remote, headers := installReviewShare(t, app, thread.ID, domain.ShareScope{SnapshotID: snapshot.ID, EntryIDs: []string{"public"}}, false)
	app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) {
		t.Fatal("remote/mismatched open read history")
		return snapshot, nil
	}
	app.nativeOpener = func(context.Context, string) error { t.Fatal("remote/mismatched open launched app"); return nil }
	response := requestJSON(t, remote, "POST", nativeOpenPath(thread.ID), nativeOpenRequest{SnapshotID: snapshot.ID, ConfirmOpen: true}, headers)
	if response.Code != http.StatusForbidden {
		t.Fatalf("remote open accepted: %d %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, app.Handler(), "POST", nativeOpenPath(other.ID), nativeOpenRequest{SnapshotID: snapshot.ID, ConfirmOpen: true}, nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-Thread snapshot accepted: %d %s", response.Code, response.Body.String())
	}
}

func TestNativeOpenRejectsManagedIdentityAcrossAllThreadsIncludingClosedHistory(t *testing.T) {
	for _, state := range []string{"running", "archived", "closed-no-ack", "closed-ack", "memory-only", "late-after-read"} {
		t.Run(state, func(t *testing.T) {
			app := newIntegrationApp(t, t.TempDir())
			snapshot := nativeOpenFixture()
			snapshot.Capabilities.Open = true
			thread := createNativeOpenReview(t, app, snapshot)
			other := createNativeOpenReview(t, app, nativeOpenFixture())
			install := func() {
				run := domain.AgentRun{ID: uuid.NewString(), ThreadID: other.ID, Provider: "codex", SessionID: strings.ToUpper(nativeOpenTestID), Status: state}
				if strings.HasPrefix(state, "closed") {
					run.Status = "closed"
				}
				if state == "closed-ack" {
					run.ClosedAt = time.Now().UTC()
				}
				if state == "memory-only" {
					app.mu.Lock()
					app.runsByID[run.ID] = &managedRun{ID: run.ID, ThreadID: other.ID, Provider: run.Provider, SessionID: run.SessionID, Status: "running"}
					app.mu.Unlock()
					return
				}
				if _, err := app.store.CreateAgentRun(context.Background(), run); err != nil {
					t.Fatal(err)
				}
			}
			if state != "late-after-read" {
				install()
			}
			app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) {
				if state == "late-after-read" {
					install()
				}
				return snapshot, nil
			}
			app.nativeOpener = func(context.Context, string) error { t.Fatal("managed native identity opened"); return nil }
			response := requestJSON(t, app.Handler(), "POST", nativeOpenPath(thread.ID), nativeOpenRequest{SnapshotID: snapshot.ID, ConfirmOpen: true}, nil)
			if response.Code != http.StatusConflict {
				t.Fatalf("managed source accepted: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestNativeOpenUnknownRunRequiresDurableClosedAcknowledgment(t *testing.T) {
	for _, state := range []string{"starting", "closed-missing-time", "closed-ack", "memory-closed", "transition", "command"} {
		t.Run(state, func(t *testing.T) {
			app := newIntegrationApp(t, t.TempDir())
			snapshot := nativeOpenFixture()
			snapshot.Capabilities.Open = true
			thread := createNativeOpenReview(t, app, snapshot)
			other := createNativeOpenReview(t, app, nativeOpenFixture())
			run := domain.AgentRun{ID: uuid.NewString(), ThreadID: other.ID, Provider: "codex", Status: "closed"}
			switch state {
			case "starting":
				run.Status = "starting"
			case "closed-ack":
				run.ClosedAt = time.Now().UTC()
			case "transition":
				app.switching[other.ID] = true
			case "command":
				app.agentOps[other.ID] = 1
			case "memory-closed":
				app.runsByID[run.ID] = &managedRun{ID: run.ID, ThreadID: other.ID, Provider: "codex", Status: "closed"}
			}
			if state == "starting" || state == "closed-missing-time" || state == "closed-ack" {
				if _, err := app.store.CreateAgentRun(context.Background(), run); err != nil {
					t.Fatal(err)
				}
			}
			app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) { return snapshot, nil }
			opens := 0
			app.nativeOpener = func(context.Context, string) error { opens++; return nil }
			response := requestJSON(t, app.Handler(), "POST", nativeOpenPath(thread.ID), nativeOpenRequest{SnapshotID: snapshot.ID, ConfirmOpen: true}, nil)
			if state == "closed-ack" {
				if response.Code != http.StatusAccepted || opens != 1 {
					t.Fatalf("confirmed unrelated empty Run prevented request: %d %s", response.Code, response.Body.String())
				}
			} else if response.Code != http.StatusConflict || opens != 0 {
				t.Fatalf("unconfirmed state accepted: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestNativeOpenFencesConcurrentTransitionAndFailureNeverStartsAgent(t *testing.T) {
	app := newIntegrationApp(t, t.TempDir())
	snapshot := nativeOpenFixture()
	snapshot.Capabilities.Open = true
	thread := createNativeOpenReview(t, app, snapshot)
	app.snapshotReader = func(context.Context, string, string) (domain.SessionSnapshot, error) { return snapshot, nil }
	entered := make(chan struct{})
	release := make(chan struct{})
	app.nativeOpener = func(context.Context, string) error {
		if app.mu.TryLock() {
			app.mu.Unlock()
			t.Error("open must retain the Writer-admission fence")
		}
		close(entered)
		<-release
		return errors.New("PRIVATE OPEN ERROR")
	}
	responseReady := make(chan int, 1)
	go func() {
		response := requestJSON(t, app.Handler(), "POST", nativeOpenPath(thread.ID), nativeOpenRequest{SnapshotID: snapshot.ID, ConfirmOpen: true}, nil)
		if strings.Contains(response.Body.String(), "PRIVATE") {
			t.Error("OS failure leaked private diagnostics")
		}
		responseReady <- response.Code
	}()
	<-entered
	transitionReady := make(chan struct{})
	go func() {
		app.beginAgentTransition(thread.ID, "codex", false, false)
		close(transitionReady)
	}()
	select {
	case <-transitionReady:
		t.Error("transition passed the open fence")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if status := <-responseReady; status != http.StatusBadGateway {
		t.Fatalf("failed request status=%d", status)
	}
	<-transitionReady
	app.endAgentTransition(thread.ID)
	runs, _ := app.store.ListAgentRuns(context.Background(), thread.ID)
	if len(runs) != 0 || app.bridge != nil {
		t.Fatal("failed open started Agent work")
	}
}

func TestNativeOpenCommandRejectsAnyNonMinimalLink(t *testing.T) {
	for _, value := range []string{"codex://threads/new", "codex://threads/" + nativeOpenTestID + "?prompt=x", "codex://threads/" + nativeOpenTestID + "/x", "https://example.com", "/tmp/app", "codex://threads/" + nativeOpenTestID + "#x"} {
		if command, err := nativeDesktopOpenCommand(context.Background(), value); err == nil || command != nil {
			t.Fatalf("unsafe argv target accepted: %s", value)
		}
	}
}
