package share

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"teamcross/internal/invite"
)

const HandshakePath = "/share/v1/handshake"

// AccessGate binds a listener to one Share and authenticates every request.
// It is intentionally unaware of Threads, storage, or the local admin API.
type AccessGate struct {
	ShareID   string
	Secret    []byte
	ExpiresAt time.Time
	Revoked   func() bool
	Now       func() time.Time
	Next      http.Handler
}

func (gate AccessGate) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	now := time.Now
	if gate.Now != nil {
		now = gate.Now
	}
	response.Header().Set("X-TeamCross-Protocol", strconv.FormatUint(invite.Version, 10))
	if request.Header.Get("X-TeamCross-Protocol") != strconv.FormatUint(invite.Version, 10) {
		response.Header().Set("X-TeamCross-Error", "version")
		http.Error(response, "incompatible Team Cross protocol", http.StatusUpgradeRequired)
		return
	}
	if gate.Revoked != nil && gate.Revoked() {
		response.Header().Set("X-TeamCross-Error", "revoked")
		http.Error(response, "share revoked", http.StatusGone)
		return
	}
	if !gate.ExpiresAt.IsZero() && !now().Before(gate.ExpiresAt) {
		response.Header().Set("X-TeamCross-Error", "expired")
		http.Error(response, "share expired", http.StatusGone)
		return
	}
	if request.Header.Get("X-TeamCross-Share-ID") != gate.ShareID || !validBearer(request.Header.Get("Authorization"), gate.Secret) {
		response.Header().Set("X-TeamCross-Error", "unauthorized")
		http.Error(response, "invalid share credentials", http.StatusUnauthorized)
		return
	}
	if request.URL.Path == HandshakePath {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"ok":       true,
			"shareId":  gate.ShareID,
			"protocol": invite.Version,
		})
		return
	}
	if gate.Next == nil {
		http.NotFound(response, request)
		return
	}
	gate.Next.ServeHTTP(response, request)
}

func validBearer(header string, secret []byte) bool {
	scheme, encoded, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	got, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(got) != len(secret) {
		return false
	}
	return subtle.ConstantTimeCompare(got, secret) == 1
}
