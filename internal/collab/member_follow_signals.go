package collab

import (
	"fmt"
	"sort"

	"teamcross/internal/spacecontext"
)

// Saved with the preference in the source record's existing outbox. Retrying a
// follow command cannot create a second initialization or change its snapshot.
func (s *Session) queueFollowChangeLocked(member string, p MemberFollowPreference) error {
	a := s.activityStateLocked()
	if len(a.Outbox) >= maxActivityOutbox {
		return fmt.Errorf("动态待发布队列已满，关注设置尚未保存")
	}
	root := s.activityRoot()
	ref := SpaceSourceRef{SpaceID: root.record.ID}
	change := &spacecontext.FollowChange{Mode: p.Mode, Revision: p.Revision}
	parent := root.activityParentLocked(p.Subject)
	if parent.ID != "" {
		pp, _ := root.activityPreferenceLocked(member, parent)
		change.Parent = &spacecontext.FollowSetting{Subject: parent, Mode: pp.Mode, Revision: pp.Revision}
	}
	var baseline *spacecontext.FollowChange
	var err error
	ref, baseline, err = s.followSourceBaselineLocked(p.Subject)
	if err != nil {
		return err
	}
	change.Baseline, change.Partial = baseline.Baseline, baseline.Partial
	if p.Mode != "following" {
		change.Baseline, change.Partial = nil, false
	}
	e := ActivityEnvelope{
		ID:   contentHash([]any{s.record.ID, "follow", member, p.Subject, p.Revision}),
		Kind: "follow_changed", Group: subjectKey(p.Subject), Source: ref, Subject: p.Subject,
		Actor: SpaceActor{Kind: "system"}, CreatedAt: p.UpdatedAt,
		Recipients: map[string][]string{member: {"follow_preference"}}, Follow: change,
	}
	a.Outbox = append(a.Outbox, e)
	return nil
}

// The caller holds the shared space lock. This captures public source identity
// without changing a member preference or creating an initialization event.
func (s *Session) followSourceBaselineLocked(subject spacecontext.Subject) (SpaceSourceRef, *spacecontext.FollowChange, error) {
	root := s.activityRoot()
	a := s.activityStateLocked()
	ref := SpaceSourceRef{SpaceID: root.record.ID}
	change := &spacecontext.FollowChange{}
	switch subject.Kind {
	case "material":
		ref.MaterialID = subject.ID
		for _, m := range root.record.Materials {
			if m.ID == subject.ID && len(m.Versions) > 0 {
				ref.Version = m.Versions[len(m.Versions)-1].Version
			}
		}
		change.Baseline = []SpaceSourceRef{ref}
	case "discussion":
		for _, n := range s.record.Annotations {
			if n.ID != subject.ID {
				continue
			}
			_, ref = s.activitySubjects(n)
			change.Baseline = []SpaceSourceRef{ref}
			start := max(0, len(n.Replies)-8)
			change.Partial = start > 0
			for _, reply := range n.Replies[start:] {
				r := ref
				r.ReplyID = reply.ID
				change.Baseline = append(change.Baseline, r)
			}
		}
	case "execution":
		ref.ExecutionID = subject.ID
		turns := []ActivityTurn{}
		for _, turn := range a.Turns {
			if turn.State != "" && turn.State != "inProgress" {
				turns = append(turns, turn)
			}
		}
		sort.Slice(turns, func(i, j int) bool {
			if turns[i].UpdatedAt.Equal(turns[j].UpdatedAt) {
				return turns[i].ID < turns[j].ID
			}
			return turns[i].UpdatedAt.Before(turns[j].UpdatedAt)
		})
		if len(turns) > 0 {
			turn := turns[len(turns)-1]
			ref.TurnID, ref.ItemIDs = turn.ID, append([]string{}, turn.ItemIDs...)
			change.Baseline = []SpaceSourceRef{ref}
			change.Partial = len(turns) > 1
		}
	default:
		return ref, change, fmt.Errorf("关注对象类型无效")
	}
	return ref, change, nil
}
