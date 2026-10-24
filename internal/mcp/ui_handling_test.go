package mcp

import "testing"

func TestUIQuestionHandlingRoutesKeepLocalShape(t *testing.T) {
	for _, write := range []bool{false, true} {
		var body any
		if write {
			body = map[string]any{"shadowId": "owned"}
		}
		method, err := uiRoute("collaborations/space/attention/route", body, write)
		want := "GET"
		if write {
			want = "POST"
		}
		if err != nil || method != want {
			t.Fatal(method, err)
		}
	}
	for _, path := range []string{"collaborations/space/attention?direction=needs_owner&limit=30", "collaborations/space/attention/handling?annotationId=question&replyId=reply&executionId=execution", "collaborations/space/attention/questions?shadowId=owned&history=true&cursor=page&limit=10", "collaborations/space/attention/question?shadowId=owned&id=question&cursor=conditions"} {
		if method, err := uiRoute(path, nil, false); err != nil || method != "GET" {
			t.Fatal(path, method, err)
		}
	}
	if method, err := uiRoute("collaborations/space/attention/event?eventId=known", nil, false); err != nil || method != "GET" {
		t.Fatal(method, err)
	}
	if _, err := uiRoute("collaborations/space/attention/event?memberId=other", nil, false); err == nil {
		t.Fatal("event detail accepted another identity")
	}
	if method, err := uiRoute("collaborations/space/attention/handling", map[string]any{"handler": "owner"}, true); err != nil || method != "POST" {
		t.Fatal(method, err)
	}
	for _, path := range []string{"collaborations/space/attention?memberId=other", "collaborations/space/attention/handling?shadowId=other", "collaborations/space/attention/questions?memberId=other", "collaborations/space/attention/question?prompt=inject", "v2/attention", "collaborations/space/attention/private"} {
		if _, err := uiRoute(path, nil, false); err == nil {
			t.Fatal("unexpected private route", path)
		}
	}
	if _, err := uiRoute("collaborations/space/attention/question", map[string]any{"id": "question"}, true); err == nil {
		t.Fatal("question detail accepted a write")
	}
}
