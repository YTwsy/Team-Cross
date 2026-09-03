package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"teamcross/internal/bridgeclient"
	"teamcross/internal/domain"
	"teamcross/internal/storage"
	"teamcross/internal/transfer"
)

// Two independent local data directories plus the compiled Mock Adapter prove
// source-independent Git/Core behavior, not two Macs, transport, or a native
// Provider. No personal Session, credentials, or real Provider is accessed.
func TestOfflineReceiverRestartsAndMockContinuesAfterSenderUnavailable(t *testing.T) {
	options, err := bridgeclient.DefaultCommand()
	if err != nil {
		t.Skipf("built local Bridge unavailable: %v", err)
	}
	ctx := context.Background()
	repo := serverTestRepository(t)
	writeCapturedFixture(t, filepath.Join(repo, "tracked-binary.bin"), []byte{0, 1, 2, 3})
	serverTestGit(t, repo, "add", "tracked-binary.bin")
	serverTestGit(t, repo, "commit", "-m", "binary baseline")
	writeCapturedFixture(t, filepath.Join(repo, "tracked.txt"), []byte("staged\n"))
	serverTestGit(t, repo, "add", "tracked.txt")
	writeCapturedFixture(t, filepath.Join(repo, "tracked.txt"), []byte("staged\nunstaged\n"))
	writeCapturedFixture(t, filepath.Join(repo, "tracked-binary.bin"), []byte{0, 255, 2, 0, 42})
	captured := []byte{0, 254, 42, 0, 255, 7}
	writeCapturedFixture(t, filepath.Join(repo, "selected.bin"), captured)
	writeCapturedFixture(t, filepath.Join(repo, "not-selected.txt"), []byte("PRIVATE SOURCE ONLY\n"))
	if err := os.Symlink("tracked.txt", filepath.Join(repo, "selected-link")); err != nil {
		t.Fatal(err)
	}
	sourceBefore := serverTestGit(t, repo, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	sender := newIntegrationApp(t, repo)
	source := captureSelectedFixture(t, sender, []string{"selected.bin", "selected-link"})
	evidenceBody := []byte("explicit offline evidence\n")
	evidenceObject, err := sender.store.PutObject(ctx, evidenceBody, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := sender.store.CreateEvidence(ctx, domain.Evidence{ThreadID: source.ID, Kind: "note", Title: "Offline note", ObjectHash: evidenceObject.Hash})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := sender.store.CreateSessionSnapshot(ctx, domain.SessionSnapshot{
		ThreadID: source.ID, CapturedAt: time.Now().UTC(),
		Source:       domain.SessionRef{Provider: "mock", SessionID: "synthetic-offline-session", Surface: "cli", IdentityKind: "session.id", Cwd: repo},
		Entries:      []domain.SessionEntry{{ID: "context", Kind: "message", Role: "assistant", Text: "Explicitly selected synthetic context"}},
		Capabilities: domain.SessionCapabilities{Read: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	exported := requestJSON(t, sender.Handler(), http.MethodPost, "/api/v1/threads/"+source.ID+"/bundles", map[string]any{
		"roundId": source.Rounds[0].ID, "confirmExport": true,
		"evidenceIds": []string{evidence.ID}, "snapshotIds": []string{snapshot.ID},
	}, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("sender export=%d %s", exported.Code, exported.Body.String())
	}
	var bundle transfer.Bundle
	decodeResponse(t, exported, &bundle)
	receiver := newIntegrationApp(t, filepath.Join(t.TempDir(), "unused-project"))
	if receiver.paths.Root == sender.paths.Root {
		t.Fatal("receiver reused sender data directory")
	}
	imported := requestJSON(t, receiver.Handler(), http.MethodPost, "/api/v1/bundles/import", bundle, nil)
	if imported.Code != http.StatusCreated {
		t.Fatalf("receiver import=%d %s", imported.Code, imported.Body.String())
	}
	var received threadDetail
	decodeResponse(t, imported, &received)
	if received.ID == source.ID || received.AgentRun != nil || len(received.Rounds) != 1 || received.Rounds[0].ID == source.Rounds[0].ID || received.Git.Head != source.Git.Head {
		t.Fatalf("import must create a new inert baseline-preserving Thread: %#v", received)
	}
	if runs, err := receiver.store.ListAgentRuns(ctx, received.ID); err != nil || len(runs) != 0 {
		t.Fatalf("import executed Agent: %v %v", runs, err)
	}
	if !bytes.Equal(sourceBefore, serverTestGit(t, repo, "status", "--porcelain=v2", "-z", "--untracked-files=all")) {
		t.Fatal("capture/export/import altered source checkout")
	}
	// Only dedicated test fixtures are moved. Source repo, CAS, SQLite, context,
	// and worktree are now unavailable through every original sender path.
	if err := sender.store.Close(); err != nil {
		t.Fatal(err)
	}
	unavailable := t.TempDir()
	retiredRepo := filepath.Join(unavailable, "sender-repository")
	for _, move := range [][2]string{{repo, retiredRepo}, {sender.paths.Root, filepath.Join(unavailable, "sender-data")}} {
		if err := os.Rename(move[0], move[1]); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(move[0]); !os.IsNotExist(err) {
			t.Fatalf("sender path remains available: %s", move[0])
		}
	}
	if err := receiver.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(ctx, receiver.paths.Root)
	if err != nil {
		t.Fatal(err)
	}
	receiver.store = reopened
	t.Cleanup(func() { _ = reopened.Close() })
	thread, err := reopened.GetThread(ctx, received.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(thread.RepoRoot, receiver.paths.Worktrees+string(os.PathSeparator)) || thread.BaselineCommit != bundle.Baseline {
		t.Fatalf("receiver is not self-contained: %#v", thread)
	}
	if remotes := serverTestGit(t, thread.RepoRoot, "remote", "-v"); len(remotes) != 0 {
		t.Fatalf("imported repository retained remote dependency: %s", remotes)
	}
	if _, err := os.Lstat(filepath.Join(thread.RepoRoot, "objects", "info", "alternates")); !os.IsNotExist(err) {
		t.Fatal("imported repository retained object alternates")
	}
	round, err := reopened.GetRound(ctx, received.Rounds[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	manifestData, err := reopened.GetObject(ctx, round.ManifestObject)
	if err != nil {
		t.Fatal(err)
	}
	var manifest sealedRoundContext
	if err := json.Unmarshal(manifestData, &manifest); err != nil || manifest.Origin == nil || *manifest.Origin != bundle.Origin {
		t.Fatalf("source provenance not durable: %#v %v", manifest, err)
	}
	evidenceList, err := reopened.ListEvidence(ctx, thread.ID)
	if err != nil || len(evidenceList) != 1 {
		t.Fatalf("received evidence closure lost: %v %v", evidenceList, err)
	}
	data, err := reopened.GetObject(ctx, evidenceList[0].ObjectHash)
	if err != nil || !bytes.Equal(data, evidenceBody) {
		t.Fatalf("received evidence depends on sender: %q %v", data, err)
	}
	sessions, err := reopened.ListSessionSnapshots(ctx, thread.ID)
	if err != nil || len(sessions) != 1 || sessions[0].Source.Cwd != "" || sessions[0].Entries[0].Text != snapshot.Entries[0].Text {
		t.Fatalf("received Session closure lost or carried cwd: %v %v", sessions, err)
	}
	bridge, err := bridgeclient.Start(ctx, options)
	if err != nil {
		t.Fatalf("compiled local Mock Bridge unavailable: %v", err)
	}
	receiver.bridge = bridge
	consumeDone := make(chan struct{})
	go func() {
		defer close(consumeDone)
		receiver.consumeBridgeEvents(bridge)
	}()
	t.Cleanup(func() {
		_ = bridge.Close()
		select {
		case <-consumeDone:
		case <-time.After(3 * time.Second):
			t.Error("Mock Bridge event consumer did not stop")
		}
	})
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	_, err = bridge.Probe(probeCtx)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	continued := requestJSON(t, receiver.Handler(), http.MethodPost, "/api/v1/threads/"+thread.ID+"/continue", map[string]any{
		"roundId": round.ID, "provider": "mock", "prompt": "write receiver offline result", "expectedRevision": thread.Revision,
	}, nil)
	if continued.Code != http.StatusCreated {
		t.Fatalf("offline Mock Continue=%d %s", continued.Code, continued.Body.String())
	}
	var sealed domain.Round
	deadline := time.Now().Add(5 * time.Second)
	for {
		rounds, err := reopened.ListRounds(ctx, thread.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rounds) == 2 && rounds[1].Kind == "agent_turn" {
			sealed = rounds[1]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("offline Mock Turn did not seal: %#v", rounds)
		}
		time.Sleep(20 * time.Millisecond)
	}
	patchResponse := requestJSON(t, receiver.Handler(), http.MethodGet, "/api/v1/threads/"+thread.ID+"/patch", nil, nil)
	if patchResponse.Code != http.StatusOK {
		t.Fatalf("offline patch export=%d %s", patchResponse.Code, patchResponse.Body.String())
	}
	patch := bytes.Clone(patchResponse.Body.Bytes())
	if !bytes.Contains(patch, []byte(".teamcross-mock-output.txt")) || !bytes.Contains(patch, []byte("selected.bin")) || bytes.Contains(patch, []byte("not-selected.txt")) {
		t.Fatal("offline patch omitted captured/Mock results or widened source selection")
	}
	fresh := filepath.Join(t.TempDir(), "fresh-baseline")
	serverTestGit(t, thread.RepoRoot, "worktree", "add", "--detach", fresh, bundle.Baseline)
	for _, args := range [][]string{{"apply", "--binary", "--check", "-"}, {"apply", "--binary", "-"}} {
		cmd := exec.Command("git", append([]string{"-C", fresh}, args...)...)
		cmd.Stdin = bytes.NewReader(patch)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fresh receiver baseline apply: %v %s", err, output)
		}
	}
	wantTree, err := transfer.TreeDigest(thread.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	gotTree, err := transfer.TreeDigest(fresh)
	if err != nil || gotTree != wantTree {
		t.Fatalf("applied offline result does not match receiver code tree: %s != %s (%v)", gotTree, wantTree, err)
	}
	for path, want := range map[string][]byte{"selected.bin": captured, "tracked.txt": []byte("staged\nunstaged\n")} {
		data, err := os.ReadFile(filepath.Join(fresh, path))
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("applied offline file %s=%q %v", path, data, err)
		}
	}
	finalExport := requestJSON(t, receiver.Handler(), http.MethodPost, "/api/v1/threads/"+thread.ID+"/bundles", map[string]any{
		"roundId": sealed.ID, "confirmExport": true,
		"evidenceIds": []string{evidenceList[0].ID}, "snapshotIds": []string{sessions[0].ID},
	}, nil)
	if finalExport.Code != http.StatusOK {
		t.Fatalf("offline receiver cannot re-export new sealed result: %d %s", finalExport.Code, finalExport.Body.String())
	}
	finalBundle, err := transfer.Decode(bytes.NewReader(finalExport.Body.Bytes()))
	if err != nil || finalBundle.Baseline != bundle.Baseline || finalBundle.Origin.ThreadID != thread.ID || finalBundle.Origin.RoundID != sealed.ID || len(finalBundle.Evidence) != 1 || len(finalBundle.SessionSnapshots) != 1 {
		t.Fatalf("offline result bundle closure invalid: %v", err)
	}
	if !bytes.Equal(sourceBefore, serverTestGit(t, retiredRepo, "status", "--porcelain=v2", "-z", "--untracked-files=all")) {
		t.Fatal("receiver execution or export modified retired source checkout")
	}
	if _, err := os.Lstat(filepath.Join(retiredRepo, ".teamcross-mock-output.txt")); !os.IsNotExist(err) {
		t.Fatal("Mock output escaped to sender checkout")
	}
}
