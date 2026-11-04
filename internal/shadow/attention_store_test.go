package shadow

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"teamcross/internal/spacecontext"
)

func attentionFixture(t *testing.T) (*AttentionStore, AttentionReceipt, spacecontext.Signal) {
	t.Helper()
	s, err := OpenAttention(t.TempDir(), "space", "owner")
	if err != nil {
		t.Fatal(err)
	}
	route := AttentionRoute{ShadowID: "first-shadow", Revision: 1, Enabled: true}
	if err := s.SetRoute(route); err != nil {
		t.Fatal(err)
	}
	receipt := AttentionReceipt{Token: "host-receipt-1", Route: route, Through: 1}
	signal := spacecontext.Signal{ID: "event-1", Seq: 1, Kind: "reply", Source: spacecontext.SourceRef{SpaceID: "space", AnnotationID: "discussion", ReplyID: "reply-1"}, Subject: spacecontext.Subject{Kind: "discussion", ID: "discussion"}, Actor: spacecontext.Actor{Kind: "human", MemberID: "colleague"}, CreatedAt: time.Now(), Preview: "the source must still be read", Reasons: []string{"follow_discussion"}}
	return s, receipt, signal
}

func TestAttentionReceiptRestartRouteChangeAndIsolation(t *testing.T) {
	s, receipt, signal := attentionFixture(t)
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Accept(receipt, []spacecontext.Signal{signal}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	before := s.Snapshot()
	if len(before.Events) != 1 || len(before.Focuses) != 1 || before.Pending == nil {
		t.Fatal(before)
	}
	for _, f := range before.Focuses {
		if f.Summary != "" || f.ProcessedSeq != 0 || f.Processing != "queued" {
			t.Fatal("persistence was mistaken for understanding", f)
		}
	}
	reloaded, err := OpenAttention(s.directory, "space", "owner")
	if err != nil || !reflect.DeepEqual(before, reloaded.Snapshot()) {
		t.Fatal("restart lost received work", err)
	}
	changed := receipt.Route
	changed.ShadowID, changed.Revision = "second-shadow", 2
	if err := reloaded.SetRoute(changed); err != nil {
		t.Fatal(err)
	}
	after := reloaded.Snapshot()
	if !reflect.DeepEqual(before.Events, after.Events) || !reflect.DeepEqual(before.Focuses, after.Focuses) || after.Pending != nil {
		t.Fatal("route change discarded durable work or retained stale receipt", after)
	}
	if err := reloaded.Accept(receipt, []spacecontext.Signal{signal}); err == nil {
		t.Fatal("old route accepted a new batch")
	}
	receipt.Route, receipt.Token = changed, "replayed-on-new-route"
	if err := reloaded.Accept(receipt, []spacecontext.Signal{signal}); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Snapshot().Events) != 1 {
		t.Fatal("route replay duplicated the event")
	}
	if _, err := OpenAttention(s.directory, "other-space", "owner"); err == nil {
		t.Fatal("another space adopted the store")
	}
	if _, err := OpenAttention(s.directory, "space", "other-member"); err == nil {
		t.Fatal("another member adopted the store")
	}
	forged := signal
	forged.Source.ReplyID = "a-different-reply"
	if err := reloaded.Accept(receipt, []spacecontext.Signal{forged}); err == nil {
		t.Fatal("same event id accepted a different source")
	}
}

func TestAttentionFailedPersistenceNeverAcknowledges(t *testing.T) {
	s, receipt, signal := attentionFixture(t)
	path := filepath.Join(s.directory, "attention.json")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.Accept(receipt, []spacecontext.Signal{signal}); err == nil {
		t.Fatal("accepted without durable storage")
	}
	if r := s.Snapshot(); len(r.Events) != 0 || len(r.Focuses) != 0 || r.Pending != nil {
		t.Fatal("failed mutation changed live state", r)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if err := s.Accept(receipt, []spacecontext.Signal{signal}); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmReceipt(receipt); err != nil || s.PendingReceipt() != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmReceipt(receipt); err != nil {
		t.Fatal("confirmation is not idempotent", err)
	}
	if len(s.Snapshot().Events) != 1 {
		t.Fatal("receipt confirmation deleted unprocessed work")
	}
}

func TestAttentionRejectsMixedScopeBatchAtomically(t *testing.T) {
	s, receipt, signal := attentionFixture(t)
	other := signal
	other.ID, other.Source.SpaceID = "wrong-scope", "other-space"
	if err := s.Accept(receipt, []spacecontext.Signal{signal, other}); err == nil {
		t.Fatal("mixed-scope batch accepted")
	}
	if len(s.Snapshot().Events) != 0 {
		t.Fatal("invalid batch partially persisted")
	}
	e, _, _ := setup(t)
	if err := e.AttachAttention(s); err == nil {
		t.Fatal("engine attached another space's attention")
	}
}
