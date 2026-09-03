package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSessionSourceProvenanceIsOptionalAndNotAuthority(t *testing.T) {
	old := []byte(`{"provider":"codex","sessionId":"native","identityKind":"thread.id","surface":"unknown"}`)
	var source SessionRef
	if err := json.Unmarshal(old, &source); err != nil || source.ProviderSource != "" {
		t.Fatalf("old source is not compatible: %+v %v", source, err)
	}
	encoded, err := json.Marshal(source)
	if err != nil || strings.Contains(string(encoded), "providerSource") {
		t.Fatalf("absent provenance was not omitted: %s %v", encoded, err)
	}
	source.ProviderSource = "vscode"
	encoded, err = json.Marshal(source)
	var restored SessionRef
	if err != nil || json.Unmarshal(encoded, &restored) != nil || restored.ProviderSource != "vscode" {
		t.Fatalf("provenance round trip failed: %s %v", encoded, err)
	}
	withoutToken := source
	withoutToken.ProviderSource = ""
	if !SameNativeLiveSource(source, withoutToken) {
		t.Fatal("provenance changed native identity or fencing")
	}
	raw := SessionSnapshot{ID: "snapshot", ThreadID: "thread", CapturedAt: time.Now().UTC(), Source: source, Entries: []SessionEntry{{ID: "entry", Kind: "message", Text: "allowed"}}}
	public, _, err := ProjectNativeSnapshot(raw, []string{"message"})
	if err != nil || public.Source.ProviderSource != "" || public.Source.Surface != "unknown" {
		t.Fatalf("public native source leaked provenance or inferred a UI: %+v %v", public.Source, err)
	}
	if raw.Source.ProviderSource != "vscode" {
		t.Fatal("projection modified the private snapshot")
	}
}
