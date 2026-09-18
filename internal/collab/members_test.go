package collab

import (
	"context"
	"encoding/json"
	"testing"

	"teamcross/internal/nativecodex"
)

func TestThreeMembersSeparateInvitationsInputAndRevocation(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx := context.Background()
	first, err := s.Invite(ctx, "lan", "invite-b")
	if err != nil {
		t.Fatal(err)
	}
	b := joinFixture(t, s)
	bid := b.view(ctx)["selfId"].(string)
	next, err := s.Invite(ctx, "lan", "invite-c")
	if err != nil || first.Token == next.Token {
		t.Fatal("separate admission missing", err)
	}
	c := joinFixture(t, s)
	cid := c.view(ctx)["selfId"].(string)
	if bid == cid || bid == "owner" || cid == "owner" {
		t.Fatal("members share identity")
	}
	retry, err := s.Invite(ctx, "lan", "invite-b")
	if err != nil || retry.ID != first.ID || retry.State != "joined" || retry.Token != "" {
		t.Fatal("invite retry minted another admission", retry, err)
	}
	if err = s.Action(ctx, "handoff"); err == nil {
		t.Fatal("ambiguous handoff accepted")
	}
	epoch := s.view()["epoch"].(uint64)
	for _, j := range []*Joined{b, c} {
		if err = j.request(ctx, "POST", "/v2/request_input", map[string]any{"epoch": epoch}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.ActionFor(ctx, "handoff", bid, epoch); err != nil {
		t.Fatal(err)
	}
	if s.writer != bid || c.view(ctx)["inputRequested"] != true {
		t.Fatal("handoff consumed another member's request")
	}
	if err = c.request(ctx, "POST", "/v2/rpc", RPCInput{Method: "turn/start", RequestID: "same", Params: map[string]any{}}, nil); err == nil {
		t.Fatal("C wrote while B owns input")
	}
	if err = b.request(ctx, "POST", "/v2/rpc", RPCInput{Method: "turn/start", RequestID: "same", Params: map[string]any{}}, nil); err != nil {
		t.Fatal(err)
	}
	// Both members use the same human display name and request ID; authors and
	// deduplication must still distinguish their independently authenticated IDs.
	root, err := s.annotate(Annotation{Text: "review"}, "发起者")
	if err != nil {
		t.Fatal(err)
	}
	reply := AnnotationReplyInput{AnnotationID: root.ID, RequestID: "same-reply", Text: "response"}
	var result Annotation
	for _, j := range []*Joined{b, c} {
		if err = j.request(ctx, "POST", "/v2/annotation-replies", reply, &result); err != nil {
			t.Fatal(err)
		}
	}
	if len(result.Replies) != 2 || result.Replies[0].AuthorID == result.Replies[1].AuthorID {
		t.Fatal("member replies collapsed", result)
	}
	// Revoking a busy writer returns control without reporting the turn complete.
	before := s.view()["epoch"].(uint64)
	if err = s.RevokeMember(bid); err != nil {
		t.Fatal(err)
	}
	if s.view()["writer"] != "owner" || s.view()["busy"] != true || s.view()["epoch"].(uint64) <= before {
		t.Fatal("writer revocation corrupted runtime")
	}
	if err = b.request(ctx, "GET", "/v2/context?kind=annotations", nil, nil); err == nil {
		t.Fatal("revoked B still reads")
	}
	if err = c.request(ctx, "GET", "/v2/context?kind=annotations", nil, nil); err != nil {
		t.Fatal("revoking B affected C", err)
	}
	s.onMessage(nativecodex.Message{Method: "turn/completed"})
	if err = s.ActionFor(ctx, "handoff", cid); err != nil {
		t.Fatal(err)
	}
	if err = c.request(ctx, "POST", "/v2/rpc", RPCInput{Method: "turn/start", RequestID: "same", Params: map[string]any{}}, nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls := 0
	for _, method := range f.calls {
		if method == "turn/start" {
			calls++
		}
	}
	f.mu.Unlock()
	if calls != 2 {
		t.Fatal("one member reused another's write result", calls)
	}
	// Reusing a request ID with different content is rejected within a member.
	if err = c.request(ctx, "POST", "/v2/rpc", RPCInput{Method: "turn/start", RequestID: "same", Params: map[string]any{"input": []any{map[string]any{"type": "text", "text": "changed"}}}}, nil); err == nil {
		t.Fatal("changed write reused request ID")
	}
	var status map[string]any
	if err = c.request(ctx, "GET", "/v2/status", nil, &status); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(status)
	if status["invitation"] != nil || status["invitations"] != nil {
		t.Fatal("member received admission secrets", string(raw))
	}
}

func TestRevokePendingInvitationDoesNotRemoveOtherMembers(t *testing.T) {
	a, f, _ := fixture(t)
	s := createFixture(t, a, f, "existing")
	ctx := context.Background()
	if _, err := s.Invite(ctx, "lan", "B"); err != nil {
		t.Fatal(err)
	}
	b := joinFixture(t, s)
	pending, err := s.Invite(ctx, "lan", "C")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeInvitation(pending.ID); err != nil {
		t.Fatal(err)
	}
	c, err := Open(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.Join(ctx, pending.Token); err == nil {
		t.Fatal("revoked invitation admitted C")
	}
	if err = b.request(ctx, "GET", "/v2/status", nil, nil); err != nil {
		t.Fatal("revoking invite affected B", err)
	}
}
