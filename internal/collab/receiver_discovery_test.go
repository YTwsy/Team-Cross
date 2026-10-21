package collab

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/runtimeconfig"
)

func TestReceiverRechecksCurrentCLIAndExplicitlyRetriesLegacyPathFailure(t *testing.T) {
	ctx := context.Background()
	a, f, space := spaceFixture(t)
	app := filepath.Join(t.TempDir(), "ChatGPT.app")
	old := filepath.Join(app, "Contents/Resources/codex")
	cli := settingsCLI(t, filepath.Join(app, "Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"))
	t.Setenv("TEAMCROSS_CODEX_BIN", "")
	a.mu.Lock()
	a.settings.Binary, a.settings.DesktopApp = old, app
	a.settings.ClaudeBinary = filepath.Join(app, "missing-claude")
	a.mu.Unlock()
	id := uuid.NewString()
	home, err := a.providerHome("codex")
	if err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(a.Config.DataDir, "receivers", "receiver-"+id, "workspace")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	s := a.newReceiver(receiverRecord{SpaceID: space.record.ID, Record: Record{
		Schema: 3, ID: "receiver-" + id, Title: "空间协作助手", State: "error", Error: "Codex 路径不可用",
		ExecutionRecord: &ExecutionRecord{RequestID: id, RuntimeMode: runtimeconfig.Restricted, Provider: "codex", ProviderHome: home, ExecutionCwd: cwd, WorkspaceRoot: cwd, WorkspaceOwned: true, Commands: map[string]Command{}},
	}})
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if err := a.loadSpaceReceivers(); err != nil {
		t.Fatal(err)
	}
	s = a.receivers[s.record.ID]
	file := filepath.Join(s.runtimeDirectory(), "receiver.json")
	before, _ := os.ReadFile(file)
	view := a.receiverView(s)
	if view["binary"] != cli || view["binary"] != a.Info(ctx)["binary"] || view["clientError"] != "" || view["error"] != "" || view["clientRecovered"] != true || view["canRetryCreation"] != true {
		t.Fatal(view)
	}
	after, _ := os.ReadFile(file)
	if string(before) != string(after) || len(f.calls) != 0 {
		t.Fatal("reading recovery status modified the record or opened a native session")
	}
	// A later settings change must be reflected by the same card immediately.
	a.mu.Lock()
	a.settings.Binary = filepath.Join(app, "missing-custom-cli")
	a.mu.Unlock()
	view = a.receiverView(s)
	if view["binary"] != "" || view["clientError"] != "Codex CLI 文件不存在" || view["clientRecovered"] != false {
		t.Fatal(view)
	}
	if _, err := a.retrySpaceReceiver(ctx, s); err == nil || len(f.calls) != 0 {
		t.Fatal("retry ignored the current invalid path", err)
	}
	w := httptest.NewRecorder()
	a.Handler(nil).ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1/api/settings", strings.NewReader(fmt.Sprintf(`{"binary":%q,"desktopApp":%q}`, old, app))))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var launched string
	start := a.Config.StartProcess
	a.Config.StartProcess = func(binary, home, cwd, log string) (Runtime, error) {
		launched = binary
		return start(binary, home, cwd, log)
	}
	w = httptest.NewRecorder()
	a.Handler(nil).ServeHTTP(w, httptest.NewRequest("POST", "http://127.0.0.1/api/space-receivers/"+s.record.ID+"/action", strings.NewReader(`{"action":"retry"}`)))
	if w.Code != 200 || launched != cli || s.record.State != "ready" || s.record.Error != "" || s.record.SessionID == "" || s.receiverCreation != "thread" {
		t.Fatal(w.Code, w.Body.String(), launched, s.record)
	}
	// A lost response or double click cannot create a second native session.
	if _, err := a.createSpaceReceiver(ctx, space.record.ID, id, s.record.Title); err != nil {
		t.Fatal(err)
	}
	if _, err := a.retrySpaceReceiver(ctx, s); err == nil {
		t.Fatal("a completed creation was retried")
	}
	if len(a.receivers) != 1 || len(a.agents.Pairings) != 1 || slices.Contains(f.calls, "turn/start") {
		t.Fatal(a.receivers, a.agents.Pairings, f.calls)
	}
	count := 0
	for _, method := range f.calls {
		if method == "thread/start" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate native session", f.calls)
	}
	// The recovered card must also support its first explicit delivery. This
	// crosses the persisted-empty-ledger boundary omitted by creation-only tests.
	request, err := a.sendAgentRequest(ctx, agentRequestInput{
		RequestID: uuid.NewString(), PairingID: id, Intent: "analyze",
		Instruction: "Read the synthetic brief", WorkbenchSpaceID: space.record.ID,
	})
	if err != nil || request.State != "submitted" || request.TurnID == "" {
		t.Fatal("first delivery after recovery failed", request, err)
	}
	if status, err := a.ControlStatus(ctx); err != nil || status.Active != 1 {
		t.Fatal("recovered delivery blocked Core status", status, err)
	}
}

type receiverLostStart struct{ Runtime }

func (r receiverLostStart) Call(ctx context.Context, method string, in, out any) error {
	if err := r.Runtime.Call(ctx, method, in, out); err != nil {
		return err
	}
	if method == "thread/start" {
		return fmt.Errorf("lost thread/start response")
	}
	return nil
}

func TestReceiverRetryRequiresKnownPreThreadFailure(t *testing.T) {
	ctx := context.Background()
	a, f, space := spaceFixture(t)
	start := a.Config.StartProcess
	a.Config.StartProcess = func(string, string, string, string) (Runtime, error) {
		return nil, fmt.Errorf("client startup failed")
	}
	id := uuid.NewString()
	if _, err := a.createSpaceReceiver(ctx, space.record.ID, id, "助手"); err == nil {
		t.Fatal("expected client failure")
	}
	s := a.receivers["receiver-"+id]
	if a.receiverView(s)["canRetryCreation"] != true || s.receiverCreation != "client" || len(f.calls) != 0 {
		t.Fatal(a.receiverView(s), f.calls)
	}
	if err := a.loadSpaceReceivers(); err != nil {
		t.Fatal(err)
	}
	s = a.receivers[s.record.ID]
	a.Config.StartProcess = func(binary, home, cwd, log string) (Runtime, error) {
		r, err := start(binary, home, cwd, log)
		return receiverLostStart{r}, err
	}
	if _, err := a.retrySpaceReceiver(ctx, s); err == nil {
		t.Fatal("expected lost creation response")
	}
	if s.receiverCreation != "thread" || a.receiverView(s)["canRetryCreation"] != false {
		t.Fatal("unknown outcome allowed retry", a.receiverView(s))
	}
	if err := a.loadSpaceReceivers(); err != nil {
		t.Fatal(err)
	}
	s = a.receivers[s.record.ID]
	if _, err := a.retrySpaceReceiver(ctx, s); err == nil || a.receiverView(s)["canRetryCreation"] != false {
		t.Fatal("restart made an uncertain creation retryable", err)
	}
}

func TestMissingCLIDoesNotAllocateFailedReceiver(t *testing.T) {
	a, f, space := spaceFixture(t)
	a.mu.Lock()
	a.settings.Binary = filepath.Join(t.TempDir(), "missing-custom-cli")
	a.mu.Unlock()
	for range 2 {
		if _, err := a.createSpaceReceiver(context.Background(), space.record.ID, uuid.NewString(), "助手"); err == nil {
			t.Fatal("missing CLI accepted")
		}
	}
	if len(a.receivers) != 0 || len(f.calls) != 0 {
		t.Fatal("missing path created another failed card", a.receivers, f.calls)
	}
}
