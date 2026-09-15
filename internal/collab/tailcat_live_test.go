package collab

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"
	"teamcross/internal/sharing"
)

// TestLiveTailcatCollaboration is an opt-in, same-Mac integration smoke. It
// covers Team Cross's explicit transport selection, one-time admission,
// persistent member status and direct-client WebSocket bridge over Tailcat.
// Two physical Macs and a forced relay path remain separate acceptance gates.
func TestLiveTailcatCollaboration(t *testing.T) {
	if os.Getenv("TEAMCROSS_TEST_TAILCAT") != "1" {
		t.Skip("set TEAMCROSS_TEST_TAILCAT=1 to use Tailcat's network")
	}

	a, f, _ := fixture(t)
	session := createFixture(t, a, f, "existing")
	b, _, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	if err := session.Share(ctx, string(sharing.TransportTailcat)); err != nil {
		t.Fatal(err)
	}
	ownerView := session.view()
	if ownerView["sharing"] != true || ownerView["transport"] != "tailcat" || ownerView["invitationState"] != "pending" {
		t.Fatalf("unexpected owner view: %#v", ownerView)
	}

	joined, err := b.Join(ctx, session.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	remoteView := joined.view(ctx)
	if remoteView["online"] != true || remoteView["state"] != "ready" || remoteView["transport"] != "tailcat" {
		t.Fatalf("unexpected remote view: %#v", remoteView)
	}

	if err = session.Action(ctx, "handoff"); err != nil {
		t.Fatal(err)
	}
	endpoint, err := b.endpoint(joined.ID)
	if err != nil {
		t.Fatal(err)
	}
	direct, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	if session.view()["connected"] != true {
		t.Fatal("remote direct client did not reach the shared runtime")
	}
	_ = direct.Close(websocket.StatusNormalClosure, "test complete")
	eventually(t, func() bool { return session.view()["connected"] == false })
}
