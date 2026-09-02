package transfer

import (
	"fmt"
	"strings"
	"testing"
)

func TestPatchRejectsCopyAmplificationRepeatedRenameAndNewSubmodules(t *testing.T) {
	var copies strings.Builder
	for index := 0; index < 65; index++ {
		fmt.Fprintf(&copies, "diff --git a/big.bin b/copy-%d.bin\nsimilarity index 100%%\ncopy from big.bin\ncopy to copy-%d.bin\n", index, index)
	}
	for name, patch := range map[string]string{
		"copy amplification": copies.String(),
		"repeated rename":    "diff --git a/big.bin b/one.bin\nsimilarity index 100%\nrename from big.bin\nrename to one.bin\ndiff --git a/big.bin b/two.bin\nsimilarity index 100%\nrename from \"big.bin\"\nrename to two.bin\n",
		"new gitlink":        "diff --git a/module b/module\nnew file mode 160000\nindex 0000000..1234567\n--- /dev/null\n+++ b/module\n@@ -0,0 +1 @@\n+Subproject commit 1234567890123456789012345678901234567890\n",
		"gitlink index":      "diff --git a/module b/module\nindex 1234567..7654321 160000\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateBinaryPatchLimits([]byte(patch)); err == nil {
				t.Fatal("unsafe expansion/dependency patch accepted")
			}
		})
	}
	if err := validateBinaryPatchLimits([]byte("diff --git a/one b/two\nsimilarity index 100%\nrename from one\nrename to two\n")); err != nil {
		t.Fatalf("ordinary rename rejected: %v", err)
	}
}
