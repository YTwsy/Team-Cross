package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
)

func multiExecutionFixture(t *testing.T) (*App, *fakeRuntime, *Session) {
	t.Helper()
	a, reader, _ := fixture(t)
	// One provider per execution is essential: a shared fake would mask runtime
	// shutdown, approval-ID collisions and accidental handler replacement.
	a.Config.StartProcess = func(_, _, _, log string) (Runtime, error) {
		if filepath.Base(log) == "reader.log" {
			return reader, nil
		}
		return &fakeRuntime{source: reader.source, alive: true}, nil
	}
	space, err := a.CreateSpace(context.Background(), SpaceInput{RequestID: uuid.NewString(), Title: "Multiple executions"})
	if err != nil {
		t.Fatal(err)
	}
	return a, reader, space
}

func createInSpace(t *testing.T, a *App, reader *fakeRuntime, space *Session, mode string) *Session {
	t.Helper()
	in := CreateInput{SpaceID: space.record.ID, RequestID: uuid.NewString(), SourceID: reader.source.ID, WorkspaceMode: mode, Title: "Session " + mode}
	preview, err := a.Preview(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = preview.Hash
	s, err := a.Create(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func joinSpace(t *testing.T, space *Session, names ...string) *Joined {
	t.Helper()
	b, err := Open(Config{DataDir: t.TempDir(), Binary: "/missing-codex"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	b.Host = "Fixture member"
	if len(names) > 0 {
		b.Host = names[0]
	}
	j, err := b.Join(context.Background(), space.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func waitExecutionReleased(t *testing.T, s *Session) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		done := s.process == nil && s.stopping == nil
		s.mu.Unlock()
		if done {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("execution did not release its runtime")
}

func TestExecutionReferencesAndRuntimeTokensCannotCrossSiblings(t *testing.T) {
	a, reader, space := multiExecutionFixture(t)
	one := createInSpace(t, a, reader, space, "worktree")
	two := createInSpace(t, a, reader, space, "worktree")
	public, err := space.annotate(Annotation{Text: "public discussion"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Session{one, two} {
		if _, err := s.annotate(Annotation{Text: "private " + s.record.ID}, "owner"); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.WithValue(context.Background(), runtimeAnnotationKey{}, one.record.AnnotationToken)
	ref := LibraryReference{SpaceID: space.record.ID, ExecutionID: one.record.ID, Kind: "context"}
	sibling := ref
	sibling.ExecutionID = two.record.ID
	if contentHash(ref) == contentHash(sibling) {
		t.Fatal("references collapsed")
	}
	if _, err := a.libraryResource(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := a.libraryResource(ctx, sibling); err == nil {
		t.Fatal("runtime token read sibling")
	}
	if _, err := a.libraryResource(ctx, LibraryReference{SpaceID: space.record.ID, Kind: "annotation", AnnotationID: public.ID}); err != nil {
		t.Fatal("own runtime lost public discussion", err)
	}
	if _, err := a.libraryResource(context.Background(), LibraryReference{SpaceID: space.record.ID, Kind: "context"}); err == nil {
		t.Fatal("implicit execution context accepted")
	}
}

func TestDirectoryLeaseUsesWorktreeIdentityAndAliases(t *testing.T) {
	a, reader, space := multiExecutionFixture(t)
	one := createInSpace(t, a, reader, space, "existing")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(reader.source.Cwd, alias); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(reader.source.Cwd, "subdir")
	if err := os.Mkdir(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	two := a.newExecution(space, Record{ID: uuid.NewString(), SpaceID: space.record.ID})
	for _, path := range []string{alias, subdir} {
		if err := two.acquireDirectory(path); err == nil {
			t.Fatal("overlapping directory acquired", path)
		}
	}
	if err := one.Action(context.Background(), "pause"); err != nil {
		t.Fatal(err)
	}
	waitExecutionReleased(t, one)
	if err := two.acquireDirectory(alias); err != nil {
		t.Fatal("released directory still occupied", err)
	}
	two.releaseDirectory()
}

func TestSpaceAndExecutionCreationCannotReuseIdentity(t *testing.T) {
	a, reader, space := multiExecutionFixture(t)
	ctx := context.Background()
	in := CreateInput{SpaceID: space.record.ID, RequestID: space.record.ID, SourceID: reader.source.ID, WorkspaceMode: "worktree"}
	preview, err := a.Preview(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = preview.Hash
	if _, err = a.Create(ctx, in); err == nil {
		t.Fatal("execution reused its parent identity")
	}
	child := createInSpace(t, a, reader, space, "worktree")
	if _, err = a.CreateSpace(ctx, SpaceInput{RequestID: child.record.ID, Title: "Collision"}); err == nil {
		t.Fatal("space reused an execution identity")
	}
	if _, err = a.View(ctx, space.record.ID); err != nil {
		t.Fatal("identity rejection damaged the space", err)
	}

	in.RequestID = uuid.NewString()
	preview, err = a.Preview(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = preview.Hash
	start, results := make(chan struct{}), make(chan error, 2)
	go func() { <-start; _, err := a.Create(ctx, in); results <- err }()
	go func() {
		<-start
		_, err := a.CreateSpace(ctx, SpaceInput{RequestID: in.RequestID, Title: "Concurrent collision"})
		results <- err
	}()
	close(start)
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatal("concurrent space/execution creation must have exactly one winner", first, second)
	}
}

func TestMultipleExecutionsMembershipWritersAndLifecycle(t *testing.T) {
	a, reader, space := multiExecutionFixture(t)
	first := createInSpace(t, a, reader, space, "worktree")
	second := createInSpace(t, a, reader, space, "worktree")
	ctx := context.Background()
	if err := space.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	b, c := joinSpace(t, space), joinSpace(t, space)
	bid, cid := b.view(ctx)["selfId"].(string), c.view(ctx)["selfId"].(string)
	for _, child := range []*Session{first, second} {
		for _, member := range []*Joined{b, c} {
			view, err := member.executionView(ctx, child.record.ID)
			if err != nil || view["sessionId"] != child.record.SessionID || view["executionCwd"] != child.record.ExecutionCwd {
				t.Fatal("member could not read a session", view, err)
			}
			if err := member.request(ctx, "GET", remoteExecutionPath(child.record.ID, "context?kind=history"), nil, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := first.ActionFor(ctx, "handoff", bid); err != nil {
		t.Fatal(err)
	}
	if err := second.ActionFor(ctx, "handoff", cid); err != nil {
		t.Fatal(err)
	}
	if first.writer != bid || second.writer != cid {
		t.Fatal("writers were shared between executions")
	}
	input := RPCInput{Method: "turn/start", RequestID: "same-request", Params: map[string]any{"input": []any{}}}
	if err := b.request(ctx, "POST", remoteExecutionPath(second.record.ID, "rpc"), input, nil); err == nil {
		t.Fatal("B wrote to C's execution")
	}
	var wg sync.WaitGroup
	for _, pair := range []struct {
		j *Joined
		s *Session
	}{{b, first}, {c, second}} {
		wg.Add(1)
		go func(j *Joined, s *Session) {
			defer wg.Done()
			for range 2 {
				if err := j.request(ctx, "POST", remoteExecutionPath(s.record.ID, "rpc"), input, nil); err != nil {
					t.Error(err)
				}
			}
		}(pair.j, pair.s)
	}
	wg.Wait()
	for _, child := range []*Session{first, second} {
		child.mu.Lock()
		p := child.process.(*fakeRuntime)
		child.mu.Unlock()
		p.mu.Lock()
		writes := 0
		for _, method := range p.calls {
			if method == "turn/start" {
				writes++
			}
		}
		p.mu.Unlock()
		if writes != 1 {
			t.Fatalf("deduplication crossed sessions or replayed input: %d", writes)
		}
		child.onMessage(nativecodex.Message{ID: json.RawMessage(`7`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{}`)})
	}
	if err := first.Action(ctx, "end"); err != nil {
		t.Fatal(err)
	}
	if !space.share.HasMember(bid) || !space.share.HasMember(cid) {
		t.Fatal("stopping execution revoked space membership")
	}
	if first.writer != "owner" || second.writer != cid {
		t.Fatal("ending one session changed sibling input ownership")
	}
	if view, err := b.executionView(ctx, first.record.ID); err != nil || view["sessionId"] != first.record.SessionID {
		t.Fatal("ending execution hid its history", view, err)
	}
	if !first.online {
		t.Fatal("accepted work was killed before draining")
	}
	if err := first.Respond(ctx, "owner", json.RawMessage(`7`), map[string]any{}); err != nil {
		t.Fatal(err)
	}
	first.onMessage(nativecodex.Message{Method: "turn/completed"})
	waitExecutionReleased(t, first)
	second.mu.Lock()
	if !second.online || len(second.approvals) != 1 {
		t.Fatal("approval IDs or runtime were shared")
	}
	second.mu.Unlock()
	for _, path := range []string{first.record.WorkspaceRoot, second.record.WorkspaceRoot, reader.source.Cwd} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("ending execution deleted directory", err)
		}
	}
	if err := first.Action(ctx, "start"); err != nil {
		t.Fatal(err)
	}
	if view, err := b.executionView(ctx, first.record.ID); err != nil || view["sessionId"] != first.record.SessionID || first.writer != "owner" {
		t.Fatal("resume lost shared reading or restored previous input ownership", view, err)
	}
	if first.record.SessionID == second.record.SessionID {
		t.Fatal("native IDs collapsed")
	}
}

func TestExecutionStorageAndFailureLeaveSpaceUsable(t *testing.T) {
	a, reader, space := multiExecutionFixture(t)
	first := createInSpace(t, a, reader, space, "existing")
	in := CreateInput{SpaceID: space.record.ID, RequestID: uuid.NewString(), SourceID: reader.source.ID, WorkspaceMode: "existing"}
	preview, err := a.Preview(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = preview.Hash
	failed, err := a.Create(context.Background(), in)
	if err == nil || !strings.Contains(err.Error(), "工作目录") {
		t.Fatal("same worktree allowed concurrent runtimes", err)
	}
	if failed == nil || failed.record.State != "error" || space.record.State != "ready" || !first.online {
		t.Fatal("child failure corrupted the space or sibling")
	}
	first.mu.Lock()
	first.record.Commands["uncertain"] = Command{ID: "uncertain", State: "pending"}
	if err := first.saveLocked(); err != nil {
		t.Fatal(err)
	}
	first.mu.Unlock()
	if err := first.Action(context.Background(), "pause"); err != nil {
		t.Fatal(err)
	}
	waitExecutionReleased(t, first)
	third := createInSpace(t, a, reader, space, "existing")
	if third.record.SessionID == first.record.SessionID {
		t.Fatal("creation resumed an existing native session")
	}
	a.Close()
	starts := 0
	cfg := a.Config
	cfg.StartProcess = func(string, string, string, string) (Runtime, error) { starts++; return nil, nil }
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if starts != 0 || len(reopened.executions) != 3 || len(reopened.sessions) != 1 {
		t.Fatal("reload started work or lost children")
	}
	restored := reopened.executions[first.record.ID]
	if restored.record.SessionID != first.record.SessionID || restored.record.Commands["uncertain"].State != "unknown" {
		t.Fatal("restart lost identity or left a pending write replayable")
	}
	if reopened.sessions[space.record.ID].record.ExecutionRecord != nil {
		t.Fatal("space persisted execution state")
	}
}

func TestExecutionRoutesRequireCorrectParentAndKeepDiscussionScope(t *testing.T) {
	a, reader, space := multiExecutionFixture(t)
	child := createInSpace(t, a, reader, space, "worktree")
	server := httptest.NewServer(a.Handler(http.NotFoundHandler()))
	defer server.Close()
	for _, item := range []struct {
		path   string
		status int
	}{
		{"/api/collaborations/" + executionAddress(space.record.ID, child.record.ID), 200},
		{"/api/collaborations/" + executionAddress(uuid.NewString(), child.record.ID), 400},
		{"/api/collaborations/" + child.record.ID, 400},
		{"/api/collaborations/" + child.record.ID + "/context?kind=history", 400},
	} {
		res, err := server.Client().Get(server.URL + item.path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != item.status {
			t.Fatalf("%s: %d", item.path, res.StatusCode)
		}
	}
	private, err := child.annotate(Annotation{Text: "session discussion"}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if err := space.Share(context.Background(), "lan"); err != nil {
		t.Fatal(err)
	}
	b := joinSpace(t, space)
	if err := b.request(context.Background(), "GET", remoteExecutionPath(child.record.ID, "context?kind=annotations"), nil, nil); err != nil {
		t.Fatal("space member could not read session discussion", err)
	}
	var public struct {
		Annotations []Annotation `json:"annotations"`
	}
	if err := b.request(context.Background(), "GET", "/v2/context?kind=annotations", nil, &public); err != nil {
		t.Fatal(err)
	}
	for _, annotation := range public.Annotations {
		if annotation.ID == private.ID {
			t.Fatal("session discussion lost its explicit scope")
		}
	}
}
