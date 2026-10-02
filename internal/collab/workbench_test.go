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
	"teamcross/internal/mcp"
)

func stopWorkbenchPump(a *App) { a.workbenchCancel(); a.workbenchWG.Wait() }

func spaceFixture(t *testing.T) (*App, *fakeRuntime, *Session) {
	t.Helper()
	a, f, _ := fixture(t)
	stopWorkbenchPump(a)
	s, err := a.CreateSpace(context.Background(), SpaceInput{RequestID: uuid.NewString(), Title: "协作研究"})
	if err != nil {
		t.Fatal(err)
	}
	return a, f, s
}

func spaceChannel(t *testing.T, a *App, spaceID string) (AgentPairing, agentCaller, SpaceTarget) {
	t.Helper()
	caller := agentCaller{Caller: mcp.CallerSource{Provider: "claude", SourceID: uuid.NewString(), ToolUseID: "verify"}, ReceiverID: uuid.NewString()}
	if a.Config.ClaudeHome == "" {
		a.Config.ClaudeHome = t.TempDir()
	}
	writeAgentClaudeTranscript(t, a, caller)
	p := AgentPairing{ID: uuid.NewString(), Name: "成员的分析会话", Provider: "claude", SessionID: caller.Caller.SourceID, ReceiverID: caller.ReceiverID, Transport: "claude_channel", State: "paired", CreatedAt: time.Now()}
	a.agentMu.Lock()
	next := libraryClone(a.agents)
	next.Pairings[p.ID] = p
	err := a.saveAgentsLocked(next)
	a.receiverLocked(p.ReceiverID)
	a.agentMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	out, err := a.registerSpacePairing(context.Background(), spaceID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.pumpWorkbench(context.Background())
	return p, caller, workbenchDecode[SpaceTarget](out)
}

func wb(t *testing.T, a *App, spaceID, op string, in workbenchInput) any {
	t.Helper()
	out, err := a.workbenchCall(context.Background(), spaceID, op, in)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return out
}

func TestSpaceWorkbenchThreeCoresPreserveIdentityScopeAndResults(t *testing.T) {
	ctx := context.Background()
	a, f, s := spaceFixture(t)
	d := materialDraft(t, a, f)
	pub, err := a.Publish(ctx, s.record.ID, PublishInput{PreviewID: d.ID, PreviewHash: d.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	mid := pub.(map[string]any)["materialId"].(string)
	if err = s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	b, _, _ := fixture(t)
	c, fc, _ := fixture(t)
	stopWorkbenchPump(b)
	stopWorkbenchPump(c)
	jb, err := b.Join(ctx, s.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	jc, err := c.Join(ctx, s.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	p, caller, target := spaceChannel(t, b, jb.ID)
	_, _, thirdTarget := spaceChannel(t, c, jc.ID)
	if target.MemberID == "owner" || target.MemberID == thirdTarget.MemberID {
		t.Fatal("member identities collapsed", target, thirdTarget)
	}
	ref := LibraryReference{SpaceID: jc.ID, Kind: "material", MaterialID: mid, Version: 1}
	args := map[string]any{"spaceId": jc.ID, "requestId": uuid.NewString(), "targetId": target.ID, "references": []LibraryReference{ref}, "instruction": "核对材料范围，给出结论", "intent": "analyze"}
	source := agentCaller{Caller: mcp.CallerSource{Provider: "codex", SourceID: fc.source.ID, TurnID: "turn-fixture"}}
	out, err := c.invokeWorkbenchTool(ctx, "send_space_request", args, source, "")
	if err != nil {
		t.Fatal(err)
	}
	request := workbenchDecode[SpaceRequest](out)
	if request.State != "queued" || request.Actor.MemberID != thirdTarget.MemberID || request.Actor.Kind != "session" || request.References[0].SpaceID != jc.ID {
		t.Fatal(request)
	}
	// Duplicate creation never restarts delivery, even across member aliases.
	if _, err = c.invokeWorkbenchTool(ctx, "send_space_request", args, source, ""); err != nil {
		t.Fatal(err)
	}
	b.pumpWorkbench(ctx)
	b.agentMu.Lock()
	events := len(b.agentReceivers[p.ReceiverID].events)
	local := b.agents.Requests[request.ID]
	b.agentMu.Unlock()
	if events != 1 || local.References[0].SpaceID != jb.ID {
		t.Fatal(events, local)
	}
	if _, err = c.workbenchCall(ctx, jc.ID, "finish", workbenchInput{RequestID: request.ID, TargetID: target.ID, State: "completed", Summary: "forged"}); err == nil {
		t.Fatal("another member reported a result")
	}
	read, err := b.readAgentRequest(ctx, caller, request.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if workbenchDecode[map[string]any](read)["spaceId"] != jb.ID {
		t.Fatal(read)
	}
	done, err := b.finishAgentRequest(ctx, caller, request.ID, "", "completed", "已核对固定版本，未读取未公开历史。")
	if err != nil {
		t.Fatal(err)
	}
	if done.State != "completed" {
		t.Fatal(done)
	}
	for _, pair := range []struct {
		a  *App
		id string
	}{{a, s.record.ID}, {b, jb.ID}, {c, jc.ID}} {
		view := workbenchDecode[WorkbenchView](wb(t, pair.a, pair.id, "view", workbenchInput{}))
		if len(view.Requests) != 1 || view.Requests[0].Summary != done.Summary || view.Requests[0].ReceivedAt == nil {
			t.Fatal(view)
		}
		data, _ := json.Marshal(view)
		for _, secret := range []string{p.SessionID, p.ID, p.ReceiverID, "前置未公开", "后续未公开"} {
			if strings.Contains(string(data), secret) {
				t.Fatal("private data leaked", secret)
			}
		}
	}
	// Follow-ups explicitly retain their parent; peer requests do not require an assistant.
	follow := workbenchInput{RequestID: uuid.NewString(), TargetID: thirdTarget.ID, ParentRequestID: request.ID, Instruction: "请复核前一条建议", Intent: "analyze"}
	wb(t, a, s.record.ID, "send", follow)
	if s.record.ExecutionRecord != nil || s.record.Workbench.Assistant.State != "disabled" {
		t.Fatal("peer work enabled execution/assistant")
	}
	// Membership is rechecked on every subsequent receipt and read.
	if err = s.RevokeMember(target.MemberID); err != nil {
		t.Fatal(err)
	}
	if _, err = b.readAgentRequest(ctx, caller, request.ID, "", 0); err == nil {
		t.Fatal("revoked member read request")
	}
	view := workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	for _, v := range view.Targets {
		if v.ID == target.ID && v.Available {
			t.Fatal("revoked receiver still available")
		}
	}
}

func TestSpaceBriefCASAndAssistantLifecycle(t *testing.T) {
	ctx := context.Background()
	a, _, s := spaceFixture(t)
	_, caller, target := spaceChannel(t, a, s.record.ID)
	brief := SpaceBrief{Topic: "明确接口改造范围", Decisions: []BriefItem{{Text: "先验证兼容行为", Sources: []LibraryReference{}}}, Questions: []BriefItem{{Text: "还有哪些测试缺口？", Sources: []LibraryReference{}}}}
	wb(t, a, s.record.ID, "brief", workbenchInput{Brief: brief})
	if _, err := a.workbenchCall(ctx, s.record.ID, "brief", workbenchInput{Brief: brief}); err == nil {
		t.Fatal("lost brief update")
	}
	if _, err := a.workbenchCall(ctx, s.record.ID, "brief", workbenchInput{BaseRevision: 1, Brief: brief, Actor: SpaceActor{Kind: "session"}}); err == nil {
		t.Fatal("agent confirmed human decisions")
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.workbenchCall(ctx, s.record.ID, "assistant", workbenchInput{State: "initializing", TargetID: target.ID, RequestID: uuid.NewString()})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("multiple assistant bindings", success)
	}
	view := workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	first := view.Assistant
	if first.State != "initializing" {
		t.Fatal(first)
	}
	a.pumpWorkbench(ctx)
	if _, err := a.finishAgentRequest(ctx, caller, first.BootstrapID, "", "completed", "accepted"); err == nil {
		t.Fatal("ready without reading")
	}
	read, err := a.readAgentRequest(ctx, caller, first.BootstrapID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	request := workbenchDecode[struct {
		Request SpaceRequest `json:"request"`
	}](read).Request
	if request.Bootstrap == nil || request.Bootstrap.Brief.Revision != 1 || request.Bootstrap.Title != s.record.Title {
		t.Fatal(request)
	}
	// Pause wins over the late completion; existing work remains attributed.
	wb(t, a, s.record.ID, "assistant", workbenchInput{State: "paused", BaseEpoch: first.Epoch})
	if _, err = a.finishAgentRequest(ctx, caller, first.BootstrapID, "", "completed", "accepted"); err != nil {
		t.Fatal(err)
	}
	view = workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	if view.Assistant.State != "paused" {
		t.Fatal(view.Assistant)
	}
	second := workbenchDecode[SpaceAssistant](wb(t, a, s.record.ID, "assistant", workbenchInput{State: "initializing", BaseEpoch: view.Assistant.Epoch, TargetID: target.ID, RequestID: uuid.NewString()}))
	a.pumpWorkbench(ctx)
	if _, err = a.readAgentRequest(ctx, caller, second.BootstrapID, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = a.finishAgentRequest(ctx, caller, second.BootstrapID, "", "completed", "已读取并接手"); err != nil {
		t.Fatal(err)
	}
	view = workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	if view.Assistant.State != "ready" {
		t.Fatal(view.Assistant)
	}
	wb(t, a, s.record.ID, "assistant", workbenchInput{State: "disabled", BaseEpoch: second.Epoch})
	view = workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	if view.Assistant.TargetID != "" || len(view.Requests) != 2 || len(a.agentPairings()) != 1 {
		t.Fatal(view)
	}
}

func TestWorkbenchClaimNeverReplaysAndSourcesAreChecked(t *testing.T) {
	ctx := context.Background()
	a, _, s := spaceFixture(t)
	_, _, target := spaceChannel(t, a, s.record.ID)
	in := workbenchInput{RequestID: uuid.NewString(), TargetID: target.ID, Instruction: "inspect", Intent: "analyze", References: []LibraryReference{{SpaceID: s.record.ID, Kind: "context"}}}
	if _, err := a.workbenchCall(ctx, s.record.ID, "send", in); err == nil {
		t.Fatal("published workbench imported private context")
	}
	in.References = nil
	wb(t, a, s.record.ID, "send", in)
	wb(t, a, s.record.ID, "claim", workbenchInput{RequestID: in.RequestID, TargetID: target.ID})
	if _, err := a.workbenchCall(ctx, s.record.ID, "claim", workbenchInput{RequestID: in.RequestID, TargetID: target.ID}); err == nil {
		t.Fatal("claim replayed")
	}
	a.pumpWorkbench(ctx)
	a.agentMu.Lock()
	_, delivered := a.agents.Requests[in.RequestID]
	a.agentMu.Unlock()
	if delivered {
		t.Fatal("uncertain claim caused native input")
	}
	dataDir := a.Config.DataDir
	a.Close()
	reopened, err := Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got := workbenchDecode[SpaceRequest](wb(t, reopened, s.record.ID, "request", workbenchInput{RequestID: in.RequestID}))
	if got.State != "unknown" {
		t.Fatal(got)
	}
}

func TestWorkbenchPauseStopsNewSessionRequestsAndExpiredQueueIsVisible(t *testing.T) {
	ctx := context.Background()
	a, _, s := spaceFixture(t)
	_, _, coordinator := spaceChannel(t, a, s.record.ID)
	_, _, peer := spaceChannel(t, a, s.record.ID)
	in := workbenchInput{RequestID: uuid.NewString(), TargetID: peer.ID, Instruction: "inspect", Intent: "analyze", Actor: SpaceActor{Kind: "session", Provider: coordinator.Provider, Session: coordinator.Session}}
	wb(t, a, s.record.ID, "send", in)
	s.mu.Lock()
	s.record.Workbench.Assistant = SpaceAssistant{TargetID: coordinator.ID, State: "paused", Epoch: 1}
	s.mu.Unlock()
	// Retrying an existing ID only reads its receipt, even after a pause.
	wb(t, a, s.record.ID, "send", in)
	oldID := in.RequestID
	in.RequestID = uuid.NewString()
	if _, err := a.workbenchCall(ctx, s.record.ID, "send", in); err == nil {
		t.Fatal("paused coordinator dispatched new work")
	}
	s.mu.Lock()
	r := s.record.Workbench.Requests[oldID]
	r.CreatedAt = time.Now().Add(-time.Minute)
	s.record.Workbench.Requests[oldID] = r
	s.mu.Unlock()
	view := workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	if view.Requests[0].State != "failed" {
		t.Fatal("expired queue remained pending without a receiver poll", view.Requests)
	}
	role := workbenchDecode[SpaceAssistant](wb(t, a, s.record.ID, "assistant", workbenchInput{State: "initializing", BaseEpoch: 1, TargetID: coordinator.ID, RequestID: uuid.NewString()}))
	wb(t, a, s.record.ID, "cancel", workbenchInput{RequestID: role.BootstrapID})
	view = workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	if view.Assistant.State != "failed" {
		t.Fatal("cancelled bootstrap left role initializing", view.Assistant)
	}
	s.mu.Lock()
	s.record.State = "ended"
	s.mu.Unlock()
	if _, err := a.workbenchCall(ctx, s.record.ID, "receiver-check", workbenchInput{}); err == nil {
		t.Fatal("ended space allowed a new receiver")
	}
}

func TestIndependentReceiverDoesNotEnableExecutionAndScopesNativeTools(t *testing.T) {
	ctx := context.Background()
	a, f, s := spaceFixture(t)
	raw, err := a.createSpaceReceiver(ctx, s.record.ID, uuid.NewString(), "研究助手")
	if err != nil {
		t.Fatal(err)
	}
	out := workbenchDecode[map[string]any](raw)
	id := out["id"].(string)
	a.mu.Lock()
	receiver := a.receivers[id]
	a.mu.Unlock()
	if s.record.ExecutionRecord != nil || receiver.record.SourceID != "" || receiver.record.SessionID == f.source.ID || f.forks != 0 {
		t.Fatal("receiver forked/enabled execution")
	}
	if a.Active() != 1 {
		t.Fatal("active receiver omitted from Core exit status", a.Active())
	}
	if _, err = os.Stat(filepath.Join(receiver.record.ExecutionCwd, ".git")); !os.IsNotExist(err) {
		t.Fatal("receiver created a Git checkout")
	}
	if !strings.HasPrefix(receiver.record.ExecutionCwd, filepath.Join(a.Config.DataDir, "receivers")) {
		t.Fatal(receiver.record.ExecutionCwd)
	}
	for _, method := range f.calls {
		if method == "turn/start" {
			t.Fatal("creating receiver started a model turn")
		}
	}
	a.pumpWorkbench(ctx)
	view := workbenchDecode[WorkbenchView](wb(t, a, s.record.ID, "view", workbenchInput{}))
	if len(view.Targets) != 1 || !view.Targets[0].Available || view.Targets[0].Execution {
		t.Fatal(view)
	}
	caller := mcp.CallerSource{Provider: "codex", SourceID: receiver.record.SessionID, TurnID: "turn-fixture"}
	invoke := func(name string, args map[string]any, source mcp.CallerSource) *httptest.ResponseRecorder {
		data, _ := json.Marshal(map[string]any{"name": name, "arguments": args, "caller": source})
		req := httptest.NewRequest("POST", "http://127.0.0.1/api/runtime-annotations/"+id, strings.NewReader(string(data)))
		req.Header.Set("Authorization", "Bearer "+receiver.record.AnnotationToken)
		res := httptest.NewRecorder()
		a.Handler(http.NotFoundHandler()).ServeHTTP(res, req)
		return res
	}
	if res := invoke("read_space_brief", map[string]any{}, caller); res.Code != 200 || !strings.Contains(res.Body.String(), s.record.ID) {
		t.Fatal(res.Code, res.Body.String())
	}
	if res := invoke("read_space_brief", map[string]any{"spaceId": uuid.NewString()}, caller); res.Code == 200 {
		t.Fatal("receiver selected another space")
	}
	wrong := caller
	wrong.SourceID = uuid.NewString()
	if res := invoke("list_materials", map[string]any{}, wrong); res.Code == 200 {
		t.Fatal("token used from another session")
	}
	s.mu.Lock()
	s.record.Annotations = append(s.record.Annotations, Annotation{ID: "public", Text: "公开讨论"}, Annotation{ID: "private", Text: "私人执行内容", Target: &AnnotationTarget{Kind: "history"}})
	s.mu.Unlock()
	if res := invoke("read_annotations", map[string]any{}, caller); res.Code != 200 || strings.Contains(res.Body.String(), "私人执行内容") {
		t.Fatal(res.Code, res.Body.String())
	}
	if res := invoke("reply_to_annotation", map[string]any{"annotationId": "private", "text": "escape", "requestId": uuid.NewString()}, caller); res.Code == 200 {
		t.Fatal("receiver replied to private execution")
	}
	dataDir := a.Config.DataDir
	nativeID := receiver.record.SessionID
	a.Close()
	reopened, err := Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.sessions[s.record.ID].record.ExecutionRecord != nil || reopened.receivers[id].record.SessionID != nativeID || reopened.receivers[id].process != nil {
		t.Fatal("restart changed receiver or resumed it automatically")
	}
}
