package sharing

import (
	"net/url"
	"strings"
	"teamcross/internal/problem"
	"testing"
	"time"
)

func TestInvitationLink(t *testing.T) {
	r := Runtime{Invitation: Invitation{Version: 2, ID: "test", Title: "协作", Host: "A", Endpoints: []string{"127.0.0.1:1234"}, Pin: strings.Repeat("a", 64), Secret: strings.Repeat("b", 40), ExpiresAt: time.Now().Add(time.Hour), Capability: Capability}}
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
	r.Invitation.ExpiresAt = time.Now().Add(-time.Hour)
	_, e := Decode(r.Token())
	if problem.Describe(e).Code != "invitation_expired" {
		t.Fatal(e)
	}
}
