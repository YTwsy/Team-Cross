package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"teamcross/internal/sharing"
)

func fixtureMaterialTurn(id, text string) MaterialTurn {
	return MaterialTurn{ID: id, Status: "completed", Items: []MaterialItem{{ID: "user", Type: "userMessage", Text: text}, {ID: "answer", Type: "agentMessage", Text: "答案：" + text}}}
}
func materialDraft(t *testing.T, a *App, f *fakeRuntime) PublicationDraft {
	t.Helper()
	f.mu.Lock()
	f.history = []MaterialTurn{fixtureMaterialTurn("later", "后续未公开"), fixtureMaterialTurn("public", "已选的验证结果🙂"), fixtureMaterialTurn("private", "前置未公开内容")}
	// Native userMessage shape contains content rather than text. The exporter
	// tests separately exercise that shape; use agent messages here.
	for i := range f.history {
		f.history[i].Items = f.history[i].Items[1:]
	}
	f.source.Cwd = filepath.Join(t.TempDir(), "missing-directory")
	f.mu.Unlock()
	d, err := a.FreezeSource(context.Background(), "codex", f.source.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err = a.PreviewPublication(PublicationSelection{DraftID: d.ID, Title: "范围内的调查", StartTurnID: "public", EndTurnID: "public", ReadingStartID: "public"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestReadOnlySpacePublicationBoundariesAndRecovery(t *testing.T) {
	ctx := context.Background()
	a, f, _ := fixture(t)
	d := materialDraft(t, a, f)
	s, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "只读调查"})
	if err != nil {
		t.Fatal(err)
	}
	if s.record.ExecutionRecord != nil || s.process != nil {
		t.Fatal("read-only space created execution")
	}
	if err = s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	inv, err := sharing.Decode(s.share.Token())
	if err != nil || !inv.ReadOnly || inv.RuntimeMode != "" {
		t.Fatal(inv, err)
	}
	b, _, _ := fixture(t)
	j, err := b.Join(ctx, s.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	in := PublishInput{PreviewID: d.ID, PreviewHash: d.Hash, RequestID: uuid.NewString()}
	out, err := a.Publish(ctx, s.record.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	result := out.(map[string]any)
	mid := result["materialId"].(string)
	// The source advances after review, but publication reads the frozen preview.
	f.mu.Lock()
	f.history = append([]MaterialTurn{fixtureMaterialTurn("new", "绝不能自动公开")}, f.history...)
	f.mu.Unlock()
	if _, err = a.Publish(ctx, s.record.ID, in); err != nil || len(s.record.Materials) != 1 || len(s.record.Materials[0].Versions) != 1 {
		t.Fatal("duplicate publish", err)
	}
	view, _ := json.Marshal(j.view(ctx))
	for _, secret := range []string{"前置未公开", "后续未公开", "绝不能自动公开", "答案：已选", "missing-directory", "providerHome", "runtimeMode"} {
		if strings.Contains(string(view), secret) {
			t.Fatalf("directory leaked %s", secret)
		}
	}
	var page map[string]any
	if err = j.request(ctx, "POST", "/v2/read-material", MaterialRead{MaterialID: mid, Version: 1}, &page); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(page)
	if !strings.Contains(string(encoded), "已选的验证结果") || strings.Contains(string(encoded), "未公开") {
		t.Fatal(string(encoded))
	}
	for _, kind := range []string{"history", "file", "changes", "events"} {
		if err = j.request(ctx, "GET", "/v2/context?kind="+kind+"&path=file.txt", nil, nil); err == nil {
			t.Fatalf("read-only exposes %s", kind)
		}
	}
	for _, path := range []string{"/v2/publications/source", "/v2/sources", "/v2/publications/draft"} {
		if err = j.request(ctx, "POST", path, map[string]any{"sourceId": f.source.ID, "draftId": d.ID}, nil); err == nil {
			t.Fatalf("private route exposed %s", path)
		}
	}
	if err = j.request(ctx, "POST", "/v2/request_input", map[string]any{"epoch": s.epoch}, nil); err == nil {
		t.Fatal("readonly requested input")
	}
	if _, err = s.RPC(ctx, "owner", "turn/start", nil, uuid.NewString()); err == nil {
		t.Fatal("readonly sent input")
	}
	if _, err = s.Context(ctx, "file", "../publication-drafts/"+d.ID+".json", 0); err == nil {
		t.Fatal("draft reachable")
	}
	if _, err = a.ClientPlan(ctx, s.record.ID, "tui", false); err == nil {
		t.Fatal("native client enabled")
	}
	var annotation Annotation
	ref := []MaterialReference{{MaterialID: mid, Version: 1, TurnID: "public"}}
	if err = j.request(ctx, "POST", "/v2/annotations", Annotation{Text: "已经核对", Materials: ref, Target: &AnnotationTarget{Kind: "material", MaterialID: mid, Version: 1, TurnID: "public", ItemID: "answer", Quote: "答案", StartOffset: 0, EndOffset: 2}}, &annotation); err != nil {
		t.Fatal(err)
	}
	if annotation.Target.SessionID != "" || annotation.AuthorID == "owner" {
		t.Fatal(annotation)
	}
	if err = j.request(ctx, "POST", "/v2/annotations", Annotation{Text: "越界", Target: &AnnotationTarget{Kind: "material", MaterialID: mid, Version: 1, TurnID: "private", ItemID: "answer", Quote: "秘密", EndOffset: 2}}, nil); err == nil {
		t.Fatal("unpublished reference accepted")
	}
	if err = j.request(ctx, "POST", "/v2/withdraw-material", materialIDInput{mid}, nil); err == nil {
		t.Fatal("guest withdrew owner's material")
	}
	for _, method := range f.calls {
		if method == "thread/fork" || method == "thread/resume" || method == "turn/start" {
			t.Fatalf("read-only called %s", method)
		}
	}
	id, dataDir := s.record.ID, a.Config.DataDir
	a.Close()
	a2, err := Open(Config{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer a2.Close()
	s2, err := a2.owned(id)
	if err != nil {
		t.Fatal(err)
	}
	if s2.share != nil || s2.record.ExecutionRecord != nil {
		t.Fatal("restart revived execution/admission")
	}
	if _, err = s2.readMaterial(ctx, MaterialRead{MaterialID: mid, Version: 1}); err != nil {
		t.Fatal(err)
	}
	status, err := s2.publicationStatus(ctx, in.RequestID)
	if err != nil || status.(map[string]any)["materialId"] != mid {
		t.Fatal(status, err)
	}
	reply := AnnotationReplyInput{AnnotationID: annotation.ID, RequestID: uuid.NewString(), Text: "固定引用的回复", Materials: ref}
	if _, err = s2.replyAnnotation(ctx, reply, "发起者"); err != nil {
		t.Fatal(err)
	}
	if _, err = s2.withdrawMaterial(ctx, mid); err != nil {
		t.Fatal(err)
	}
	if _, err = s2.readMaterial(ctx, MaterialRead{MaterialID: mid, Version: 1}); err == nil {
		t.Fatal("withdrawn material still readable")
	}
	if _, err = s2.replyAnnotation(ctx, reply, "发起者"); err != nil {
		t.Fatal("saved reply retry must survive referenced material withdrawal", err)
	}
	reply.RequestID = uuid.NewString()
	if _, err = s2.replyAnnotation(ctx, reply, "发起者"); err == nil {
		t.Fatal("new reply must not attach withdrawn material")
	}
}

func TestThreeMembersPublishVersionsAndPinnedReferences(t *testing.T) {
	ctx := context.Background()
	a, _, _ := fixture(t)
	s, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "三人调查"})
	if err != nil {
		t.Fatal(err)
	}
	b, bf, _ := fixture(t)
	c, cf, _ := fixture(t)
	i1, err := s.Invite(ctx, "lan", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	jb, err := b.Join(ctx, i1.Token)
	if err != nil {
		t.Fatal(err)
	}
	i2, err := s.Invite(ctx, "lan", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	jc, err := c.Join(ctx, i2.Token)
	if err != nil {
		t.Fatal(err)
	}
	db, dc := materialDraft(t, b, bf), materialDraft(t, c, cf)
	bin := PublishInput{PreviewID: db.ID, PreviewHash: db.Hash, RequestID: uuid.NewString()}
	bout, err := b.Publish(ctx, jb.ID, bin)
	if err != nil {
		t.Fatal(err)
	}
	bid := bout.(map[string]any)["materialId"].(string)
	if _, err = c.Publish(ctx, jc.ID, PublishInput{PreviewID: dc.ID, PreviewHash: dc.Hash, RequestID: bin.RequestID}); err != nil {
		t.Fatal("member dedup collided", err)
	}
	if len(s.record.Materials) != 2 {
		t.Fatal("missing concurrent contributions")
	}
	if _, err = c.Publish(ctx, jc.ID, PublishInput{PreviewID: dc.ID, PreviewHash: dc.Hash, RequestID: uuid.NewString(), MaterialID: bid, BaseVersion: 1}); err == nil {
		t.Fatal("C updated B's contribution")
	}
	db.MaterialContent.Turns = append([]MaterialTurn{}, db.Turns...)
	db.Turns = append(db.Turns, fixtureMaterialTurn("verification", "新增验证"))
	db.EndTurnID = "verification"
	db.ID = uuid.NewString()
	db.Hash = contentHash(db.MaterialContent)
	if err = b.saveDraft(db); err != nil {
		t.Fatal(err)
	}
	update := PublishInput{PreviewID: db.ID, PreviewHash: db.Hash, RequestID: uuid.NewString(), MaterialID: bid, BaseVersion: 1}
	if _, err = b.Publish(ctx, jb.ID, update); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Publish(ctx, jb.ID, update); err != nil {
		t.Fatal("retry not idempotent", err)
	}
	update.RequestID = uuid.NewString()
	if _, err = b.Publish(ctx, jb.ID, update); err == nil {
		t.Fatal("stale base accepted")
	}
	var one, two any
	if err = jc.request(ctx, "POST", "/v2/read-material", MaterialRead{MaterialID: bid, Version: 1}, &one); err != nil {
		t.Fatal(err)
	}
	if err = jc.request(ctx, "POST", "/v2/read-material", MaterialRead{MaterialID: bid, Version: 2}, &two); err != nil {
		t.Fatal(err)
	}
	b1, _ := json.Marshal(one)
	b2, _ := json.Marshal(two)
	if strings.Contains(string(b1), "新增验证") || !strings.Contains(string(b2), "新增验证") {
		t.Fatal("versions not pinned")
	}
	// Failed persistence must not turn an unacknowledged upload into a success.
	path := filepath.Join(a.Config.DataDir, "collaborations", s.record.ID, "collaboration.json")
	backup := path + ".test-save"
	if err = os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	update.BaseVersion = 2
	if _, err = b.Publish(ctx, jb.ID, update); err == nil {
		t.Fatal("reported saved on disk failure")
	}
	if len(s.record.Materials[0].Versions) != 2 {
		t.Fatal("failed save changed memory")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeMember(s.record.Materials[0].AuthorID); err != nil {
		t.Fatal(err)
	}
	if err = jb.request(ctx, "POST", "/v2/read-material", MaterialRead{MaterialID: bid, Version: 1}, nil); err == nil {
		t.Fatal("revoked B read")
	}
	if err = jc.request(ctx, "POST", "/v2/read-material", MaterialRead{MaterialID: bid, Version: 1}, nil); err != nil {
		t.Fatal("B removal lost C's access or material", err)
	}
}

func TestEnableExecutionPreservesMembersLinkAndAccessScopes(t *testing.T) {
	ctx := context.Background()
	a, f, _ := fixture(t)
	s, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "调查后执行"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.annotate(Annotation{Text: "讨论保留"}, "发起者"); err != nil {
		t.Fatal(err)
	}
	link, err := s.Invite(ctx, "lan", "space-link")
	if err != nil {
		t.Fatal(err)
	}
	b, bf, _ := fixture(t)
	j, err := b.Join(ctx, link.Token)
	if err != nil {
		t.Fatal(err)
	}
	bid := j.view(ctx)["selfId"].(string)
	draft := materialDraft(t, b, bf)
	published, err := b.Publish(ctx, j.ID, PublishInput{PreviewID: draft.ID, PreviewHash: draft.Hash, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	mid := published.(map[string]any)["materialId"].(string)
	in := CreateInput{SpaceID: s.record.ID, RequestID: uuid.NewString(), SourceID: f.source.ID, WorkspaceMode: "existing"}
	p, err := a.Preview(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	in.PreviewHash = p.Hash
	created, err := a.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if created != s || s.record.ExecutionRecord == nil || len(s.record.Annotations) != 1 || f.forks != 1 || s.share == nil || s.share.Token() != link.Token {
		t.Fatal("enabling execution changed space, link or membership")
	}
	if _, err = a.Create(ctx, in); err != nil || f.forks != 1 {
		t.Fatal("execution retry forked twice", err)
	}
	private, err := s.annotate(Annotation{Text: "原生范围批注", Target: &AnnotationTarget{Kind: "history", TurnID: "native", ItemID: "item", Quote: "未公开", EndOffset: 3}}, "发起者")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"history", "file", "changes", "events"} {
		if err = j.request(ctx, "GET", "/v2/context?kind="+kind+"&path=file.txt", nil, nil); err == nil {
			t.Fatal("readonly obtained native context", kind)
		}
	}
	if view := j.view(ctx); view["selfId"] != bid || view["hasExecution"] != false || view["executionAvailable"] != true || view["runtimeState"] != nil || view["writer"] != nil || view["executionCwd"] != nil || view["state"] != "ready" {
		t.Fatal("readonly member lost identity or received native state", view)
	}
	var annotations map[string]any
	if err = j.request(ctx, "GET", "/v2/context?kind=annotations", nil, &annotations); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(annotations)
	if strings.Contains(string(raw), "原生范围批注") || annotations["sessionId"] != nil {
		t.Fatal("native annotation leaked", string(raw))
	}
	if err = j.request(ctx, "POST", "/v2/annotation-replies", AnnotationReplyInput{AnnotationID: private.ID, Text: "越界", RequestID: uuid.NewString()}, nil); err == nil {
		t.Fatal("readonly replied to hidden native annotation")
	}
	if err = j.request(ctx, "POST", "/v2/rpc", RPCInput{Method: "thread/read", Params: map[string]any{}}, nil); err == nil {
		t.Fatal("readonly accessed native RPC")
	}
	if err = s.ActionFor(ctx, "handoff", bid); err == nil {
		t.Fatal("handoff implicitly granted execution")
	}
	if _, err = b.Publish(ctx, j.ID, PublishInput{PreviewID: draft.ID, PreviewHash: draft.Hash, RequestID: uuid.NewString(), MaterialID: mid, BaseVersion: 1}); err != nil {
		t.Fatal("member can no longer update their publication", err)
	}
	c, _, _ := fixture(t)
	jc, err := c.Join(ctx, link.Token)
	if err != nil || jc.view(ctx)["hasExecution"] != false {
		t.Fatal("old link stopped working or broadened access", err)
	}
	owner := managementBackend(t, a)
	guest := managementBackend(t, b)
	if _, err = guest.Invoke(ctx, "set_execution_access", map[string]any{"id": j.ID, "memberId": bid, "allowed": true}); err == nil {
		t.Fatal("guest granted their own execution access")
	}
	invokeObject(t, owner, "set_execution_access", map[string]any{"id": s.record.ID, "memberId": bid, "allowed": true})
	if view := j.view(ctx); view["selfId"] != bid || view["hasExecution"] != true || view["sessionId"] != s.record.SessionID {
		t.Fatal("grant required rejoining", view)
	}
	if err = j.request(ctx, "GET", "/v2/context?kind=history", nil, nil); err != nil {
		t.Fatal("granted native read rejected", err)
	}
	if err = s.ActionFor(ctx, "handoff", bid); err != nil {
		t.Fatal(err)
	}
	if err = s.SetExecutionAccess(bid, false); err != nil {
		t.Fatal(err)
	}
	if s.view()["writer"] != "owner" || !s.share.HasMember(bid) {
		t.Fatal("revocation failed to preserve membership and reclaim input")
	}
	if err = j.request(ctx, "GET", "/v2/context?kind=history", nil, nil); err == nil {
		t.Fatal("revoked execution access survived")
	}
	if err = j.request(ctx, "POST", "/v2/read-material", MaterialRead{MaterialID: mid, Version: 1}, nil); err != nil {
		t.Fatal("execution revocation ended material access", err)
	}
	reset, err := s.Invite(ctx, "lan", "rotated", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Join(ctx, reset.Token); err != nil || len(b.joined) != 1 {
		t.Fatal("reopening link duplicated membership", err)
	}
}

func TestReadOnlySpaceCanCloseBeforeAnyoneJoinsAndReopen(t *testing.T) {
	ctx := context.Background()
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			a, _, _ := fixture(t)
			s, err := a.CreateSpace(ctx, SpaceInput{RequestID: uuid.NewString(), Title: "未加入的空间"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.annotate(Annotation{Text: "保留讨论"}, "发起者"); err != nil {
				t.Fatal(err)
			}
			old := ""
			if shared {
				if err = s.Share(ctx, "lan"); err != nil {
					t.Fatal(err)
				}
				old = s.share.Token()
			}
			if err = s.Action(ctx, "end"); err != nil {
				t.Fatal(err)
			}
			if s.view()["state"] != "ended" || s.share != nil {
				t.Fatal("close only hid invitation")
			}
			id, dir := s.record.ID, a.Config.DataDir
			a.Close()
			reopened, err := Open(Config{DataDir: dir, Loopback: true})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			restored, err := reopened.owned(id)
			if err != nil || restored.view()["state"] != "ended" || len(restored.record.Annotations) != 1 {
				t.Fatal("closed state/materials did not persist", err)
			}
			link, err := restored.Invite(ctx, "lan", "reopen")
			if err != nil || restored.view()["state"] != "ready" || link.Token == "" || link.Token == old {
				t.Fatal("reopen failed", err)
			}
		})
	}
}

func TestMaterialCursorAndExport(t *testing.T) {
	turn, err := exportTurn(json.RawMessage(`{"id":"t","status":"completed","items":[{"id":"u","type":"userMessage","content":[{"type":"text","text":"提问"},{"type":"localImage","path":"/private/secret.png"}]},{"id":"r","type":"reasoning","text":"hidden"},{"id":"tool","type":"commandExecution","command":"echo result","aggregatedOutput":"saved-output","private":"token"},{"id":"future","type":"unknown","secret":"not exported"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(turn)
	for _, private := range []string{"secret.png", "hidden", "token", "not exported"} {
		if strings.Contains(string(b), private) {
			t.Fatal("export leaked", private)
		}
	}
	if !strings.Contains(string(b), "saved-output") || turn.Items[0].Notice == "" || turn.Items[2].Notice == "" {
		t.Fatal("missing saved tool output / limitations")
	}
	a, _, _ := fixture(t)
	s, _ := a.CreateSpace(context.Background(), SpaceInput{RequestID: uuid.NewString(), Title: "分页"})
	content := MaterialContent{Title: "大工具输出", Provider: "codex", SourceID: uuid.NewString(), StartTurnID: "t", EndTurnID: "t", Turns: []MaterialTurn{{ID: "t", Status: "completed", Items: []MaterialItem{{ID: "i", Type: "toolResult", Text: strings.Repeat("🙂", 32010)}}}}}
	out, err := s.publish(context.Background(), publicationUpload{Content: content, RequestID: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	mid := out.(map[string]any)["materialId"].(string)
	page, err := s.readMaterial(context.Background(), MaterialRead{MaterialID: mid, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	cursor := page.(map[string]any)["nextCursor"].(string)
	if cursor == "" {
		t.Fatal("unbounded tool output")
	}
	if _, err = s.readMaterial(context.Background(), MaterialRead{MaterialID: mid, Version: 1, Cursor: cursor}); err != nil {
		t.Fatal(err)
	}
	other, _ := a.CreateSpace(context.Background(), SpaceInput{RequestID: uuid.NewString(), Title: "另一个空间"})
	if _, err = other.readMaterial(context.Background(), MaterialRead{MaterialID: mid, Version: 1, Cursor: cursor}); err == nil {
		t.Fatal("cross-space cursor read")
	}
}
