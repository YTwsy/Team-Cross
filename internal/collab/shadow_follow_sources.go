package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"teamcross/internal/shadow"
	"teamcross/internal/spacecontext"
)

func (a *App) ensureShadowFollow(ctx context.Context, id string, subject spacecontext.Subject, requestID string) (spacecontext.FollowSetting, error) {
	a.mu.Lock()
	e := a.shadows[id]
	a.mu.Unlock()
	if e == nil {
		return spacecontext.FollowSetting{}, fmt.Errorf("分身运行记录不存在")
	}
	_, spaceID, _, _ := e.Identity()
	if err := a.checkShadowSource(ctx, id, SpaceSourceRef{SpaceID: spaceID}); err != nil {
		return spacecontext.FollowSetting{}, err
	}
	out, err := a.memberActivityCall(ctx, spaceID, "ensure-follow", ActivityInput{Subject: subject, RequestID: requestID})
	if err != nil {
		return spacecontext.FollowSetting{}, err
	}
	p := workbenchDecode[MemberFollowPreference](out)
	return spacecontext.FollowSetting{Subject: p.Subject, Mode: p.Mode, Revision: p.Revision}, nil
}

func (a *App) readShadowFollowSource(ctx context.Context, id string, signal spacecontext.Signal) (shadow.SourceSnapshot, error) {
	if err := a.checkShadowSource(ctx, id, signal.Source); err != nil {
		return shadow.SourceSnapshot{}, err
	}
	return a.readFollowSource(ctx, signal)
}

func (a *App) readFollowSource(ctx context.Context, signal spacecontext.Signal) (shadow.SourceSnapshot, error) {
	result := shadow.SourceSnapshot{Source: signal.Source, ObservedAt: time.Now()}
	change := signal.Follow
	if change == nil || change.Mode != "following" || len(change.Baseline) > 9 {
		return result, fmt.Errorf("关注初始化来源无效")
	}
	result.Partial = change.Partial
	parts := []shadow.SourceSnapshot{}
	for _, ref := range change.Baseline {
		if ref.SpaceID != signal.Source.SpaceID {
			return result, fmt.Errorf("关注初始化来源不属于本空间")
		}
		part, err := a.readAttentionSourceLimit(ctx, ref, 32768)
		if err != nil {
			return result, err
		}
		if part.Unavailable != "" {
			part.Source = signal.Source
			return part, nil
		}
		candidate := append(parts, part)
		data, _ := json.Marshal(candidate)
		if len(data) > 60<<10 {
			result.Partial = true
			break
		}
		parts = candidate
		result.References = append(result.References, ref)
		result.Partial = result.Partial || part.Partial
	}
	if len(parts) == 0 && (signal.Subject.Kind != "execution" || signal.Source.TurnID != "" || len(change.Baseline) != 0) {
		return result, fmt.Errorf("关注对象缺少初始化来源")
	}
	result.Data, _ = json.Marshal(map[string]any{
		"purpose": "follow_baseline", "capturedAt": signal.CreatedAt,
		"scope":   "fixed_material_version_or_discussion_root_and_up_to_eight_recent_replies_or_latest_completed_turn",
		"sources": parts, "noCompletedTurnAtFollow": len(parts) == 0,
	})
	return result, nil
}
