package collab

import (
	"context"
	"fmt"
	"time"

	"teamcross/internal/sharing"
)

type memberPresence struct {
	Seen      time.Time
	Requested bool
}
type MemberView struct {
	sharing.Member
	Online         bool `json:"online"`
	InputRequested bool `json:"inputRequested"`
}

func (s *Session) membersLocked() []MemberView {
	out := []MemberView{}
	if s.sharedRuntime() == nil {
		return out
	}
	for _, m := range s.sharedRuntime().Members() {
		p := s.presence[m.ID]
		if s.space != nil {
			p.Seen = s.space.presence[m.ID].Seen
		}
		out = append(out, MemberView{Member: m, Online: m.Active && time.Since(p.Seen) < 30*time.Second, InputRequested: m.Active && p.Requested})
	}
	return out
}
func (s *Session) removeMemberLocked(id string) {
	delete(s.presence, id)
	if s.direct != nil && s.direct.role == id {
		s.direct.close()
		s.direct = nil
	}
	if s.writer == id {
		s.writer = "owner"
		s.epoch++
	}
}
func (s *Session) RevokeMember(id string) error {
	if s.space != nil {
		return s.space.RevokeMember(id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sharedRuntime() == nil || !s.sharedRuntime().RevokeMember(id) {
		return fmt.Errorf("没有找到可移除的成员")
	}
	s.removeMemberLocked(id)
	for _, child := range s.executions {
		child.removeMemberLocked(id)
	}
	return nil
}
func (s *Session) Invite(ctx context.Context, transport, requestID string, reset ...bool) (sharing.IssuedInvitation, error) {
	if len(reset) > 0 && reset[0] && requestID == "" {
		return sharing.IssuedInvitation{}, fmt.Errorf("重置链接需要 requestId")
	}
	if err := s.Share(ctx, transport); err != nil {
		return sharing.IssuedInvitation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sharedRuntime() == nil {
		return sharing.IssuedInvitation{}, fmt.Errorf("共享已结束")
	}
	if len(reset) > 0 && reset[0] {
		return s.sharedRuntime().ResetInvitation(requestID)
	}
	return s.sharedRuntime().IssueInvitation(requestID)
}
func (s *Session) RevokeInvitation(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sharedRuntime() == nil || !s.sharedRuntime().RevokeInvitation(id) {
		return fmt.Errorf("邀请链接不存在")
	}
	return nil
}

// A missing target is only unambiguous when exactly one member is active.
func (s *Session) handoffMemberLocked(id string) (string, error) {
	if s.sharedRuntime() == nil {
		return "", fmt.Errorf("请先创建邀请")
	}
	if id != "" {
		if s.sharedRuntime().HasMember(id) {
			return id, nil
		}
		return "", fmt.Errorf("所选成员尚未加入或已离开")
	}
	for _, m := range s.sharedRuntime().Members() {
		if !m.Active {
			continue
		}
		if id != "" {
			return "", fmt.Errorf("有多位成员，请明确选择输入接收者")
		}
		id = m.ID
	}
	if id == "" {
		return "", fmt.Errorf("同事尚未加入协作")
	}
	return id, nil
}

func (s *Session) Invitation(id string) (sharing.IssuedInvitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sharedRuntime() == nil {
		return sharing.IssuedInvitation{}, fmt.Errorf("共享已结束")
	}
	return s.sharedRuntime().GetInvitation(id)
}
