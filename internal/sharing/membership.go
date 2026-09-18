package sharing

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// A joining Core persists this independent credential before admission, so a
// lost join response can be recovered by reading status with the same token.
func NewCredential() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

type admission struct {
	id         string
	invitation *Invitation
	memberID   string
	revoked    bool
}
type member struct {
	Member
	credential string
	ctx        context.Context
	cancel     context.CancelFunc
}
type Member struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	JoinedAt time.Time `json:"joinedAt"`
	Active   bool      `json:"active"`
}
type InvitationInfo struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expiresAt"`
	MemberID  string    `json:"memberId,omitempty"`
}
type IssuedInvitation struct {
	InvitationInfo
	Token string `json:"invitation,omitempty"`
}

func encodeInvitation(i Invitation) string {
	b, _ := json.Marshal(i)
	return "tcx3." + base64.RawURLEncoding.EncodeToString(b)
}
func (r *Runtime) initializeLocked() {
	if r.invitations != nil {
		return
	}
	id := uuid.NewString()
	r.invitations = map[string]*admission{id: {id: id, invitation: &r.Invitation}}
	r.latestInvite = id
	r.members = map[string]*member{}
	r.inviteRequests = map[string]string{}
}
func (r *Runtime) revokeLocked() {
	r.revoked = true
	for _, m := range r.members {
		m.cancel()
	}
}
func (r *Runtime) stateLocked(a *admission) string {
	if r.revoked || a.revoked {
		return "revoked"
	}
	if a.memberID != "" {
		if m := r.members[a.memberID]; m != nil && m.ctx.Err() == nil {
			return "joined"
		}
		return "left"
	}
	if !time.Now().Before(a.invitation.ExpiresAt) {
		return "expired"
	}
	return "pending"
}
func (r *Runtime) infoLocked(a *admission) InvitationInfo {
	return InvitationInfo{ID: a.id, State: r.stateLocked(a), ExpiresAt: a.invitation.ExpiresAt, MemberID: a.memberID}
}

// IssueInvitation creates a separate one-person invitation. Retrying an explicit
// request ID returns its original admission, even after use, expiry or revocation.
// An omitted ID only retrieves the latest admission; it never admits another user.
func (r *Runtime) IssueInvitation(requestID string) (IssuedInvitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initializeLocked()
	if r.revoked {
		return IssuedInvitation{}, fmt.Errorf("共享已结束")
	}
	if len(requestID) > 200 {
		return IssuedInvitation{}, fmt.Errorf("邀请请求标识过长")
	}
	id := r.latestInvite
	if requestID != "" {
		if old, ok := r.inviteRequests[requestID]; ok {
			id = old
		} else {
			// Claim the unassigned initial invitation, then mint distinct invitations.
			if len(r.inviteRequests) != 0 || r.initialExposed || r.stateLocked(r.invitations[id]) != "pending" {
				inv := r.Invitation
				inv.Secret, inv.ExpiresAt = NewCredential(), time.Now().Add(time.Hour).Round(0)
				id = uuid.NewString()
				r.invitations[id] = &admission{id: id, invitation: &inv}
			}
			r.inviteRequests[requestID], r.latestInvite = id, id
		}
	}
	a := r.invitations[id]
	out := IssuedInvitation{InvitationInfo: r.infoLocked(a)}
	if out.State == "pending" {
		r.initialExposed = true
		out.Token = encodeInvitation(*a.invitation)
	}
	return out, nil
}
func (r *Runtime) Invitations() []InvitationInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initializeLocked()
	out := []InvitationInfo{}
	for _, a := range r.invitations {
		out = append(out, r.infoLocked(a))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (r *Runtime) RevokeInvitation(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.invitations[id]
	if a == nil || a.memberID != "" {
		return false
	}
	a.revoked = true
	return true
}
func (r *Runtime) InvitationState() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initializeLocked()
	return r.stateLocked(r.invitations[r.latestInvite])
}
func (r *Runtime) Members() []Member {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Member{}
	for _, m := range r.members {
		v := m.Member
		v.Active = !r.revoked && m.ctx.Err() == nil
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JoinedAt.Before(out[j].JoinedAt) })
	return out
}
func (r *Runtime) HasMember(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.members[id]
	return !r.revoked && m != nil && m.ctx.Err() == nil
}
func (r *Runtime) RevokeMember(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.members[id]
	if r.revoked || m == nil {
		return false
	}
	m.cancel()
	return true
}

type authorizationKey struct{}
type authorization struct {
	runtime *Runtime
	member  *member
}

func MemberID(ctx context.Context) string {
	if a, ok := ctx.Value(authorizationKey{}).(authorization); ok {
		return a.member.ID
	}
	return "owner"
}
func MemberName(ctx context.Context) string {
	if a, ok := ctx.Value(authorizationKey{}).(authorization); ok {
		return a.member.Name
	}
	return "发起者"
}

// Remote requests remain bound to their original share and individual member.
func Authorized(ctx context.Context, current *Runtime) bool {
	a, remote := ctx.Value(authorizationKey{}).(authorization)
	return !remote || (current == a.runtime && a.member.ctx.Err() == nil && ctx.Err() == nil)
}
func (r *Runtime) Leave(ctx context.Context) bool {
	a, ok := ctx.Value(authorizationKey{}).(authorization)
	if !ok || a.runtime != r {
		return false
	}
	return r.RevokeMember(a.member.ID)
}
func rejection(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": message})
}
func (r *Runtime) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var in struct {
			Credential string `json:"credential"`
			Name       string `json:"name"`
		}
		joining := req.Method == "POST" && req.URL.Path == "/v2/join"
		if joining && json.NewDecoder(http.MaxBytesReader(w, req.Body, 2048)).Decode(&in) != nil {
			rejection(w, 400, "invitation_invalid", "加入请求无效")
			return
		}
		r.mu.Lock()
		r.initializeLocked()
		if r.revoked {
			r.mu.Unlock()
			rejection(w, 410, "sharing_ended", "发起者已结束共享，请使用新的邀请")
			return
		}
		if !strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ") {
			r.mu.Unlock()
			rejection(w, 401, "membership_invalid", "需要有效的 Bearer 凭据")
			return
		}
		credential := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
		if joining || (req.Method == "GET" && req.URL.Path == "/v2/invitation") {
			var a *admission
			for _, candidate := range r.invitations {
				if equal(credential, candidate.invitation.Secret) {
					a = candidate
					break
				}
			}
			if a == nil {
				r.mu.Unlock()
				rejection(w, 401, "invitation_invalid", "邀请无效")
				return
			}
			if a.revoked {
				r.mu.Unlock()
				rejection(w, 410, "invitation_revoked", "邀请已撤销，请获取新邀请")
				return
			}
			m := r.members[a.memberID]
			retry := joining && m != nil && m.ctx.Err() == nil && equal(in.Credential, m.credential)
			if a.memberID != "" && !retry {
				r.mu.Unlock()
				rejection(w, 410, "invitation_used", "邀请已使用，请获取另一份邀请")
				return
			}
			if !retry && !time.Now().Before(a.invitation.ExpiresAt) {
				r.mu.Unlock()
				rejection(w, 410, "invitation_expired", "邀请已到期，请获取新邀请")
				return
			}
			if joining && !retry {
				b, err := base64.RawURLEncoding.DecodeString(in.Credential)
				valid := err == nil && len(b) == 32 && utf8.ValidString(in.Name) && utf8.RuneCountInString(in.Name) <= 80
				for _, invite := range r.invitations {
					valid = valid && !equal(in.Credential, invite.invitation.Secret)
				}
				for _, existing := range r.members {
					valid = valid && !equal(in.Credential, existing.credential)
				}
				if !valid {
					r.mu.Unlock()
					rejection(w, 400, "invitation_invalid", "加入凭据或成员名称无效")
					return
				}
				id := uuid.NewString()
				name := strings.TrimSpace(in.Name)
				if name == "" {
					name = "协作者 " + id[:8]
				}
				ctx, cancel := context.WithCancel(context.Background())
				m = &member{Member: Member{ID: id, Name: name, JoinedAt: time.Now(), Active: true}, credential: in.Credential, ctx: ctx, cancel: cancel}
				r.members[id], a.memberID = m, id
			}
			memberID := a.memberID
			r.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "memberId": memberID})
			return
		}
		var m *member
		for _, candidate := range r.members {
			if equal(credential, candidate.credential) {
				m = candidate
				break
			}
		}
		if m == nil {
			r.mu.Unlock()
			rejection(w, 401, "membership_invalid", "协作访问凭据无效，请先加入")
			return
		}
		if m.ctx.Err() != nil {
			r.mu.Unlock()
			rejection(w, 410, "sharing_ended", "该成员访问已结束，请使用新邀请")
			return
		}
		r.mu.Unlock()
		ctx, cancel := context.WithCancel(req.Context())
		stop := context.AfterFunc(m.ctx, cancel)
		defer func() { stop(); cancel() }()
		ctx = context.WithValue(ctx, authorizationKey{}, authorization{r, m})
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

func (r *Runtime) GetInvitation(id string) (IssuedInvitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.invitations[id]
	if a == nil || r.revoked {
		return IssuedInvitation{}, fmt.Errorf("邀请不存在或共享已结束")
	}
	out := IssuedInvitation{InvitationInfo: r.infoLocked(a)}
	if out.State == "pending" {
		out.Token = encodeInvitation(*a.invitation)
		r.initialExposed = true
	}
	return out, nil
}
