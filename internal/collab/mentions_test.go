package collab

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func searchLibraryMentions(t *testing.T, a *App, query string) []libraryMention {
	t.Helper()
	out, err := a.libraryMentions(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(out)
	var result struct {
		Resources []libraryMention `json:"resources"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result.Resources
}

func TestMentionSearchVersionsTextAndNoLibraryMutation(t *testing.T) {
	a, s, mat, _ := libraryFixture(t)
	s.mu.Lock()
	v := s.record.Materials[0].Versions[0]
	v.Title, v.Version, v.CreatedAt = "Follow-up 调查", 2, time.Now()
	s.record.Materials[0].Versions = append(s.record.Materials[0].Versions, v)
	s.record.Annotations[0].Text = strings.Repeat("批注内容", 30) + "UNIQUE-尾部🧭"
	s.mu.Unlock()
	a.libraryMu.Lock()
	before, _ := json.Marshal(a.library)
	a.libraryMu.Unlock()
	for _, q := range []string{"v1", "版本1", "version 1", "调查 v1", "资源库验证 v1"} {
		items := searchLibraryMentions(t, a, q)
		if len(items) != 1 || items[0].Reference != mat {
			t.Fatal(q, items)
		}
	}
	if items := searchLibraryMentions(t, a, "FOLLOW-UP"); len(items) != 1 || items[0].Reference.Version != 2 {
		t.Fatal(items)
	}
	if items := searchLibraryMentions(t, a, "unique-尾部🧭"); len(items) != 1 || items[0].Reference.Kind != "annotation" {
		t.Fatal(items)
	}
	for _, q := range []string{"前置未公开", "后续未公开", "missing-query"} {
		if items := searchLibraryMentions(t, a, q); len(items) != 0 {
			t.Fatal(q, items)
		}
	}
	if items := searchLibraryMentions(t, a, ""); len(items) != 3 {
		t.Fatal(items)
	}
	if _, err := a.libraryMentions(context.Background(), strings.Repeat("字", 257)); err == nil {
		t.Fatal("oversized query accepted")
	}
	a.libraryMu.Lock()
	after, _ := json.Marshal(a.library)
	a.libraryMu.Unlock()
	if string(before) != string(after) || s.record.ExecutionRecord != nil {
		t.Fatal("search changed selection, bundles or execution")
	}
}

func TestMentionSearchBoundedStableAndLocalized(t *testing.T) {
	a, s, _, _ := libraryFixture(t)
	for i := 0; i < 24; i++ {
		if _, err := s.annotate(Annotation{Text: "分页测试 " + strings.Repeat("字", i+1)}, "作者"); err != nil {
			t.Fatal(err)
		}
	}
	first := searchLibraryMentions(t, a, "")
	second := searchLibraryMentions(t, a, "")
	x, _ := json.Marshal(first)
	y, _ := json.Marshal(second)
	if len(first) != 20 || string(x) != string(y) {
		t.Fatal(len(first), string(x), string(y))
	}
	for _, language := range []string{"en", "zh-CN"} {
		if err := a.SetUILanguage(language); err != nil {
			t.Fatal(err)
		}
		out, err := a.libraryMentions(context.Background(), "v1")
		if err != nil || out.(map[string]any)["language"] != language {
			t.Fatal(out, err)
		}
	}
}

func TestMentionSearchChecksWithdrawalAndRevocationBeforeReturningTitles(t *testing.T) {
	_, s, mat, _ := libraryFixture(t)
	ctx := context.Background()
	if err := s.Share(ctx, "lan"); err != nil {
		t.Fatal(err)
	}
	guest, _, _ := fixture(t)
	j, err := guest.Join(ctx, s.share.Token())
	if err != nil {
		t.Fatal(err)
	}
	if items := searchLibraryMentions(t, guest, "v1"); len(items) != 1 || items[0].Reference.SpaceID != j.ID {
		t.Fatal(items)
	}
	if _, err = s.withdrawMaterial(ctx, mat.MaterialID); err != nil {
		t.Fatal(err)
	}
	if items := searchLibraryMentions(t, guest, "v1"); len(items) != 0 {
		t.Fatal("withdrawn material returned", items)
	}
	if items := searchLibraryMentions(t, guest, "批注"); len(items) != 1 {
		t.Fatal(items)
	}
	for _, path := range []string{"/v2/library/mentions?query=", "/v2/library/mentions?query=v1"} {
		if err = j.request(ctx, "GET", path, nil, nil); err == nil {
			t.Fatal("personal mentions exposed on sharing listener")
		}
	}
	member := j.view(ctx)["selfId"].(string)
	if err = s.RevokeMember(member); err != nil {
		t.Fatal(err)
	}
	if items := searchLibraryMentions(t, guest, ""); len(items) != 0 {
		t.Fatal("cached titles returned after revocation", items)
	}
}
