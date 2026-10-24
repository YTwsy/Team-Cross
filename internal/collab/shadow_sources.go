package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"teamcross/internal/readview"
	"teamcross/internal/shadow"
)

func (a *App) readShadowSource(ctx context.Context, shadowID string, ref SpaceSourceRef) (shadow.SourceSnapshot, error) {
	if err := a.checkShadowSource(ctx, shadowID, ref); err != nil {
		return shadow.SourceSnapshot{}, err
	}
	return a.readAttentionSource(ctx, ref)
}

func (a *App) checkShadowSource(ctx context.Context, shadowID string, ref SpaceSourceRef) error {
	a.mu.Lock()
	e := a.shadows[shadowID]
	a.mu.Unlock()
	if e == nil {
		return fmt.Errorf("分身运行记录不存在")
	}
	_, spaceID, _, _ := e.Identity()
	if ref.SpaceID != spaceID {
		return fmt.Errorf("来源不属于本分身空间")
	}
	actor, err := a.shadowCall(ctx, spaceID, "check", shadowInput{})
	if err != nil {
		return err
	}
	if workbenchDecode[shadow.Actor](actor).ID != e.View().Owner.ID {
		return fmt.Errorf("分身所属成员已变化")
	}
	return nil
}

func (a *App) readAttentionSource(ctx context.Context, ref SpaceSourceRef) (shadow.SourceSnapshot, error) {
	return a.readAttentionSourceLimit(ctx, ref, 65536)
}

func (a *App) readAttentionSourceLimit(ctx context.Context, ref SpaceSourceRef, maxBytes int) (shadow.SourceSnapshot, error) {
	result := shadow.SourceSnapshot{Source: ref, ObservedAt: time.Now()}
	if ref.SpaceID == "" {
		return result, fmt.Errorf("读取来源需要空间身份")
	}
	if ref.IsShadow() {
		if err := ref.ValidateShadow(); err != nil {
			return result, err
		}
	}
	if (ref.ReplyID != "" && ref.AnnotationID == "") || ref.Version < 0 || (ref.Version > 0 && ref.MaterialID == "") || (ref.TurnID != "" && ref.ExecutionID == "") {
		return result, fmt.Errorf("来源定位不完整")
	}
	if ref.BriefRevision < 0 || (ref.BriefItemID != "" && ref.BriefRevision == 0) || (ref.BriefRevision > 0 && (ref.MaterialID != "" || ref.ExecutionID != "" || ref.AnnotationID != "" || ref.RequestID != "")) {
		return result, fmt.Errorf("简报来源需要独立且准确的版本")
	}
	address := ref.SpaceID
	if ref.ExecutionID != "" {
		address = executionAddress(ref.SpaceID, ref.ExecutionID)
	}
	var material map[string]any
	if ref.MaterialID != "" {
		out, err := a.target(ctx, ref.SpaceID, "GET", "materials", nil)
		if err != nil {
			return result, err
		}
		directory := workbenchDecode[[]map[string]any](out)
		for _, entry := range directory {
			if entry["id"] == ref.MaterialID {
				material = entry
				break
			}
		}
		if material == nil || material["withdrawnAt"] != nil {
			result.Latest, result.Unavailable = true, "material_withdrawn"
			if material == nil {
				result.Unavailable = "material_missing"
			}
			// A later withdrawal must not be blocked behind the queued old
			// publication forever. Deliver a tombstone, never the old body.
			result.Data, _ = json.Marshal(map[string]any{"materialId": ref.MaterialID, "unavailable": result.Unavailable})
			return result, nil
		}
	}
	var out any
	var err error
	switch {
	case ref.ShadowID != "":
		out, err = a.shadowCall(ctx, ref.SpaceID, "source", shadowInput{ID: ref.ShadowID, MessageID: ref.ShadowMessageID, ResultID: ref.ShadowResultID})
		result.Unavailable = workbenchDecode[ShadowSourceResult](out).Unavailable
	case ref.BriefRevision > 0:
		var read any
		read, err = a.workbenchCall(ctx, ref.SpaceID, "brief-read", workbenchInput{Revision: ref.BriefRevision, ItemID: ref.BriefItemID})
		brief := workbenchDecode[BriefResult](read)
		result.Unavailable = brief.Unavailable
		if result.Unavailable != "" {
			out = map[string]any{"source": ref, "unavailable": result.Unavailable}
		} else if brief.Item != nil {
			out = brief.Item
		} else {
			out = brief.Brief
		}
	case ref.AnnotationID != "":
		q := url.Values{"kind": {"annotations"}, "compact": {"true"}, "annotationId": {ref.AnnotationID}, "sourceOnly": {"true"}, "maxBytes": {strconv.Itoa(maxBytes)}}
		if ref.ReplyID != "" {
			q.Set("replyId", ref.ReplyID)
		}
		out, err = a.target(ctx, address, "GET", "context?"+q.Encode(), nil)
	case ref.MaterialID != "" && ref.Version > 0:
		out, err = a.target(ctx, ref.SpaceID, "POST", "read-material", MaterialRead{MaterialID: ref.MaterialID, Version: ref.Version, IncludeOutline: true, Options: readview.Options{View: "answers", MaxBytes: maxBytes}})
	case ref.MaterialID != "":
		out, result.Latest = material, true
	case ref.RequestID != "":
		out, err = a.workbenchCall(ctx, ref.SpaceID, "request", workbenchInput{RequestID: ref.RequestID})
		result.Latest = true
	case ref.ExecutionID != "" && ref.TurnID != "":
		q := url.Values{"kind": {"history"}, "turnId": {ref.TurnID}, "view": {"answers"}, "maxBytes": {strconv.Itoa(maxBytes)}}
		out, err = a.target(ctx, address, "GET", "context?"+q.Encode(), nil)
	case ref.ExecutionID != "":
		return result, fmt.Errorf("会话事件需要准确 turnId")
	default:
		var view any
		view, err = a.workbenchCall(ctx, ref.SpaceID, "view", workbenchInput{})
		out = workbenchDecode[WorkbenchView](view).Brief
		result.Latest = true
	}
	if err != nil {
		return result, err
	}
	// Joined Core IDs are local aliases; remap nested public references before
	// the model sees them, just as the consumer metadata route does.
	a.mu.Lock()
	j := a.joined[ref.SpaceID]
	a.mu.Unlock()
	if j != nil {
		j.mu.Lock()
		hostID := j.Invitation.ID
		j.mu.Unlock()
		out = remapWorkbench(workbenchDecode[any](out), hostID, ref.SpaceID)
	}
	result.Data, err = json.Marshal(out)
	if err != nil || len(result.Data) > maxBytes {
		return result, fmt.Errorf("来源正文超过单批读取预算")
	}
	var next struct {
		NextCursor string `json:"nextCursor"`
	}
	_ = json.Unmarshal(result.Data, &next)
	result.Partial = next.NextCursor != ""
	return result, nil
}
