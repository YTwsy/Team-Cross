package sharing

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"
)

// A joining Core persists this independent credential before admission, so a
// lost join response can be recovered by reading status with the same token.
func NewCredential() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func (r *Runtime) revokeLocked() {
	r.revoked = true
	if r.cancel != nil {
		r.cancel()
	}
}

// InvitationState describes admission, independently from shared access.
func (r *Runtime) InvitationState() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.used {
		if r.memberCtx.Err() == nil {
			return "joined"
		}
		return "left"
	}
	if !time.Now().Before(r.Invitation.ExpiresAt) {
		return "expired"
	}
	return "pending"
}

type authorizationKey struct{}
type authorization struct {
	runtime *Runtime
	member  context.Context
}

// Authorized also accepts local calls, which have already passed local Core
// authentication. Remote requests remain bound to their original share/member.
func Authorized(ctx context.Context, current *Runtime) bool {
	a, remote := ctx.Value(authorizationKey{}).(authorization)
	return !remote || (current == a.runtime && a.member.Err() == nil && ctx.Err() == nil)
}

func (r *Runtime) Leave(ctx context.Context) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := ctx.Value(authorizationKey{}).(authorization)
	if !ok || a.runtime != r || a.member != r.memberCtx || r.revoked || a.member.Err() != nil {
		return false
	}
	r.cancel()
	return true
}

func rejection(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": message})
}

func (r *Runtime) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Bound parsing happens before taking the membership lock.
		var in struct {
			Credential string `json:"credential"`
		}
		joining := req.Method == "POST" && req.URL.Path == "/v2/join"
		if joining && json.NewDecoder(http.MaxBytesReader(w, req.Body, 1024)).Decode(&in) != nil {
			rejection(w, 400, "invitation_invalid", "加入请求无效")
			return
		}
		r.mu.Lock()
		if r.revoked {
			r.mu.Unlock()
			rejection(w, 410, "sharing_ended", "发起者已结束共享，请使用新的邀请")
			return
		}
		if joining || (req.Method == "GET" && req.URL.Path == "/v2/invitation") {
			if !equal(req.Header.Get("Authorization"), "Bearer "+r.Invitation.Secret) {
				r.mu.Unlock()
				rejection(w, 401, "invitation_invalid", "邀请无效")
				return
			}
			// An explicit retry of the same admission is idempotent, including
			// after the original deadline. It never admits a different person.
			retry := joining && r.used && equal(in.Credential, r.member) && r.memberCtx.Err() == nil
			if r.used && !retry {
				r.mu.Unlock()
				rejection(w, 410, "invitation_used", "邀请已使用，请让发起者创建新邀请")
				return
			}
			if !retry && !time.Now().Before(r.Invitation.ExpiresAt) {
				r.mu.Unlock()
				rejection(w, 410, "invitation_expired", "邀请已到期，请让发起者重新分享")
				return
			}
			if joining && !retry {
				b, err := base64.RawURLEncoding.DecodeString(in.Credential)
				if err != nil || len(b) != 32 || equal(in.Credential, r.Invitation.Secret) {
					r.mu.Unlock()
					rejection(w, 400, "invitation_invalid", "加入凭据无效")
					return
				}
				r.used, r.member = true, in.Credential
				r.memberCtx, r.cancel = context.WithCancel(context.Background())
			}
			r.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if !r.used || !equal(req.Header.Get("Authorization"), "Bearer "+r.member) {
			r.mu.Unlock()
			rejection(w, 401, "membership_invalid", "协作访问凭据无效，请先加入")
			return
		}
		if r.memberCtx.Err() != nil {
			r.mu.Unlock()
			rejection(w, 410, "sharing_ended", "已离开协作，请使用新的邀请")
			return
		}
		member := r.memberCtx
		r.mu.Unlock()
		ctx, cancel := context.WithCancel(req.Context())
		stop := context.AfterFunc(member, cancel)
		defer func() { stop(); cancel() }()
		ctx = context.WithValue(ctx, authorizationKey{}, authorization{r, member})
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}
