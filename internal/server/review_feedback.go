package server

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"teamcross/internal/domain"
)

// Fences treat quoted content as data even when it contains Markdown/HTML or
// closing fences. Limit both size and controls; no format is rendered/executed.
func feedbackExcerpt(out *strings.Builder, value string) {
	quote := boundedFeedbackText(value, 2000)
	fence := "```"
	for strings.Contains(quote, fence) {
		fence += "`"
	}
	fmt.Fprintf(out, "\n%stext\n%s\n%s\n", fence, quote, fence)
}

func boundedFeedbackText(value string, limit int) string {
	var out strings.Builder
	count := 0
	for _, r := range value {
		if count == limit {
			out.WriteString("…（引用截断）")
			break
		}
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			r = '\uFFFD'
		}
		out.WriteRune(r)
		count++
	}
	return out.String()
}

func (app *App) reviewFeedback(ctx context.Context, detail threadDetail, identity access) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# %s — 审阅反馈\n\nThread: %s\n\n以下内容为人工意见及引用，不会自动提交给 Agent。\n", detail.Title, detail.ID)
	codes := map[string]sealedCodeView{}
	for _, annotation := range detail.Annotations {
		fmt.Fprintf(&out, "\n## %s · %s\n\n%s\n", annotation.Author, annotation.CreatedAt.Format(time.RFC3339), annotation.Body)
		target := annotation.Target
		if annotation.File != "" && (target == nil || target.RoundID == "" || target.Side == "") {
			fmt.Fprintf(&out, "\n历史代码批注（未绑定不可变定位，未自动归锚）: %q:%d\n", annotation.File, annotation.Line)
			continue
		}
		if target == nil {
			continue
		}
		fmt.Fprintf(&out, "\n来源锚点: snapshot=%s entry=%s round=%s evidence=%s\n", target.SnapshotID, target.EntryID, target.RoundID, target.EvidenceID)
		if target.SnapshotID != "" {
			// Resolve the exact snapshot rather than whichever window is latest.
			var snapshot domain.SessionSnapshot
			var err error
			if identity.Mode == "share" {
				p, loadErr := app.loadShareProjection(ctx, identity.ShareID)
				err = loadErr
				if err == nil {
					snapshot, _, err = app.resolveSharedSnapshot(ctx, identity.ShareID, detail.ID, target.SnapshotID, p)
				}
			} else {
				snapshot, err = app.store.GetSessionSnapshot(ctx, detail.ID, target.SnapshotID)
			}
			if err != nil {
				out.WriteString("\n原快照引用不可用；未使用新窗口替代。\n")
				continue
			}
			fmt.Fprintf(&out, "\nProvider: %s; %s=%s; capturedAt=%s\n", snapshot.Source.Provider, snapshot.Source.IdentityKind, snapshot.Source.SessionID, snapshot.CapturedAt.Format(time.RFC3339))
			for _, entry := range snapshot.Entries {
				if entry.ID == target.EntryID {
					feedbackExcerpt(&out, entry.Text)
				}
			}
		}
		if target.EvidenceID != "" {
			app.appendEvidenceFeedback(ctx, &out, detail, identity, target.EvidenceID)
		}
		if annotation.File != "" {
			code, ok := codes[target.RoundID]
			if !ok {
				var err error
				code, err = app.readRoundCode(ctx, detail.ID, target.RoundID, identity)
				if err != nil {
					out.WriteString("\n封存代码引用不可用；未使用当前文件替代。\n")
					continue
				}
				codes[target.RoundID] = code
			}
			index, err := code.anchorIndex(annotation.File, target.Side, annotation.Line)
			if err != nil {
				out.WriteString("\n封存代码行引用不可用；未重新定位。\n")
				continue
			}
			fmt.Fprintf(&out, "\n代码: Round=%s baseline=%s side=%s path=%q line=%d\n", code.RoundID, code.Baseline, target.Side, annotation.File, annotation.Line)
			var excerpt strings.Builder
			for i := max(0, index-2); i < min(len(code.Lines), index+3); i++ {
				line := code.Lines[i]
				if (target.Side == "old" && line.OldPath == annotation.File && line.OldLine > 0) || (target.Side == "new" && line.NewPath == annotation.File && line.NewLine > 0) {
					number := line.NewLine
					if target.Side == "old" {
						number = line.OldLine
					}
					marker := " "
					if i == index {
						marker = ">"
					}
					// A huge neighbouring line cannot consume the entire quote
					// budget and hide the actual annotated line.
					fmt.Fprintf(&excerpt, "%s%d: %s\n", marker, number, boundedFeedbackText(line.Text[1:], 300))
				}
			}
			feedbackExcerpt(&out, excerpt.String())
		}
	}
	return out.String()
}

func (app *App) appendEvidenceFeedback(ctx context.Context, out *strings.Builder, detail threadDetail, identity access, id string) {
	var selected *evidenceView
	for i := range detail.Evidence {
		if detail.Evidence[i].ID == id {
			selected = &detail.Evidence[i]
			break
		}
	}
	if selected == nil {
		out.WriteString("\nEvidence 不在当前可引用范围内。\n")
		return
	}
	item, err := app.store.GetEvidence(ctx, detail.ID, id)
	if err == nil && identity.Mode == "share" {
		p, loadErr := app.loadShareProjection(ctx, identity.ShareID)
		if loadErr != nil || p.EvidenceHashes[id] == "" || app.requireCurrentShare(ctx, identity.ShareID, detail.ID) != nil {
			out.WriteString("\nEvidence 不在当前 Share 的授权范围内。\n")
			return
		}
		// Use only the exact content hash frozen when this Share was published.
		item.ObjectHash = p.EvidenceHashes[id]
	}
	if err != nil {
		out.WriteString("\nEvidence 原引用不可用。\n")
		return
	}
	data, err := app.store.GetObject(ctx, item.ObjectHash)
	if err != nil {
		out.WriteString("\nEvidence 原内容不可用。\n")
		return
	}
	contentType := selected.MIMEType
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	baseType, _, _ := mime.ParseMediaType(contentType)
	// Keep the content identity intact even if a very long label is truncated.
	fmt.Fprintf(out, "\nEvidence ID=%s sha256=%s bytes=%d\n", id, item.ObjectHash, len(data))
	metadata, _ := json.Marshal(map[string]any{"id": id, "name": selected.Name, "kind": selected.Kind, "mimeType": contentType, "sha256": item.ObjectHash, "bytes": len(data)})
	feedbackExcerpt(out, "Evidence: "+string(metadata))
	if utf8.Valid(data) && !strings.ContainsRune(string(data), '\x00') && (strings.HasPrefix(baseType, "text/") || baseType == "application/json" || baseType == "application/xml") {
		feedbackExcerpt(out, string(data))
	} else {
		out.WriteString("\n非文本 Evidence：仅引用元数据与内容哈希，不展开二进制内容。\n")
	}
}
