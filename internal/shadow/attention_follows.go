package shadow

import (
	"fmt"
	"time"

	"teamcross/internal/spacecontext"
)

// Follows is only a mirror of saved member preferences for already-received
// work. Users still manage a single follow relationship on the space host.
func acceptFollow(next *AttentionRecord, signal spacecontext.Signal) error {
	if next.Follows == nil {
		next.Follows = map[string]spacecontext.FollowSetting{}
	}
	change := signal.Follow
	if signal.Kind != "follow_changed" || change.Revision == 0 || (change.Mode != "following" && change.Mode != "direct_only" && change.Mode != "muted") || len(change.Baseline) > 9 {
		return fmt.Errorf("关注变更信号无效")
	}
	for _, ref := range change.Baseline {
		if ref.SpaceID != next.SpaceID {
			return fmt.Errorf("关注初始化来源不属于本空间")
		}
	}
	put := func(setting spacecontext.FollowSetting) error {
		key := attentionHash(setting.Subject)
		old, ok := next.Follows[key]
		if ok && old.Revision == setting.Revision && attentionHash(old) != attentionHash(setting) {
			return fmt.Errorf("同一关注版本对应不同设置")
		}
		if !ok || setting.Revision > old.Revision {
			next.Follows[key] = setting
		}
		return nil
	}
	if err := put(spacecontext.FollowSetting{Subject: signal.Subject, Mode: change.Mode, Revision: change.Revision}); err != nil {
		return err
	}
	if change.Parent != nil && change.Parent.Revision > 0 {
		if err := put(*change.Parent); err != nil {
			return err
		}
	}
	if change.Mode != "following" || next.Follows[attentionHash(signal.Subject)].Revision != change.Revision {
		event := next.Events[signal.ID]
		event.State = "control"
		next.Events[signal.ID] = event
	}
	return nil
}

func attentionEligible(record AttentionRecord, signal spacecontext.Signal) bool {
	setting, exists := record.Follows[attentionHash(signal.Subject)]
	parent := spacecontext.Subject{}
	if signal.Subject.Kind == "discussion" {
		if signal.Source.MaterialID != "" {
			parent = spacecontext.Subject{Kind: "material", ID: signal.Source.MaterialID}
		} else if signal.Source.ExecutionID != "" {
			parent = spacecontext.Subject{Kind: "execution", ID: signal.Source.ExecutionID}
		}
	}
	ps, hasParent := record.Follows[attentionHash(parent)]
	if hasParent && ps.Mode == "muted" {
		return false
	}
	if signal.Follow != nil && (signal.Follow.Mode != "following" || !exists || setting.Revision != signal.Follow.Revision) {
		return false
	}
	if !exists && hasParent {
		setting, exists = ps, true
	}
	if !exists {
		// Old preferences are not eagerly imported. The host already matched
		// this event; a later saved change will supply an exact control signal.
		return true
	}
	return setting.Mode == "following" || setting.Mode == "direct_only" && signalDirect(signal)
}

func batchFollowEligible(record AttentionRecord, batch ObservationBatch) bool {
	if c := record.Continuations[batch.ID]; c.HandlingID != "" {
		return continuationFollowEligible(record, c)
	}
	for _, id := range batch.EventIDs {
		if !attentionEligible(record, record.Events[id].Signal) {
			return false
		}
	}
	return true
}

func continuationFollowEligible(record AttentionRecord, c AttentionContinuation) bool {
	if c.HandlingID != "" {
		h := record.Handling[c.HandlingID]
		return h.Handler == "shadow" && h.Revision == c.HandlingRevision
	}
	for _, id := range c.EventIDs {
		if !attentionEligible(record, record.Events[id].Signal) {
			return false
		}
	}
	return true
}

func reconcileFollowBatches(next *AttentionRecord) {
	for _, batch := range next.Batches {
		if batch.State == "preparing" && !batchFollowEligible(*next, batch) {
			resetBatch(next, batch, "superseded", "", time.Time{})
		}
	}
	for id, c := range next.Continuations {
		if c.State == "queued" && !continuationFollowEligible(*next, c) {
			c.State, c.UpdatedAt = "superseded", time.Now()
			next.Continuations[id] = c
			if c.Mode == "direct" {
				resetBatch(next, next.Batches[c.BatchID], "superseded", "", time.Time{})
			}
		}
	}
}
