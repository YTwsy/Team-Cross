// Package transfer implements bounded, self-contained offline Thread forks.
// Bundles are data, never an instruction to launch a Provider or apply to a user checkout.
package transfer

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"teamcross/internal/gitstate"
)

const (
	Format               = "teamcross.offline-fork"
	Version              = 1
	MaxJSONBytes   int64 = 100 << 20
	MaxTotalBytes        = 64 << 20
	MaxObjectBytes       = 16 << 20
	MaxObjects           = 10000
)

type Origin struct {
	ThreadID    string `json:"threadId"`
	RoundID     string `json:"roundId"`
	RoundNumber int64  `json:"roundNumber"`
}

type GitObject struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data []byte `json:"data"`
}

type Object struct {
	Hash string `json:"hash"`
	Data []byte `json:"data"`
}

type Evidence struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	ObjectHash string `json:"objectHash"`
}

// SnapshotRecord is deliberately opaque to the Git transport. Core validates
// its versioned domain schema before persisting it, after envelope validation.
type SnapshotRecord struct {
	ID         string `json:"id"`
	ObjectHash string `json:"objectHash"`
}

type Context struct {
	Goal      string `json:"goal,omitempty"`
	Progress  string `json:"progress,omitempty"`
	Blocker   string `json:"blocker,omitempty"`
	Tried     string `json:"tried,omitempty"`
	Questions string `json:"questions,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

type Bundle struct {
	Format           string            `json:"format"`
	Version          int               `json:"version"`
	CreatedAt        time.Time         `json:"createdAt"`
	Title            string            `json:"title"`
	Origin           Origin            `json:"origin"`
	Baseline         string            `json:"baseline"`
	ObjectFormat     string            `json:"objectFormat"`
	GitObjects       []GitObject       `json:"gitObjects"`
	Snapshot         gitstate.Snapshot `json:"snapshot"`
	Context          Context           `json:"context"`
	Evidence         []Evidence        `json:"evidence"`
	SessionSnapshots []SnapshotRecord  `json:"sessionSnapshots"`
	Objects          []Object          `json:"objects"`
}

func Decode(reader io.Reader) (Bundle, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxJSONBytes+1))
	if err != nil {
		return Bundle{}, err
	}
	if int64(len(data)) > MaxJSONBytes {
		return Bundle{}, errors.New("bundle exceeds 100 MiB JSON limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var bundle Bundle
	if err := decoder.Decode(&bundle); err != nil {
		return Bundle{}, fmt.Errorf("decode bundle: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Bundle{}, errors.New("bundle must contain exactly one JSON value")
	}
	if err := bundle.Validate(); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func gitHash(format, kind string, data []byte) string {
	header := []byte(fmt.Sprintf("%s %d\x00", kind, len(data)))
	if format == "sha256" {
		hash := sha256.New()
		_, _ = hash.Write(header)
		_, _ = hash.Write(data)
		return hex.EncodeToString(hash.Sum(nil))
	}
	hash := sha1.New()
	_, _ = hash.Write(header)
	_, _ = hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

func (bundle Bundle) Validate() error {
	if bundle.Format != Format || bundle.Version != Version {
		return errors.New("unsupported offline bundle format or version")
	}
	if bundle.ObjectFormat != "sha1" && bundle.ObjectFormat != "sha256" {
		return errors.New("unsupported Git object format")
	}
	if bundle.Origin.ThreadID == "" || bundle.Origin.RoundID == "" || bundle.Origin.RoundNumber < 0 {
		return errors.New("bundle requires source Thread and Round provenance")
	}
	if len(bundle.Title) > 4096 || len(bundle.Origin.ThreadID) > 256 || len(bundle.Origin.RoundID) > 256 {
		return errors.New("bundle metadata exceeds limits")
	}
	if len(bundle.GitObjects) == 0 || len(bundle.GitObjects)+len(bundle.Objects) > MaxObjects {
		return errors.New("bundle object count exceeds limits or baseline is missing")
	}
	if len(bundle.Evidence) > MaxObjects || len(bundle.SessionSnapshots) > MaxObjects || len(bundle.Snapshot.Untracked) > MaxObjects {
		return errors.New("bundle reference count exceeds limits")
	}
	for _, patch := range [][]byte{bundle.Snapshot.StagedPatch, bundle.Snapshot.UnstagedPatch} {
		if err := validateBinaryPatchLimits(patch); err != nil {
			return err
		}
	}
	if bundle.Snapshot.RepoRoot != "" || bundle.Snapshot.Branch != "" || bundle.Snapshot.Unborn || bundle.Snapshot.Head != bundle.Baseline || len(bundle.Snapshot.Status) != 0 {
		return errors.New("portable snapshot must contain only its fixed baseline and captured changes")
	}
	total := len(bundle.Snapshot.StagedPatch) + len(bundle.Snapshot.UnstagedPatch)
	gitIDs := make(map[string]string, len(bundle.GitObjects))
	for _, object := range bundle.GitObjects {
		if object.Type != "commit" && object.Type != "tree" && object.Type != "blob" && object.Type != "tag" {
			return errors.New("unsupported Git object type")
		}
		if len(object.Data) > MaxObjectBytes || object.ID != gitHash(bundle.ObjectFormat, object.Type, object.Data) {
			return errors.New("Git object hash mismatch or object size limit exceeded")
		}
		if _, exists := gitIDs[object.ID]; exists {
			return errors.New("duplicate Git object")
		}
		gitIDs[object.ID] = object.Type
		total += len(object.Data)
	}
	if gitIDs[bundle.Baseline] != "commit" {
		return errors.New("baseline commit object is missing")
	}
	objects := make(map[string]struct{}, len(bundle.Objects))
	for _, object := range bundle.Objects {
		if len(object.Data) > MaxObjectBytes || object.Hash != ContentHash(object.Data) {
			return errors.New("content hash mismatch or object size limit exceeded")
		}
		if _, exists := objects[object.Hash]; exists {
			return errors.New("duplicate content object")
		}
		objects[object.Hash] = struct{}{}
		total += len(object.Data)
	}
	ids := map[string]bool{}
	referenced := map[string]bool{}
	for _, item := range bundle.Evidence {
		if item.ID == "" || ids["e:"+item.ID] {
			return errors.New("missing or duplicate evidence identity")
		}
		ids["e:"+item.ID] = true
		if _, ok := objects[item.ObjectHash]; !ok {
			return errors.New("missing evidence object")
		}
		referenced[item.ObjectHash] = true
	}
	for _, item := range bundle.SessionSnapshots {
		if item.ID == "" || ids["s:"+item.ID] {
			return errors.New("missing or duplicate Session snapshot identity")
		}
		ids["s:"+item.ID] = true
		if _, ok := objects[item.ObjectHash]; !ok {
			return errors.New("missing Session snapshot object")
		}
		referenced[item.ObjectHash] = true
	}
	if len(referenced) != len(objects) {
		return errors.New("bundle contains unselected content objects")
	}
	var untrackedTotal int64
	paths := map[string]bool{}
	for _, file := range bundle.Snapshot.Untracked {
		if err := ValidatePath(file.Path); err != nil {
			return err
		}
		key := portablePathKey(file.Path)
		if paths[key] {
			return errors.New("duplicate or case-colliding untracked path")
		}
		paths[key] = true
		if !file.Included {
			if len(file.Content) > 0 {
				return errors.New("omitted file must not carry content")
			}
			continue
		}
		if file.Size != int64(len(file.Content)) || file.SHA256 != ContentHash(file.Content) {
			return errors.New("untracked size or hash mismatch")
		}
		if file.Size > gitstate.DefaultMaxUntrackedFileBytes {
			return errors.New("untracked file exceeds 5 MiB limit")
		}
		untrackedTotal += file.Size
		if file.Symlink {
			if err := ValidateSymlink(file.Path, string(file.Content)); err != nil {
				return err
			}
		}
		total += len(file.Content)
	}
	if untrackedTotal > gitstate.DefaultMaxUntrackedTotalBytes || total > MaxTotalBytes {
		return errors.New("bundle exceeds decoded content limits")
	}
	return nil
}

func ValidatePath(value string) error {
	if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\\\x00") || path.IsAbs(value) || path.Clean(value) != value || value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("unsafe bundle path: %q", value)
	}
	for _, component := range strings.Split(value, "/") {
		if strings.EqualFold(component, ".git") || strings.Contains(component, ":") {
			return fmt.Errorf("unsafe Git metadata path: %q", value)
		}
	}
	return nil
}

func ValidateSymlink(file, target string) error {
	if target == "" || path.IsAbs(target) || strings.ContainsAny(target, "\\\x00") {
		return fmt.Errorf("unsafe symlink in bundle: %q", file)
	}
	// Lexical cleaning is insufficient when an earlier component is itself a
	// symlink: substitution changes the meaning of later '..' components.
	for _, component := range strings.Split(target, "/") {
		if component == ".." {
			return fmt.Errorf("parent-traversing symlink is unsupported in offline bundles: %q", file)
		}
	}
	return ValidatePath(path.Clean(path.Join(path.Dir(file), target)))
}

func portablePathKey(value string) string { return cases.Fold().String(norm.NFC.String(value)) }

// Preserve distinct names across default case-insensitive, normalizing macOS
// filesystems; silently collapsing a Git tree is not a valid snapshot restore.
func registerPortablePath(paths map[string]string, filename string, directory bool) error {
	parts := strings.Split(filename, "/")
	for index := range parts {
		name := strings.Join(parts[:index+1], "/")
		kind := "d:"
		if index == len(parts)-1 && !directory {
			kind = "f:"
		}
		key := portablePathKey(name)
		if previous, exists := paths[key]; exists && previous != kind+name {
			return fmt.Errorf("case, Unicode or file/directory path collision: %q", filename)
		}
		paths[key] = kind + name
	}
	return nil
}
