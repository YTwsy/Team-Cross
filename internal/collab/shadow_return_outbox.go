package collab

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/inflow"
	"teamcross/internal/shadow"
)

// Pending result bodies outlive the ordinary 256-result browsing history.
// Delivered IDs are small durable dedupe records; only their recent bodies stay.
type ShadowReturnInbox struct {
	ID                string                       `json:"id"`
	MessageID         string                       `json:"messageId"`
	MemberID          string                       `json:"memberId"`
	SessionKey        string                       `json:"sessionKey"`
	Events            map[string]ShadowReturnEvent `json:"events"`
	Delivered         map[string]time.Time         `json:"delivered"`
	DeliveredVersions map[string]int               `json:"deliveredVersions,omitempty"`
}

type ShadowReturnEvent struct {
	Item     inflow.Item            `json:"item"`
	Result   *shadow.TaskResult     `json:"result,omitempty"`
	Question *shadow.QuestionReturn `json:"question,omitempty"`
}

func shadowReturnDelivered(route ShadowReturnInbox, item inflow.Item) bool {
	if route.DeliveredVersions[item.Key] >= item.Version {
		return true
	}
	_, oldReceipt := route.Delivered[item.Key]
	return oldReceipt && item.Version == 1
}

func bindShadowReturnInbox(shared *ShadowShared, in shadowInput, actor shadow.Actor) error {
	if in.ReturnRouteID == "" {
		if in.ReturnSessionKey != "" {
			return fmt.Errorf("任务返回路线不完整")
		}
		return nil
	}
	_, idErr := uuid.Parse(in.ReturnRouteID)
	key, keyErr := hex.DecodeString(in.ReturnSessionKey)
	if idErr != nil || keyErr != nil || len(key) != 12 || actor.Kind == "shadow" {
		return fmt.Errorf("任务返回路线无效")
	}
	if existing, ok := shared.Returns[in.ReturnRouteID]; ok {
		if existing.MessageID != in.Message.ID || existing.MemberID != actor.ID || existing.SessionKey != in.ReturnSessionKey {
			return fmt.Errorf("任务返回路线已属于其他委托")
		}
		return nil
	}
	if shared.Returns == nil {
		shared.Returns = map[string]ShadowReturnInbox{}
	}
	shared.Returns[in.ReturnRouteID] = ShadowReturnInbox{ID: in.ReturnRouteID, MessageID: in.Message.ID, MemberID: actor.ID, SessionKey: in.ReturnSessionKey, Events: map[string]ShadowReturnEvent{}, Delivered: map[string]time.Time{}}
	return nil
}

func validateShadowReturnResults(shared ShadowShared, incoming []shadow.TaskResult) error {
	for _, result := range incoming {
		if hash := shared.ResultHashes[result.ID]; hash != "" && hash != contentHash(result) {
			return fmt.Errorf("分身历史结果不可覆盖")
		}
	}
	previous := append([]shadow.TaskResult{}, shared.View.TaskResults...)
	for _, route := range shared.Returns {
		for _, event := range route.Events {
			if event.Result != nil {
				previous = append(previous, *event.Result)
			}
		}
	}
	return validateShadowResults(previous, incoming)
}

func captureShadowReturns(shared *ShadowShared, exported []shadow.TaskResult) bool {
	changed := false
	for id, route := range shared.Returns {
		add := func(event ShadowReturnEvent) {
			if _, exists := route.Events[event.Item.Key]; exists {
				return
			}
			if _, delivered := route.Delivered[event.Item.Key]; delivered {
				return
			}
			event.Item.SpaceID, event.Item.ShadowID = shared.View.SpaceID, shared.View.ID
			event.Item.Author = shortText(shared.View.Name, 80)
			route.Events[event.Item.Key], changed = event, true
		}
		for _, message := range shared.View.Messages {
			if message.Role != "assistant" || message.ReplyTo != route.MessageID || message.TaskResultID != "" {
				continue
			}
			add(ShadowReturnEvent{Item: inflow.Item{Key: "shadow_message:" + message.ID, Version: 1, Kind: "shadow_reply", SourceID: route.MessageID, MessageID: message.ID, Text: shortText(message.Text, 400), State: "replied", At: message.CreatedAt}})
		}
		results := append(append([]shadow.TaskResult{}, shared.View.TaskResults...), exported...)
		for _, result := range results {
			if result.MessageID != route.MessageID {
				continue
			}
			r := result
			add(ShadowReturnEvent{Item: inflow.Item{Key: "shadow_result:" + r.ID, Version: 1, Kind: "shadow_task_result", SourceID: route.MessageID, ResultID: r.ID, TaskID: r.TaskID, TaskVersion: r.TaskVersion, Text: shortText(r.Text, 400), State: r.State, At: r.CreatedAt}, Result: &r})
		}
		shared.Returns[id] = route
	}
	return changed
}

func (s *Session) shadowReturnMutedLocked(route ShadowReturnInbox) bool {
	b := s.record.Workbench
	if b == nil {
		return false
	}
	for _, target := range b.Targets {
		if target.Removed || target.MemberID != route.MemberID || target.Session != route.SessionKey {
			continue
		}
		if pref, configured := b.InFlow[inFlowRecipient(target.ID, target.ExecutionID)]; configured && !pref.Enabled {
			return true
		}
	}
	// Managed executions have no optional target association.
	if route.MemberID == "owner" {
		for id, ex := range s.executions {
			if contentHash([]string{ex.record.Provider, ex.record.SessionID})[:24] == route.SessionKey {
				if pref, configured := b.InFlow[inFlowRecipient("", id)]; configured && !pref.Enabled {
					return true
				}
			}
		}
	}
	return false
}

func (s *Session) shadowReturnOperationLocked(op string, in shadowInput, actor shadow.Actor, shared *ShadowShared) (any, bool, error) {
	route, ok := shared.Returns[in.ReturnRouteID]
	if !ok || route.MemberID != actor.ID || route.SessionKey != in.ReturnSessionKey {
		return nil, false, fmt.Errorf("任务返回不属于当前发起会话")
	}
	now := time.Now()
	key := "shadow_return:" + in.ID + ":" + route.ID
	if s.inFlowLeases == nil {
		s.inFlowLeases = map[string]inFlowLease{}
	}
	for id, lease := range s.inFlowLeases {
		if !lease.Expires.After(now) {
			delete(s.inFlowLeases, id)
		}
	}
	if op != "return-prepare" {
		lease, found := s.inFlowLeases[in.Receipt]
		if !found || lease.Recipient != key {
			return nil, false, fmt.Errorf("任务返回批次已过期或不属于当前会话")
		}
		if op == "return-release" {
			delete(s.inFlowLeases, in.Receipt)
			return map[string]bool{"released": true}, false, nil
		}
		if route.DeliveredVersions == nil {
			route.DeliveredVersions = map[string]int{}
		}
		for _, item := range lease.Items {
			route.Delivered[item.Key] = now
			route.DeliveredVersions[item.Key] = max(route.DeliveredVersions[item.Key], item.Version)
		}
		// Keep every pending body and the newest 256 delivered bodies.
		delivered := []string{}
		for id, event := range route.Events {
			if shadowReturnDelivered(route, event.Item) {
				delivered = append(delivered, id)
			}
		}
		sort.Slice(delivered, func(i, j int) bool {
			if route.Delivered[delivered[i]].Equal(route.Delivered[delivered[j]]) {
				return delivered[i] < delivered[j]
			}
			return route.Delivered[delivered[i]].Before(route.Delivered[delivered[j]])
		})
		for _, id := range delivered[:max(0, len(delivered)-shadow.TaskResultHistoryLimit)] {
			delete(route.Events, id)
		}
		shared.Returns[route.ID] = route
		return map[string]bool{"delivered": true}, true, nil
	}
	batch := inflow.Batch{SpaceID: s.record.ID, ReturnRouteID: route.ID, Items: []inflow.Item{}}
	if s.shadowReturnMutedLocked(route) {
		return batch, false, nil
	}
	changed := s.refreshShadowQuestionReturnsLocked(shared, &route)
	if changed {
		shared.Returns[route.ID] = route
	}
	for _, lease := range s.inFlowLeases {
		if lease.Recipient != key {
			continue
		}
		for _, item := range lease.Items {
			current, exists := route.Events[item.Key]
			if exists && current.Item.Version == item.Version && !shadowReturnDelivered(route, item) {
				return batch, changed, nil
			}
		}
	}
	items := []inflow.Item{}
	for _, event := range route.Events {
		if !shadowReturnDelivered(route, event.Item) && shadowQuestionReturnVisible(route, event) {
			items = append(items, event.Item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].At.Equal(items[j].At) {
			return items[i].Key < items[j].Key
		}
		return items[i].At.Before(items[j].At)
	})
	for _, item := range items {
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
	if len(batch.Items) > 0 {
		batch.Receipt = uuid.NewString()
		s.inFlowLeases[batch.Receipt] = inFlowLease{Recipient: key, Expires: now.Add(30 * time.Second), Items: batch.Items}
	}
	return batch, changed, nil
}
