package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	"teamcross/internal/domain"
	"teamcross/internal/gitstate"
	"teamcross/internal/transfer"
)

type sealedCodeLine struct {
	Kind    string `json:"kind"`
	Text    string `json:"text"`
	OldPath string `json:"oldPath,omitempty"`
	NewPath string `json:"newPath,omitempty"`
	OldLine int    `json:"oldLine,omitempty"`
	NewLine int    `json:"newLine,omitempty"`
}

type sealedCodeView struct {
	RoundID  string           `json:"roundId"`
	Baseline string           `json:"baseline"`
	Patch    string           `json:"patch"`
	Lines    []sealedCodeLine `json:"lines"`
}

var reviewHunk = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

func validReviewPath(value string) bool {
	if value == "" || value == "." || path.IsAbs(value) || path.Clean(value) != value || strings.ContainsAny(value, "\x00\r\n\\") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || strings.EqualFold(part, ".git") {
			return false
		}
	}
	return true
}

func reviewHeaderPath(value, prefix string) (string, error) {
	if value == "/dev/null" {
		return "", nil
	}
	var err error
	if strings.HasPrefix(value, `"`) {
		value, err = strconv.Unquote(value)
	} else {
		// Git appends a separator to unquoted paths containing spaces.
		value = strings.TrimSuffix(value, "\t")
	}
	if err != nil || !strings.HasPrefix(value, prefix) || !validReviewPath(strings.TrimPrefix(value, prefix)) {
		return "", errors.New("unsupported code-review path")
	}
	return strings.TrimPrefix(value, prefix), nil
}

// Parse only a generated, sealed Git patch. Both annotation validation and the
// UI consume these exact sides/paths; raw patch parsing cannot grant an anchor.
func parseSealedCode(patch string) ([]sealedCodeLine, error) {
	if len(patch) > 8<<20 || strings.Count(patch, "\n") > 100000 {
		return nil, errors.New("sealed code exceeds the review display budget; use the authorized patch download")
	}
	lines := []sealedCodeLine{}
	oldPath, newPath := "", ""
	oldLine, newLine, oldLeft, newLeft := 0, 0, 0, 0
	for _, text := range strings.Split(patch, "\n") {
		line := sealedCodeLine{Kind: "meta", Text: text}
		if oldLeft > 0 || newLeft > 0 {
			line.OldPath, line.NewPath = oldPath, newPath
			switch {
			case strings.HasPrefix(text, `\ No newline`):
			case strings.HasPrefix(text, " ") && oldLeft > 0 && newLeft > 0:
				line.Kind, line.OldLine, line.NewLine = "context", oldLine, newLine
				oldLine, newLine, oldLeft, newLeft = oldLine+1, newLine+1, oldLeft-1, newLeft-1
			case strings.HasPrefix(text, "-") && oldLeft > 0:
				line.Kind, line.OldLine = "delete", oldLine
				oldLine, oldLeft = oldLine+1, oldLeft-1
			case strings.HasPrefix(text, "+") && newLeft > 0:
				line.Kind, line.NewLine = "add", newLine
				newLine, newLeft = newLine+1, newLeft-1
			default:
				return nil, errors.New("invalid sealed diff hunk")
			}
		} else {
			switch {
			case strings.HasPrefix(text, "diff --git "):
				oldPath, newPath = "", ""
			case strings.HasPrefix(text, "--- "):
				var err error
				oldPath, err = reviewHeaderPath(strings.TrimPrefix(text, "--- "), "a/")
				if err != nil {
					return nil, err
				}
			case strings.HasPrefix(text, "+++ "):
				var err error
				newPath, err = reviewHeaderPath(strings.TrimPrefix(text, "+++ "), "b/")
				if err != nil {
					return nil, err
				}
			case strings.HasPrefix(text, "@@"):
				match := reviewHunk.FindStringSubmatch(text)
				if match == nil {
					return nil, errors.New("unsupported sealed diff hunk")
				}
				values := []int{0, 1, 0, 1}
				for i, raw := range match[1:] {
					if raw == "" {
						continue
					}
					value, err := strconv.Atoi(raw)
					if err != nil || value > 1000000000 {
						return nil, errors.New("invalid sealed diff line number")
					}
					values[i] = value
				}
				oldLine, oldLeft, newLine, newLeft = values[0], values[1], values[2], values[3]
				if (oldLeft > 0 && (oldPath == "" || oldLine < 1)) || (newLeft > 0 && (newPath == "" || newLine < 1)) {
					return nil, errors.New("sealed diff line has no source path")
				}
			}
			line.OldPath, line.NewPath = oldPath, newPath
		}
		lines = append(lines, line)
	}
	if oldLeft != 0 || newLeft != 0 {
		return nil, errors.New("incomplete sealed diff hunk")
	}
	return lines, nil
}

func (app *App) readRoundCode(ctx context.Context, threadID, roundID string, identity access) (sealedCodeView, error) {
	view := sealedCodeView{RoundID: roundID}
	if identity.Mode == "share" {
		if err := app.requireCurrentShare(ctx, identity.ShareID, threadID); err != nil {
			return view, err
		}
		p, err := app.loadShareProjection(ctx, identity.ShareID)
		if err != nil {
			return view, err
		}
		// Legacy shares have no sealed membership. Do not manufacture one from
		// their current worktree or grant every historical Round.
		allowed := false
		for _, round := range p.Detail.Rounds {
			allowed = allowed || round.ID == roundID
		}
		if p.Legacy || !p.Scope.IncludeCode || !allowed || p.Detail.ID != threadID {
			return view, domain.ErrNotFound
		}
		view.Baseline, view.Patch = p.Detail.Git.Head, p.Detail.Git.FinalPatch
	} else {
		bundle, err := app.buildOfflineBundle(ctx, threadID, roundID, nil, nil)
		if err != nil {
			return view, err
		}
		restored, err := transfer.Materialize(ctx, bundle, app.paths.Worktrees)
		if err != nil {
			return view, err
		}
		defer restored.Cleanup()
		patch, err := gitstate.ExportSnapshotPatch(ctx, restored.Worktree, bundle.Snapshot)
		if err != nil {
			return view, err
		}
		view.Baseline, view.Patch = bundle.Baseline, string(patch)
	}
	var err error
	view.Lines, err = parseSealedCode(view.Patch)
	return view, err
}

func (app *App) handleGetRoundCode(w http.ResponseWriter, r *http.Request) {
	threadID, ok := app.authorizeThread(w, r)
	if !ok {
		return
	}
	view, err := app.readRoundCode(r.Context(), threadID, r.PathValue("roundID"), accessFrom(r))
	if err == nil && accessFrom(r).Mode == "share" {
		err = app.requireCurrentShare(r.Context(), accessFrom(r).ShareID, threadID)
	}
	if err != nil {
		status := http.StatusUnprocessableEntity
		if accessFrom(r).Mode == "share" || errors.Is(err, domain.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, "sealed_code_unavailable", "封存代码不可用或超出审阅范围；不会使用当前 worktree 替代。")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, view)
}

func (view sealedCodeView) anchorIndex(file, side string, number int) (int, error) {
	for i, line := range view.Lines {
		if (side == "old" && line.OldPath == file && line.OldLine == number) || (side == "new" && line.NewPath == file && line.NewLine == number) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("code anchor is not a displayed line in the selected sealed Round")
}
