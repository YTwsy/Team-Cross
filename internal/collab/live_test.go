package collab

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"teamcross/internal/nativecodex"
	"teamcross/internal/workspace"
	"testing"
	"time"
)

// Opt in only. Uses a dedicated native history home and repository. The sole
// credential reference is a local symlink; no credential is printed or copied.
const liveModel = "gpt-5.6-luna"

func TestLiveCodex(t *testing.T) {
	root := os.Getenv("TEAMCROSS_LIVE_DIR")
	if root == "" {
		t.Skip("set TEAMCROSS_LIVE_DIR to an empty dedicated test directory")
	}
	if _, e := os.Stat(filepath.Join(root, "fixture.json")); e == nil {
		t.Fatal("fixture already exists; choose a new test directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	home := filepath.Join(root, "codex-home")
	repo := filepath.Join(root, "repo")
	os.MkdirAll(home, 0700)
	os.MkdirAll(filepath.Join(repo, "src"), 0700)
	userHome, _ := os.UserHomeDir()
	if e := os.Symlink(filepath.Join(userHome, ".codex", "auth.json"), filepath.Join(home, "auth.json")); e != nil && !os.IsExist(e) {
		t.Fatal(e)
	}
	t.Setenv("CODEX_HOME", home)
	if e := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \""+liveModel+"\"\nmodel_reasoning_effort = \"low\"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Team Cross Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		if _, e := workspace.Git(ctx, repo, args...); e != nil {
			t.Fatal(e)
		}
	}
	os.WriteFile(filepath.Join(repo, "src", "baseline.txt"), []byte("committed\n"), 0600)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.txt\n"), 0600)
	workspace.Git(ctx, repo, "add", "src/baseline.txt", ".gitignore")
	if _, e := workspace.Git(ctx, repo, "commit", "-m", "Dedicated collaboration fixture"); e != nil {
		t.Fatal(e)
	}
	// The shared runtime launches the actual STDIO entry, not this Go test binary.
	executable := filepath.Join(root, "teamcross")
	build := exec.CommandContext(ctx, "go", "build", "-o", executable, "./cmd/teamcross")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build runtime MCP entry: %v %s", err, out)
	}
	a, e := Open(Config{DataDir: filepath.Join(root, "data"), Repo: repo, Loopback: true, Executable: executable})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	params := nativecodex.Overrides("", filepath.Join(repo, "src"))
	delete(params, "threadId")
	if e = a.readerCall(ctx, "thread/start", params, &started); e != nil {
		t.Fatal(e)
	}
	source := started.Thread.ID
	if e = a.readerCall(ctx, "thread/name/set", map[string]any{"threadId": source, "name": "Team Cross · 客户端协作验证"}, nil); e != nil {
		t.Fatal(e)
	}
	sourceDone := make(chan string, 1)
	a.reader.SetHandler(func(m nativecodex.Message) {
		if m.Method == "turn/completed" {
			var p struct {
				Turn struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			_ = json.Unmarshal(m.Params, &p)
			select {
			case sourceDone <- p.Turn.Status:
			default:
			}
		}
	})
	if e = a.readerCall(ctx, "turn/start", map[string]any{"threadId": source, "model": liveModel, "effort": "medium", "input": []any{map[string]any{"type": "text", "text": "This is a dedicated Team Cross integration fixture. Reply exactly TEAMCROSS_SOURCE_READY. Do not use tools."}}}, nil); e != nil {
		t.Fatal(e)
	}
	select {
	case state := <-sourceDone:
		if state != "completed" {
			t.Fatal("source turn", state)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	t.Log("source completed with", liveModel, source)
	os.WriteFile(filepath.Join(repo, "src", "baseline.txt"), []byte("staged\n"), 0600)
	workspace.Git(ctx, repo, "add", "src/baseline.txt")
	os.WriteFile(filepath.Join(repo, "src", "baseline.txt"), []byte("unstaged\n"), 0600)
	os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked"), 0600)
	os.WriteFile(filepath.Join(repo, "ignored.txt"), []byte("ignored"), 0600)
	ids := map[string]string{}
	for _, mode := range []string{"existing", "worktree"} {
		in := CreateInput{SourceID: source, WorkspaceMode: mode, RequestID: uuid.NewString(), Title: map[string]string{"existing": "设计协作工具 · 当前现场", "worktree": "探索独立分支 · 新 worktree"}[mode]}
		p, e := a.Preview(ctx, in)
		if e != nil {
			t.Fatal(e)
		}
		in.PreviewHash = p.Hash
		s, e := a.Create(ctx, in)
		if e != nil {
			t.Fatal(e)
		}
		ids[mode] = s.record.ID
		t.Log("created", mode, s.record.SessionID, s.record.ExecutionCwd)
		if s.record.Model != liveModel || s.record.ReasoningEffort == nil || *s.record.ReasoningEffort != "medium" {
			t.Fatalf("source model settings not inherited: %s %v", s.record.Model, s.record.ReasoningEffort)
		}
		data, _ := os.ReadFile(filepath.Join(s.record.ExecutionCwd, "baseline.txt"))
		expected := "unstaged\n"
		if mode == "worktree" {
			expected = "committed\n"
		}
		if string(data) != expected {
			t.Fatal("wrong workspace", string(data))
		}
		text := "Use your file or command tools to create a file named " + mode + "-proof.txt in the current working directory, with exactly TEAMCROSS_" + strings.ToUpper(mode) + "_OK as its contents. Do not change any other file. Then reply with the absolute file path. This is an authorized integration test in a dedicated fixture."
		_, e = s.RPC(ctx, "owner", "turn/start", map[string]any{"model": liveModel, "effort": "low", "input": []any{map[string]any{"type": "text", "text": text}}}, "live-file-"+mode)
		if e != nil {
			t.Fatal(e)
		}
		for {
			s.mu.Lock()
			busy := s.busy
			pending := len(s.approvals)
			s.mu.Unlock()
			if pending > 0 {
				events, _ := json.Marshal(s.Events(0))
				t.Fatalf("unexpected approval: %s", events)
			}
			if !busy {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(time.Second):
			}
		}
		proof, e := os.ReadFile(filepath.Join(s.record.ExecutionCwd, mode+"-proof.txt"))
		if e != nil || strings.TrimSpace(string(proof)) != "TEAMCROSS_"+strings.ToUpper(mode)+"_OK" {
			t.Fatalf("execution proof missing: %q %v", proof, e)
		}
		if s.record.Model != liveModel || s.record.ReasoningEffort == nil || *s.record.ReasoningEffort != "low" {
			t.Fatal("client model settings not observed")
		}
		t.Log("file execution confirmed", mode)
		verifyLiveRelease(t, ctx, s)
	}
	result := map[string]any{"root": root, "repo": repo, "dataDir": a.Config.DataDir, "codexHome": home, "sourceId": source, "collaborations": ids, "model": liveModel, "createdAt": time.Now()}
	if e = writeJSONFile(filepath.Join(root, "fixture.json"), result); e != nil {
		t.Fatal(e)
	}
}
