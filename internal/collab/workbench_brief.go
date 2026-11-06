package collab

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const briefHistoryLimit = 256
const briefCommandLimit = 4096

type BriefChange struct {
	Operation string     `json:"operation"`
	Section   string     `json:"section"`
	ItemID    string     `json:"itemId,omitempty"`
	Item      *BriefItem `json:"item,omitempty"`
}

type BriefCommand struct {
	Fingerprint string     `json:"fingerprint"`
	Actor       SpaceActor `json:"actor"`
	Revision    int        `json:"revision"`
}

type BriefResult struct {
	SpaceID     string         `json:"spaceId"`
	State       string         `json:"state,omitempty"`
	RequestID   string         `json:"requestId,omitempty"`
	Revision    int            `json:"revision"`
	Source      SpaceSourceRef `json:"source"`
	Brief       *SpaceBrief    `json:"brief,omitempty"`
	Item        *BriefItem     `json:"item,omitempty"`
	Section     string         `json:"section,omitempty"`
	Conflicts   []string       `json:"conflicts,omitempty"`
	Unavailable string         `json:"unavailable,omitempty"`
	ReadBack    *BriefReadBack `json:"readBack,omitempty"`
}

type BriefReadBack struct {
	Tool      string `json:"tool"`
	RequestID string `json:"requestId"`
}

func briefSections(b *SpaceBrief) map[string]*[]BriefItem {
	return map[string]*[]BriefItem{"decisions": &b.Decisions, "questions": &b.Questions, "progress": &b.Progress}
}

func briefDefaultStatus(section string) string {
	return map[string]string{"decisions": "proposed", "questions": "open", "progress": "in_progress"}[section]
}

func briefWithIDs(b SpaceBrief) SpaceBrief {
	b = libraryClone(b)
	for section, items := range briefSections(&b) {
		if *items == nil {
			*items = []BriefItem{}
		}
		for i := range *items {
			item := &(*items)[i]
			if item.ID == "" {
				item.ID = "brief-" + contentHash([]any{section, i, item.Text})[:24]
			}
			if item.Status == "" {
				item.Status = briefDefaultStatus(section)
				if section == "decisions" && b.Revision > 0 && b.UpdatedBy.Kind == "human" {
					item.Status = "confirmed"
				}
			}
			if item.UpdatedBy.Kind == "" {
				item.UpdatedBy = b.UpdatedBy
			}
			if item.UpdatedAt.IsZero() {
				item.UpdatedAt = b.UpdatedAt
			}
			if item.Sources == nil {
				item.Sources = []SpaceSourceRef{}
			}
		}
	}
	return b
}

func briefItems(b SpaceBrief) map[string]BriefItem {
	out := map[string]BriefItem{}
	for _, items := range briefSections(&b) {
		for _, item := range *items {
			out[item.ID] = item
		}
	}
	return out
}

func briefItemContent(item BriefItem) string {
	item.UpdatedBy, item.UpdatedAt = SpaceActor{}, time.Time{}
	return contentHash(item)
}

func sameBriefActor(a, b SpaceActor) bool {
	return a.Kind == b.Kind && a.MemberID == b.MemberID && a.ShadowID == b.ShadowID && a.Provider == b.Provider && a.Session == b.Session
}

func (s *Session) briefRevisionLocked(revision int) (SpaceBrief, bool) {
	b := s.record.Workbench
	if revision == b.Brief.Revision {
		return briefWithIDs(b.Brief), true
	}
	old, ok := b.BriefHistory[revision]
	return briefWithIDs(old), ok
}

func (s *Session) readBriefLocked(in workbenchInput) (BriefResult, error) {
	b := s.record.Workbench
	revision := in.Revision
	if revision < 0 {
		return BriefResult{}, fmt.Errorf("简报版本无效")
	}
	state := ""
	if in.RequestID != "" {
		command, found := b.BriefCommands[in.RequestID]
		if !found {
			return BriefResult{}, fmt.Errorf("没有找到这次简报更新")
		}
		if revision != 0 && revision != command.Revision {
			return BriefResult{}, fmt.Errorf("简报更新与指定版本不匹配")
		}
		revision, state = command.Revision, "saved"
	} else if revision == 0 {
		revision = b.Brief.Revision
	}
	result := BriefResult{SpaceID: s.record.ID, State: state, RequestID: in.RequestID, Revision: revision, Source: SpaceSourceRef{SpaceID: s.record.ID, BriefRevision: revision, BriefItemID: in.ItemID}}
	if in.RequestID != "" {
		result.ReadBack = &BriefReadBack{Tool: "read_space_brief", RequestID: in.RequestID}
	}
	brief, found := s.briefRevisionLocked(revision)
	if !found {
		result.Unavailable = "brief_revision_unavailable"
		return result, nil
	}
	if in.ItemID != "" {
		for section, items := range briefSections(&brief) {
			for _, item := range *items {
				if item.ID == in.ItemID {
					result.Item, result.Section = &item, section
					return result, nil
				}
			}
		}
		result.Unavailable = "brief_item_missing"
	} else {
		result.Brief = &brief
	}
	return result, nil
}

func (s *Session) briefConflictLocked(in workbenchInput, full bool) BriefResult {
	current := s.record.Workbench.Brief
	previous, known := s.briefRevisionLocked(in.BaseRevision)
	oldItems, nowItems := briefItems(previous), briefItems(current)
	wanted := map[string]bool{}
	if full {
		for id := range nowItems {
			wanted[id] = true
		}
		for id := range oldItems {
			wanted[id] = true
		}
	}
	for _, change := range in.Changes {
		id := change.ItemID
		if change.Item != nil {
			id = change.Item.ID
		}
		wanted[id] = true
	}
	conflicts := []string{}
	if !known {
		conflicts = append(conflicts, "$history")
	}
	if (full || in.Topic != nil) && (!known || previous.Topic != current.Topic) {
		conflicts = append(conflicts, "$topic")
	}
	for id := range wanted {
		if !known || briefItemContent(oldItems[id]) != briefItemContent(nowItems[id]) {
			conflicts = append(conflicts, id)
		}
	}
	sort.Strings(conflicts)
	return BriefResult{SpaceID: s.record.ID, State: "conflict", Revision: current.Revision, Source: SpaceSourceRef{SpaceID: s.record.ID, BriefRevision: current.Revision}, Brief: &current, Conflicts: conflicts}
}

func (s *Session) validateBriefSourcesLocked(refs []SpaceSourceRef) error {
	if len(refs) > 32 {
		return fmt.Errorf("简报条目最多引用 32 项公开来源")
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		key := contentHash(ref)
		if ref.SpaceID != s.record.ID || seen[key] {
			return fmt.Errorf("简报来源需要本空间的唯一准确引用")
		}
		seen[key] = true
		if ref.IsShadow() {
			if err := ref.ValidateShadow(); err != nil {
				return err
			}
		}
		if ref.Version < 0 || (ref.Version > 0 && ref.MaterialID == "") || (ref.TurnID != "" && ref.ExecutionID == "") || (ref.AnnotationID != "" && ref.TurnID != "") {
			return fmt.Errorf("简报来源定位不完整或混合了不同对象")
		}
		if ref.BriefRevision < 0 || (ref.BriefItemID != "" && ref.BriefRevision == 0) || (ref.ReplyID != "" && ref.AnnotationID == "") || (len(ref.ItemIDs) > 0 && ref.TurnID == "") {
			return fmt.Errorf("简报来源定位不完整")
		}
		categories := 0
		if ref.ShadowID != "" {
			categories++
		}
		if ref.BriefRevision > 0 {
			categories++
		}
		if ref.RequestID != "" {
			categories++
		}
		if ref.AnnotationID != "" {
			categories++
		} else if ref.MaterialID != "" || ref.ExecutionID != "" {
			categories++
		}
		if categories != 1 {
			return fmt.Errorf("简报来源不能混合不同对象")
		}
		switch {
		case ref.ShadowID != "":
			shared, ok := s.record.Shadows[ref.ShadowID]
			if !ok {
				return fmt.Errorf("简报引用的分身不存在")
			}
			read, err := shadowSource(shared, shadowInput{ID: ref.ShadowID, MessageID: ref.ShadowMessageID, ResultID: ref.ShadowResultID})
			if err != nil || read.Unavailable != "" {
				return fmt.Errorf("简报引用的分身消息或结果不可读")
			}
		case ref.BriefRevision > 0:
			read, err := s.readBriefLocked(workbenchInput{Revision: ref.BriefRevision, ItemID: ref.BriefItemID})
			if err != nil || read.Unavailable != "" {
				return fmt.Errorf("简报引用版本或条目已不可读")
			}
		case ref.RequestID != "":
			request, ok := s.record.Workbench.Requests[ref.RequestID]
			if !ok || request.Summary == "" || (request.State != "completed" && request.State != "failed") {
				return fmt.Errorf("简报关联的请求尚无可引用结果")
			}
		case ref.AnnotationID != "":
			target := s
			if ref.ExecutionID != "" {
				target = s.executions[ref.ExecutionID]
			}
			if target == nil || target.record.State == "unsupported" {
				return fmt.Errorf("简报讨论来源不存在")
			}
			found := false
			for _, note := range visibleAnnotations(target.record.Annotations, target != s) {
				if note.ID != ref.AnnotationID {
					continue
				}
				if err := s.validateReferencesLocked(note.Materials); err != nil {
					return err
				}
				if note.Target != nil && note.Target.Kind == "material" {
					if err := s.validateReferencesLocked([]MaterialReference{{MaterialID: note.Target.MaterialID, Version: note.Target.Version}}); err != nil {
						return err
					}
					if ref.MaterialID != "" && (ref.MaterialID != note.Target.MaterialID || ref.Version != note.Target.Version) {
						return fmt.Errorf("讨论与材料版本不匹配")
					}
				} else if ref.MaterialID != "" {
					return fmt.Errorf("讨论不属于指定材料")
				}
				found = ref.ReplyID == ""
				for _, reply := range note.Replies {
					if reply.ID == ref.ReplyID {
						found = true
					}
				}
			}
			if !found {
				return fmt.Errorf("简报讨论或具体回复不存在")
			}
		case ref.MaterialID != "":
			if ref.ExecutionID != "" || ref.TurnID != "" || ref.Version <= 0 {
				return fmt.Errorf("简报材料需要准确发布版本")
			}
			if err := s.validateReferencesLocked([]MaterialReference{{MaterialID: ref.MaterialID, Version: ref.Version}}); err != nil {
				return err
			}
		case ref.ExecutionID != "":
			target := s.executions[ref.ExecutionID]
			if target == nil || ref.TurnID == "" || target.record.Activity == nil {
				return fmt.Errorf("简报会话来源需要已记录的准确轮次")
			}
			turn, ok := target.record.Activity.Turns[ref.TurnID]
			if !ok || (turn.State != "completed" && turn.State != "failed" && turn.State != "interrupted") {
				return fmt.Errorf("简报会话轮次尚不可引用")
			}
			for _, itemID := range ref.ItemIDs {
				found := false
				for _, known := range turn.ItemIDs {
					found = found || itemID == known
				}
				if !found {
					return fmt.Errorf("简报引用的消息不属于该轮次")
				}
			}
		}
	}
	return nil
}

func (s *Session) updateBriefLocked(in workbenchInput, actor SpaceActor, now time.Time, full bool) (BriefResult, bool, error) {
	b := s.record.Workbench
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return BriefResult{}, false, fmt.Errorf("简报更新需要唯一 requestId，同一行动重试保持不变")
	}
	fingerprint := contentHash([]any{in.BaseRevision, in.Topic, in.Changes, full, in.Brief})
	if command, found := b.BriefCommands[in.RequestID]; found {
		if !sameBriefActor(command.Actor, actor) || command.Fingerprint != fingerprint {
			return BriefResult{}, false, fmt.Errorf("简报更新 ID 已被其他身份或不同内容使用")
		}
		out, err := s.readBriefLocked(workbenchInput{RequestID: in.RequestID})
		return out, false, err
	}
	if in.BaseRevision < 0 {
		return BriefResult{}, false, fmt.Errorf("简报基准版本无效")
	}
	if in.BaseRevision != b.Brief.Revision {
		return s.briefConflictLocked(in, full), false, nil
	}
	if len(b.BriefCommands) >= briefCommandLimit {
		return BriefResult{}, false, fmt.Errorf("简报更新记录已达容量上限，既有内容和回执保留")
	}
	next := libraryClone(b.Brief)
	if full {
		next = libraryClone(in.Brief)
	} else {
		if len(in.Changes) > 32 || (len(in.Changes) == 0 && in.Topic == nil) {
			return BriefResult{}, false, fmt.Errorf("简报更新需要议题或 1–32 项条目修改")
		}
		if in.Topic != nil {
			next.Topic = *in.Topic
		}
		seen := map[string]bool{}
		for _, change := range in.Changes {
			items := briefSections(&next)[change.Section]
			if items == nil {
				return BriefResult{}, false, fmt.Errorf("未知简报分区")
			}
			id := change.ItemID
			if change.Item != nil {
				id = change.Item.ID
			}
			if id == "" || seen[id] || len(id) > 128 {
				return BriefResult{}, false, fmt.Errorf("每次修改需要不同且稳定的 itemId")
			}
			seen[id] = true
			index := -1
			for i, item := range *items {
				if item.ID == id {
					index = i
					break
				}
			}
			switch change.Operation {
			case "upsert":
				if change.Item == nil || change.ItemID != "" {
					return BriefResult{}, false, fmt.Errorf("upsert 需要完整 item")
				}
				if index < 0 {
					*items = append(*items, *change.Item)
				} else {
					(*items)[index] = *change.Item
				}
			case "remove":
				if change.Item != nil || index < 0 {
					return BriefResult{}, false, fmt.Errorf("remove 需要已有 itemId")
				}
				*items = append((*items)[:index], (*items)[index+1:]...)
			default:
				return BriefResult{}, false, fmt.Errorf("未知简报修改操作")
			}
		}
	}
	oldItems, seen := briefItems(b.Brief), map[string]bool{}
	for section, items := range briefSections(&next) {
		if len(*items) > 32 {
			return BriefResult{}, false, fmt.Errorf("简报各分区最多 32 条")
		}
		if *items == nil {
			*items = []BriefItem{}
		}
		for i := range *items {
			item := &(*items)[i]
			if item.ID == "" && full {
				item.ID = uuid.NewString()
			}
			if item.ID == "" || len(item.ID) > 128 || seen[item.ID] {
				return BriefResult{}, false, fmt.Errorf("简报 itemId 无效或重复")
			}
			seen[item.ID] = true
			if item.Status == "" {
				item.Status = briefDefaultStatus(section)
			}
			valid := map[string]string{"decisions": " proposed confirmed disputed ", "questions": " open resolved ", "progress": " in_progress completed blocked "}[section]
			if !strings.Contains(valid, " "+item.Status+" ") {
				return BriefResult{}, false, fmt.Errorf("简报条目状态与分区不匹配")
			}
			if strings.TrimSpace(item.Text) == "" || len([]rune(item.Text)) > 2000 {
				return BriefResult{}, false, fmt.Errorf("简报条目需要 1–2000 字")
			}
			if item.Sources == nil {
				item.Sources = []SpaceSourceRef{}
			}
			if item.RequestID != "" {
				if err := s.validateBriefSourcesLocked([]SpaceSourceRef{{SpaceID: s.record.ID, RequestID: item.RequestID}}); err != nil {
					return BriefResult{}, false, err
				}
			}
			old, exists := oldItems[item.ID]
			if exists && briefItemContent(old) == briefItemContent(*item) {
				item.UpdatedAt, item.UpdatedBy = old.UpdatedAt, old.UpdatedBy
			} else {
				if err := s.validateBriefSourcesLocked(item.Sources); err != nil {
					return BriefResult{}, false, err
				}
				item.UpdatedAt, item.UpdatedBy = now, actor
			}
		}
	}
	if len([]rune(next.Topic)) > 2000 {
		return BriefResult{}, false, fmt.Errorf("简报议题最多 2000 字")
	}
	next.Revision, next.UpdatedBy, next.UpdatedAt = b.Brief.Revision, b.Brief.UpdatedBy, b.Brief.UpdatedAt
	if contentHash(next) != contentHash(b.Brief) {
		if b.BriefHistory == nil {
			b.BriefHistory = map[int]SpaceBrief{}
		}
		if b.Brief.Revision > 0 {
			b.BriefHistory[b.Brief.Revision] = libraryClone(b.Brief)
		}
		next.Revision, next.UpdatedBy, next.UpdatedAt = b.Brief.Revision+1, actor, now
	}
	encoded, _ := json.Marshal(next)
	if len(encoded) > 24000 {
		return BriefResult{}, false, fmt.Errorf("简报超过 24 KB，请精简文字和来源")
	}
	b.Brief = next
	if b.BriefCommands == nil {
		b.BriefCommands = map[string]BriefCommand{}
	}
	b.BriefCommands[in.RequestID] = BriefCommand{Fingerprint: fingerprint, Actor: actor, Revision: next.Revision}
	versions := []int{}
	for version := range b.BriefHistory {
		versions = append(versions, version)
	}
	sort.Ints(versions)
	for len(versions) > briefHistoryLimit {
		delete(b.BriefHistory, versions[0])
		versions = versions[1:]
	}
	out, err := s.readBriefLocked(workbenchInput{RequestID: in.RequestID})
	return out, true, err
}
