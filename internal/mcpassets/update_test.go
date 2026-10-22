package mcpassets

import (
	"strings"
	"testing"
)

func TestBundleMatchesEmbeddedRevision(t *testing.T) {
	if Current.Protocol != 1 || Current.Script == "" || Current.Style == "" || !strings.Contains(Panel, `name="teamcross-panel-revision" content="`+Current.Revision+`"`) {
		t.Fatal("panel build and update bundle differ")
	}
	changed := ParseBundle(strings.Replace(Panel, "</script>", "\n// changed\n</script>", 1))
	if changed.Revision == Current.Revision {
		t.Fatal("script change reused old revision")
	}
}
