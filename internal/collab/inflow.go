package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/inflow"
	"teamcross/internal/problem"
)

// These preferences and delivery versions belong to a public space target or
// execution, never to a guessed cwd, member's other sessions, or MCP connection.
type InFlowPreference struct {
	Enabled   bool           `json:"enabled"`
	Since     time.Time      `json:"since"`
	Delivered map[string]int `json:"delivered,omitempty"`
}

type SpaceNote struct {
	ID          string     `json:"id"`
	TargetID    string     `json:"targetId"`
	Actor       SpaceActor `json:"actor"`
	Text        string     `json:"text"`
	CreatedAt   time.Time  `json:"createdAt"`
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`
}

type inFlowLease struct {
	Recipient string
	Expires   time.Time
	Items     []inflow.Item
}

func inFlowRecipient(targetID, executionID string) string {
	if executionID != "" {
		return "execution:" + executionID
	}
	return "target:" + targetID
}

// Called under the shared space/execution mutex and workbench save transaction.
func (s *Session) inFlowOperationLocked(ctx context.Context, op string, in workbenchInput, actor SpaceActor) (any, bool, error) {
	b := s.record.Workbench
	if b.InFlow == nil {
		b.InFlow = map[string]InFlowPreference{}
	}
	if b.Notes == nil {
		b.Notes = map[string]SpaceNote{}
	}
	now := time.Now()
	if op == "inflow-note" || op == "inflow-note-read" {
		if op == "inflow-note-read" {
			note, ok := b.Notes[in.RequestID]
			if !ok {
				return nil, false, problem.New("inflow_note_not_found", "没有找到空间留言", "可使用原标识核对或明确重试保存")
			}
			return note, false, nil
		}
		text := strings.TrimSpace(in.Instruction)
		if _, err := uuid.Parse(in.RequestID); err != nil || text == "" || len([]rune(text)) > 300 {
			return nil, false, problem.New("inflow_note_rejected", "留言需要唯一 UUID 和 1–300 字正文", "")
		}
		if old, ok := b.Notes[in.RequestID]; ok {
			if old.TargetID != in.TargetID || old.Text != text || old.Actor.Kind != actor.Kind || old.Actor.MemberID != actor.MemberID || old.Actor.Provider != actor.Provider || old.Actor.Session != actor.Session {
				return nil, false, fmt.Errorf("此留言标识已用于其他内容")
			}
			return old, false, nil
		}
		target := b.Targets[in.TargetID]
		if target.ID == "" || target.Removed || (target.MemberID != "owner" && (s.sharedRuntime() == nil || !s.sharedRuntime().HasMember(target.MemberID))) {
			return nil, false, problem.New("inflow_note_rejected", "请选择已关联的接收会话", "")
		}
		key := inFlowRecipient(target.ID, target.ExecutionID)
		if !b.InFlow[key].Enabled {
			return nil, false, problem.New("inflow_note_rejected", "接收会话尚未开启顺流感知", "")
		}
		if len(b.Notes) >= 2000 {
			return nil, false, problem.New("inflow_note_rejected", "空间留言已达上限", "")
		}
		note := SpaceNote{ID: in.RequestID, TargetID: target.ID, Actor: actor, Text: text, CreatedAt: now}
		b.Notes[note.ID] = note
		return note, true, nil
	}
	if (in.TargetID == "") == (in.ExecutionID == "") {
		return nil, false, fmt.Errorf("请选择一个准确的接收目标或协作会话")
	}
	target := b.Targets[in.TargetID]
	executionID := in.ExecutionID
	provider, session := "", ""
	if in.TargetID != "" {
		if target.ID == "" || target.Removed || target.MemberID != actor.MemberID {
			return nil, false, fmt.Errorf("只能操作自己关联的接收会话")
		}
		provider, session, executionID = target.Provider, target.Session, target.ExecutionID
	} else {
		if actor.MemberID != "owner" {
			return nil, false, fmt.Errorf("协作会话顺流感知由空间发起者设置")
		}
	}
	if executionID != "" {
		ex := s.executions[executionID]
		if ex == nil || ex.closed {
			return nil, false, fmt.Errorf("协作会话不存在")
		}
		provider, session = ex.record.Provider, contentHash([]string{ex.record.Provider, ex.record.SessionID})[:24]
		if op != "inflow-config" && (ex.record.Paused || ex.record.State != "ready" || !ex.annotationAccess) {
			return inflow.Batch{Items: []inflow.Item{}}, false, nil
		}
	}
	if actor.Kind == "session" && (actor.Provider != provider || actor.Session != session) {
		return nil, false, fmt.Errorf("顺流感知不属于当前会话")
	}
	if op != "inflow-config" && actor.Kind != "session" {
		return nil, false, fmt.Errorf("顺流投递需要当前会话身份")
	}
	key := inFlowRecipient(in.TargetID, executionID)
	pref := b.InFlow[key]
	if op == "inflow-config" {
		if in.Enabled && pref.Since.IsZero() {
			pref.Since = now
		}
		pref.Enabled = in.Enabled
		b.InFlow[key] = pref
		return map[string]any{"enabled": pref.Enabled, "spaceId": s.record.ID, "targetId": in.TargetID, "executionId": executionID}, true, nil
	}
	if !pref.Enabled {
		return inflow.Batch{Items: []inflow.Item{}}, false, nil
	}
	if s.inFlowLeases == nil {
		s.inFlowLeases = map[string]inFlowLease{}
	}
	for token, lease := range s.inFlowLeases {
		if !lease.Expires.After(now) {
			delete(s.inFlowLeases, token)
		}
	}
	if op == "inflow-delivered" || op == "inflow-release" {
		lease, ok := s.inFlowLeases[in.Receipt]
		if !ok || lease.Recipient != key {
			return nil, false, fmt.Errorf("顺流投递批次已过期或不属于当前会话")
		}
		if op == "inflow-release" {
			delete(s.inFlowLeases, in.Receipt)
			return map[string]bool{"released": true}, false, nil
		}
		if pref.Delivered == nil {
			pref.Delivered = map[string]int{}
		}
		for _, item := range lease.Items {
			pref.Delivered[item.Key] = max(pref.Delivered[item.Key], item.Version)
			if item.Kind == "note" {
				note := b.Notes[item.SourceID]
				if note.ID != "" && note.DeliveredAt == nil {
					note.DeliveredAt = &now
					b.Notes[note.ID] = note
				}
			}
		}
		b.InFlow[key] = pref
		// Keep the lease until expiry: a lost save response can safely commit the
		// same batch again without advancing beyond the versions actually output.
		return map[string]bool{"delivered": true}, true, nil
	}
	if op != "inflow-prepare" {
		return nil, false, fmt.Errorf("未知顺流感知操作")
	}
	for _, lease := range s.inFlowLeases {
		if lease.Recipient == key {
			pending := false
			for _, item := range lease.Items {
				pending = pending || pref.Delivered[item.Key] < item.Version
			}
			if pending {
				return inflow.Batch{Items: []inflow.Item{}}, false, nil
			}
		}
	}
	items := s.inFlowItemsLocked(in.TargetID, executionID, actor)
	sort.Slice(items, func(i, j int) bool {
		if items[i].At.Equal(items[j].At) {
			return items[i].Key < items[j].Key
		}
		return items[i].At.Before(items[j].At)
	})
	batch := inflow.Batch{SpaceID: s.record.ID, TargetID: in.TargetID, ExecutionID: in.ExecutionID, Items: []inflow.Item{}}
	for _, item := range items {
		if !item.At.After(pref.Since) || item.Version <= pref.Delivered[item.Key] {
			continue
		}
		candidate := append(append([]inflow.Item{}, batch.Items...), item)
		encoded, _ := json.Marshal(candidate)
		if len(encoded) > inflow.MaxBytes {
			break
		}
		batch.Items = candidate
		if len(batch.Items) == inflow.MaxItems {
			break
		}
	}
	if len(batch.Items) != 0 {
		batch.Receipt = uuid.NewString()
		s.inFlowLeases[batch.Receipt] = inFlowLease{Recipient: key, Expires: now.Add(30 * time.Second), Items: batch.Items}
	}
	return batch, false, nil
}

func (s *Session) inFlowItemsLocked(targetID, executionID string, actor SpaceActor) []inflow.Item {
	b := s.record.Workbench
	items := []inflow.Item{}
	add := func(item inflow.Item) {
		item.SpaceID = s.record.ID
		item.Author = shortText(item.Author, 80)
		items = append(items, item)
	}
	if b.Brief.Revision > 0 {
		add(inflow.Item{Key: "brief", Version: b.Brief.Revision, Kind: "brief", Text: shortText(b.Brief.Topic, 160), At: b.Brief.UpdatedAt})
	}
	if ex := s.executions[executionID]; executionID != "" && ex != nil {
		for _, note := range ex.record.Annotations {
			if note.ExecutionID != executionID {
				continue
			}
			item := inflow.Item{Key: "comment:" + note.ID, Version: 1 + len(note.Replies), Kind: "comment", ExecutionID: executionID, SourceID: note.ID, Author: note.Author, Text: shortText(note.Text, 160), At: note.CreatedAt}
			if len(note.Replies) > 0 {
				reply := note.Replies[len(note.Replies)-1]
				item.Kind, item.Author, item.Text, item.At = "comment_reply", reply.Author, shortText(reply.Text, 160), reply.CreatedAt
			}
			add(item)
		}
	}
	for _, note := range b.Notes {
		target := b.Targets[note.TargetID]
		if target.Removed {
			continue
		}
		if note.TargetID == targetID || (executionID != "" && target.ExecutionID == executionID) {
			add(inflow.Item{Key: "note:" + note.ID, Version: 1, Kind: "note", SourceID: note.ID, Author: note.Actor.Name, Text: note.Text, At: note.CreatedAt})
		}
	}
	for _, request := range b.Requests {
		if request.Actor.Kind != "session" || request.Actor.MemberID != actor.MemberID || request.Actor.Provider != actor.Provider || request.Actor.Session != actor.Session {
			continue
		}
		if request.State != "completed" && request.State != "failed" {
			continue
		}
		if s.workbenchRefsLocked(request.References) != nil {
			continue
		}
		add(inflow.Item{Key: "request:" + request.ID, Version: 1, Kind: "request_result", SourceID: request.ID, State: request.State, Text: shortText(request.Summary+" "+request.Error, 160), At: request.UpdatedAt})
	}
	return items
}
