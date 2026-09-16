package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/nativecodex"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/sharing"
)

func TestRuntimeModeBindsPreviewCreationRetryAndRestore(t *testing.T) {
	a, f, _ := fixture(t)
	source := f.source.ID
	ctx := context.Background()
	in := CreateInput{SourceID: source, WorkspaceMode: "existing", RequestID: uuid.NewString()}
	restricted, err := a.Preview(ctx, in)
	if err != nil || restricted.RuntimeMode != runtimeconfig.Restricted {
		t.Fatal(restricted, err)
	}
	in.RuntimeMode = runtimeconfig.Trusted
	trusted, err := a.Preview(ctx, in)
	if err != nil || trusted.Hash == restricted.Hash {
		t.Fatal("mode missing from confirmed preview", err)
	}
	in.PreviewHash = restricted.Hash
	if _, err = a.Create(ctx, in); err == nil {
		t.Fatal("trusted creation accepted restricted preview")
	}
	in.PreviewHash = trusted.Hash
	s, err := a.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if s.view()["runtimeMode"] != runtimeconfig.Trusted {
		t.Fatal(s.view())
	}
	f.mu.Lock()
	fork := f.requests["thread/fork"]
	f.mu.Unlock()
	if fork["permissions"] != nil || fork["approvalPolicy"] != nil || fork["deferGoalContinuation"] != true {
		t.Fatal("trusted fork did not inherit native policy", fork)
	}
	in.RuntimeMode = runtimeconfig.Restricted
	if _, err = a.Create(ctx, in); err == nil {
		t.Fatal("retry changed runtime mode")
	}
	in.RuntimeMode = "invalid"
	if _, err = a.Preview(ctx, in); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if err = s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	inv, err := sharing.Decode(s.share.Token())
	if err != nil || inv.RuntimeMode != runtimeconfig.Trusted {
		t.Fatal(inv.RuntimeMode, err)
	}
	sessionID := s.record.SessionID
	a.Close()
	reopened, err := Open(a.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := reopened.owned(s.record.ID)
	if err != nil || restored.record.RuntimeMode != runtimeconfig.Trusted {
		t.Fatal(err)
	}
	if err = restored.start(ctx, true); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	resume := f.requests["thread/resume"]
	f.mu.Unlock()
	if resume["threadId"] != sessionID || resume["permissions"] != nil || resume["approvalPolicy"] != nil {
		t.Fatal("restore lost native permissions or fork identity", resume)
	}
}

func TestTrustedRPCPreservesNativePermissionsAndWriterGate(t *testing.T) {
	a, f, _ := fixture(t)
	source := f.source.ID
	ctx := context.Background()
	in := CreateInput{SourceID: source, WorkspaceMode: "existing", RuntimeMode: runtimeconfig.Trusted, RequestID: uuid.NewString()}
	p, err := a.Preview(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = p.Hash
	s, err := a.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RPC(ctx, "remote", "thread/resume", map[string]any{"permissions": ":full-access"}, "foreign-settings"); err == nil {
		t.Fatal("non-writer changed trusted permissions")
	}
	if err = s.Action(ctx, "handoff"); err != nil {
		t.Fatal(err)
	}
	_, err = s.RPC(ctx, "remote", "thread/settings/update", map[string]any{"permissions": ":read-only", "approvalPolicy": "on-request", "approvalsReviewer": "user", "cwd": "/outside"}, "trusted-settings")
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	settings := f.requests["thread/settings/update"]
	f.mu.Unlock()
	if settings["permissions"] != ":read-only" || settings["cwd"] != s.record.ExecutionCwd || settings["threadId"] != s.record.SessionID {
		t.Fatal("trusted settings were overwritten or lost session binding", settings)
	}
	_, err = s.RPC(ctx, "remote", "permissionProfile/list", map[string]any{"cwd": "/outside"}, "")
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	profiles := f.requests["permissionProfile/list"]
	f.mu.Unlock()
	if profiles["cwd"] != s.record.ExecutionCwd {
		t.Fatal(profiles)
	}
	for _, method := range []string{"hooks/list", "skills/list"} {
		discovery, err := s.RPC(ctx, "remote", method, map[string]any{"cwds": []any{"/participant/local-cwd"}}, "")
		if err != nil || !bytes.Contains(discovery, []byte(`"cwd":"/participant/local-cwd"`)) {
			t.Fatal("native discovery lost the client's lookup key", method, string(discovery), err)
		}
		f.mu.Lock()
		cwds := f.requests[method]["cwds"].([]any)
		f.mu.Unlock()
		if len(cwds) != 1 || cwds[0] != s.record.ExecutionCwd {
			t.Fatal("native discovery did not resolve the host execution directory", method, cwds)
		}
	}
	config, err := s.RPC(ctx, "remote", "config/read", nil, "")
	if err != nil || bytes.Contains(config, []byte("secret")) {
		t.Fatal("private config exported", err)
	}
	if _, err = s.RPC(ctx, "remote", "thread/read", map[string]any{"threadId": "another-thread"}, ""); err == nil {
		t.Fatal("trusted gateway exposed another thread")
	}
	if err = s.Action(ctx, "reclaim"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RPC(ctx, "remote", "turn/start", nil, "stale-input"); err == nil {
		t.Fatal("reclaimed writer retained access")
	}
	if err = s.Action(ctx, "end"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RPC(ctx, "remote", "config/read", nil, ""); err == nil {
		t.Fatal("ended access survived")
	}
}

func TestTrustedClientDoesNotSendRestrictedDefaults(t *testing.T) {
	a, f, _ := fixture(t)
	source := f.source.ID
	ctx := context.Background()
	in := CreateInput{SourceID: source, WorkspaceMode: "existing", RuntimeMode: runtimeconfig.Trusted, RequestID: uuid.NewString()}
	p, _ := a.Preview(ctx, in)
	in.PreviewHash = p.Hash
	s, err := a.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.ClientPlan(ctx, s.record.ID, "tui", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(a.Config.DataDir, "clients", s.record.ID, "tui", "codex-home", "config.toml"))
	if err != nil || strings.Contains(string(b), nativecodex.Profile) {
		t.Fatal(string(b), err)
	}
	var record map[string]any
	b, _ = os.ReadFile(filepath.Join(a.Config.DataDir, "collaborations", s.record.ID, "collaboration.json"))
	_ = json.Unmarshal(b, &record)
	if record["runtimeMode"] != "trusted" {
		t.Fatal(record["runtimeMode"])
	}
}

func TestHookTrustStaysOnHostAndRequiresTrustedWriter(t *testing.T) {
	for _, mode := range []runtimeconfig.Mode{runtimeconfig.Restricted, runtimeconfig.Trusted} {
		t.Run(string(mode), func(t *testing.T) {
			a, f, _ := fixture(t)
			ctx := context.Background()
			in := CreateInput{SourceID: f.source.ID, WorkspaceMode: "existing", RuntimeMode: mode, RequestID: uuid.NewString()}
			preview, _ := a.Preview(ctx, in)
			in.PreviewHash = preview.Hash
			s, err := a.Create(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Share(ctx, "lan"); err != nil {
				t.Fatal(err)
			}
			params := func() map[string]any {
				return map[string]any{"edits": []any{map[string]any{"keyPath": "hooks.state", "mergeStrategy": "upsert", "value": map[string]any{"fixture": map[string]any{"trusted_hash": "sha256:fixture"}}}}, "reloadUserConfig": true, "filePath": "/outside/config.toml"}
			}
			if localClientRequest("config/batchWrite", params()) {
				t.Fatal("host hook trust was routed to the participant's local config")
			}
			if _, err = s.RPC(ctx, "remote", "config/batchWrite", params(), "non-writer"); err == nil {
				t.Fatal("non-writer approved hooks")
			}
			if err = s.Action(ctx, "handoff"); err != nil {
				t.Fatal(err)
			}
			_, err = s.RPC(ctx, "remote", "config/batchWrite", params(), "approve-hook")
			if mode == runtimeconfig.Restricted {
				if err == nil {
					t.Fatal("restricted runtime allowed hook trust writes")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			forwarded := f.requests["config/batchWrite"]
			f.mu.Unlock()
			if forwarded["filePath"] != nil || forwarded["reloadUserConfig"] != true || forwarded["edits"] == nil {
				t.Fatal("hook trust changed scope or lost native reload", forwarded)
			}
			mixed := params()
			mixed["edits"] = append(mixed["edits"].([]any), map[string]any{"keyPath": "mcp_servers", "value": "unexpected"})
			if _, err = s.RPC(ctx, "remote", "config/batchWrite", mixed, "mixed-config"); err == nil {
				t.Fatal("hook trust allowed other host config writes")
			}
			if _, err = s.RPC(ctx, "remote", "hooks/list", map[string]any{"cwds": []string{"/outside"}}, ""); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			b, _ := json.Marshal(f.requests["hooks/list"]["cwds"])
			f.mu.Unlock()
			if string(b) != "["+strconv.Quote(s.record.ExecutionCwd)+"]" {
				t.Fatal("hook discovery escaped collaboration directory", string(b))
			}
		})
	}
}
