package mcpassets

import (
	"strings"
	"testing"
)

func TestBundleMatchesEmbeddedRevision(t *testing.T) {
	if Current.Protocol != 1 || Current.RequiredBridgeProtocol != 1 || Current.Script == "" || Current.Style == "" || !strings.Contains(Panel, `name="teamcross-panel-revision" content="`+Current.Revision+`"`) {
		t.Fatal("panel build and update bundle differ")
	}
	changed := ParseBundle(strings.Replace(Panel, "</script>", "\n// changed\n</script>", 1))
	if changed.Revision == Current.Revision {
		t.Fatal("script change reused old revision")
	}
}

func TestBundleBridgeRequirementIsSeparateFromTransport(t *testing.T) {
	for _, tc := range []struct {
		meta     string
		required int
	}{
		{"", 0},
		{`<meta name="teamcross-bridge-protocol" content="2">`, 2},
		{`<meta name="teamcross-bridge-protocol" content="bad">`, -1},
	} {
		b := ParseBundle(tc.meta + `<style id="teamcross-panel-style">x</style><script type="module">y</script>`)
		if b.RequiredBridgeProtocol != tc.required || b.Protocol != 1 {
			t.Fatal(b)
		}
	}
}
