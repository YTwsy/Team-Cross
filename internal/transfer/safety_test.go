package transfer

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"teamcross/internal/gitstate"
)

func TestBinaryDeltaPreflightAcceptsRealGitAndRejectsExpansion(t *testing.T) {
	bundle, repo := testBundle(t)
	data := make([]byte, 1<<20)
	for index := range data {
		data[index] = byte(index % 251)
	}
	writeTestFile(t, repo, "delta.bin", data)
	gitTest(t, repo, "add", "delta.bin")
	gitTest(t, repo, "commit", "-m", "binary baseline")
	baseline := strings.TrimSpace(string(gitTest(t, repo, "rev-parse", "HEAD")))
	data[100] = 249
	data[500000] = 77
	writeTestFile(t, repo, "delta.bin", data)
	patch, err := gitstate.ExportBinaryPatch(context.Background(), repo, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(patch, []byte("delta ")) {
		t.Fatal("fixture did not produce a real Git binary delta")
	}
	if err := validateBinaryPatchLimits(patch); err != nil {
		t.Fatalf("valid Git delta rejected: %v", err)
	}
	format, objects, err := CaptureBaseline(context.Background(), repo, baseline)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Baseline, bundle.ObjectFormat, bundle.GitObjects = baseline, format, objects
	bundle.Snapshot = gitstate.Snapshot{Head: baseline, UnstagedPatch: patch}
	restored, err := Materialize(context.Background(), bundle, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Cleanup()
	got, err := os.ReadFile(filepath.Join(restored.Worktree, "delta.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("binary delta reconstruction differs")
	}
	if err := validateBinaryPatchLimits([]byte("GIT binary patch\nliteral 16777217\nA00000\n\n")); err == nil {
		t.Fatal("oversized literal accepted")
	}
	delta := append(deltaVarint(1), deltaVarint(MaxObjectBytes+1)...)
	encoded := encodeBinaryBlockForTest(delta)
	malicious := []byte(fmt.Sprintf("GIT binary patch\ndelta %d\n%s\n", len(delta), encoded))
	if err := validateBinaryPatchLimits(malicious); err == nil {
		t.Fatal("small delta with huge result accepted")
	}
}

func deltaVarint(value uint64) []byte {
	var result []byte
	for {
		next := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			next |= 0x80
		}
		result = append(result, next)
		if value == 0 {
			return result
		}
	}
}

func encodeBinaryBlockForTest(data []byte) string {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, _ = writer.Write(data)
	_ = writer.Close()
	var encoded strings.Builder
	input := compressed.Bytes()
	for len(input) > 0 {
		size := min(52, len(input))
		block := input[:size]
		input = input[size:]
		if size <= 26 {
			encoded.WriteByte(byte(size-1) + 'A')
		} else {
			encoded.WriteByte(byte(size-27) + 'a')
		}
		for offset := 0; offset < len(block); offset += 4 {
			var number uint32
			for index := 0; index < 4; index++ {
				number <<= 8
				if offset+index < len(block) {
					number |= uint32(block[offset+index])
				}
			}
			var chars [5]byte
			for index := 4; index >= 0; index-- {
				chars[index] = git85Alphabet[number%85]
				number /= 85
			}
			encoded.Write(chars[:])
		}
		encoded.WriteByte('\n')
	}
	return encoded.String()
}

func treeBundle(t *testing.T, entries map[string]GitObject) Bundle {
	t.Helper()
	paths := make([]string, 0, len(entries))
	for name := range entries {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	var tree bytes.Buffer
	objects := []GitObject{}
	seen := map[string]bool{}
	for _, name := range paths {
		blob := entries[name]
		if blob.Type == "" {
			blob.Type = "blob"
		}
		blob.ID = gitHash("sha1", blob.Type, blob.Data)
		if !seen[blob.ID] {
			objects = append(objects, blob)
			seen[blob.ID] = true
		}
		raw, _ := hex.DecodeString(blob.ID)
		fmt.Fprintf(&tree, "100644 %s\x00", name)
		tree.Write(raw)
	}
	treeObject := GitObject{Type: "tree", Data: tree.Bytes()}
	treeObject.ID = gitHash("sha1", "tree", treeObject.Data)
	commit := GitObject{Type: "commit", Data: []byte(fmt.Sprintf("tree %s\nauthor Test <test@example.invalid> 1 +0000\ncommitter Test <test@example.invalid> 1 +0000\n\nfixture\n", treeObject.ID))}
	commit.ID = gitHash("sha1", "commit", commit.Data)
	objects = append(objects, treeObject, commit)
	return Bundle{Format: Format, Version: Version, CreatedAt: time.Now().UTC(), Title: "hostile fixture", Origin: Origin{ThreadID: "source", RoundID: "round"}, Baseline: commit.ID, ObjectFormat: "sha1", GitObjects: objects, Snapshot: gitstate.Snapshot{Head: commit.ID}}
}

func TestBaselineCaseUnicodeCollisionsAndCheckoutExpansionRejected(t *testing.T) {
	cases := map[string]map[string]GitObject{
		"case":    {"Foo": {Data: []byte("first")}, "foo": {Data: []byte("second")}},
		"Unicode": {"caf\u00e9": {Data: []byte("first")}, "cafe\u0301": {Data: []byte("second")}},
	}
	large := GitObject{Data: bytes.Repeat([]byte("a"), 1<<20)}
	expansion := map[string]GitObject{}
	for index := 0; index < 65; index++ {
		expansion[fmt.Sprintf("%03d.bin", index)] = large
	}
	cases["repeated blob expansion"] = expansion
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			bundle := treeBundle(t, entries)
			if err := bundle.Validate(); err != nil {
				t.Fatalf("fixture envelope should fit bound: %v", err)
			}
			parent := t.TempDir()
			if _, err := Materialize(context.Background(), bundle, parent); err == nil {
				t.Fatal("unsafe baseline accepted")
			}
			left, _ := os.ReadDir(parent)
			if len(left) != 0 {
				t.Fatal("failed materialization left repository")
			}
		})
	}
}

func TestChainedParentTraversalLinksRejected(t *testing.T) {
	for _, target := range []string{"x/y/../../outside", "../sibling", "dir/../safe"} {
		if err := ValidateSymlink("link", target); err == nil {
			t.Fatalf("parent traversing target accepted: %q", target)
		}
	}
	bundle, repo := testBundle(t)
	if err := os.Mkdir(filepath.Join(repo, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".", filepath.Join(repo, "x", "y")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("x/y/../../outside", filepath.Join(repo, "b")); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", "x/y", "b")
	gitTest(t, repo, "commit", "-m", "unsafe links")
	baseline := strings.TrimSpace(string(gitTest(t, repo, "rev-parse", "HEAD")))
	format, objects, err := CaptureBaseline(context.Background(), repo, baseline)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Baseline, bundle.ObjectFormat, bundle.GitObjects = baseline, format, objects
	bundle.Snapshot = gitstate.Snapshot{Head: baseline}
	parent := t.TempDir()
	sentinel := filepath.Join(parent, "outside")
	writeTestFile(t, parent, "outside", []byte("untouched"))
	if _, err := Materialize(context.Background(), bundle, parent); err == nil {
		t.Fatal("baseline chained escape accepted")
	}
	got, _ := os.ReadFile(sentinel)
	if string(got) != "untouched" {
		t.Fatal("outside sentinel changed")
	}
}
