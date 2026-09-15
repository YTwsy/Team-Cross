package sharing

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"teamcross/internal/problem"
	"testing"
	"time"

	"github.com/tailscale/tailcat"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

func TestInvitationLink(t *testing.T) {
	r := Runtime{Invitation: Invitation{Version: 3, ID: "test", Title: "协作", Host: "A", Transport: TransportLAN, Endpoints: []string{"127.0.0.1:1234"}, Pin: strings.Repeat("a", 64), Secret: strings.Repeat("b", 40), ExpiresAt: time.Now().Add(time.Hour), Capability: Capability}}
	token := r.Token()
	for _, v := range []string{token, "teamcross://join?invite=" + url.QueryEscape(token), "邀请你加入协作\nteamcross://join?invite=" + url.QueryEscape(token) + "\n原始邀请码：\n" + token + "\n请先安装 Team Cross"} {
		i, e := Decode(v)
		if e != nil || i.ID != "test" {
			t.Fatal(i, e)
		}
	}
	for _, v := range []string{"teamcross://other?invite=" + token, "teamcross://join?invite=" + token + "&invite=" + token, "teamcross://u:p@join?invite=" + token} {
		if _, e := Decode(v); e == nil {
			t.Fatal("accepted malformed link")
		}
	}
	for _, endpoint := range []string{"203.0.113.1:1234", "[fd00::1]:1234"} {
		invalid := Runtime{Invitation: r.Invitation}
		invalid.Invitation.Endpoints = []string{endpoint}
		if _, e := Decode(invalid.Token()); problem.Describe(e).Code != "version_incompatible" {
			t.Fatal("accepted non-LAN endpoint", endpoint, e)
		}
	}
	r.Invitation.ExpiresAt = time.Now().Add(-time.Hour)
	_, e := Decode(r.Token())
	if problem.Describe(e).Code != "invitation_expired" {
		t.Fatal(e)
	}
}

func TestTailcatInvitation(t *testing.T) {
	info := tailcat.ConnInfo{
		ServerPublic:      tailcat.NodePublic{NodePublic: key.NewNode().Public()},
		ServerDiscoPublic: tailcat.DiscoPublic{DiscoPublic: key.NewDisco().Public()},
		PresharedKey:      tailcat.NewPresharedKey(),
		RegionID:          tailcfg.DERPRegionID(1),
	}
	r := Runtime{Invitation: Invitation{
		Version: 3, ID: "tailcat-test", Title: "跨网络协作", Host: "A",
		Transport: TransportTailcat,
		Tailcat:   &TailcatCandidate{Address: string(info.Addr()), Port: TailcatVirtualPort, LibraryVersion: TailcatLibraryVersion},
		Pin:       strings.Repeat("a", 64), Secret: strings.Repeat("b", 40), ExpiresAt: time.Now().Add(time.Hour), Capability: Capability,
	}}
	invitation, err := Decode(r.Token())
	if err != nil || invitation.Transport != TransportTailcat || invitation.Tailcat == nil {
		t.Fatal(invitation, err)
	}
	connection, err := NewConnection(invitation, "")
	if err != nil {
		t.Fatal(err)
	}
	if connection.Transport != TransportTailcat || connection.URL != "https://tailcat.invalid" {
		t.Fatal(connection.Transport, connection.URL)
	}
	_ = connection.Close()

	invitation.Tailcat.LibraryVersion = "v0.5.0"
	b, _ := json.Marshal(invitation)
	if _, err = Decode("tcx3." + base64.RawURLEncoding.EncodeToString(b)); problem.Describe(err).Code != "version_incompatible" {
		t.Fatal("accepted incompatible Tailcat invitation", err)
	}
	invitation.Tailcat.LibraryVersion = TailcatLibraryVersion
	invitation.Tailcat.Address = "tc-not-an-address"
	b, _ = json.Marshal(invitation)
	if _, err = Decode("tcx3." + base64.RawURLEncoding.EncodeToString(b)); problem.Describe(err).Code != "version_incompatible" {
		t.Fatal("accepted malformed Tailcat address", err)
	}
}
