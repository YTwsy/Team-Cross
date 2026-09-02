// Package invite defines Team Cross' portable, versioned share invitation.
package invite

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const (
	Prefix             = "tcx1."
	Version            = uint64(1)
	ScopeCollaborate   = "collaborate"
	TailcatLibraryV040 = "v0.4.0"
	maxEncodedSize     = 128 << 10
)

var (
	ErrMalformed    = errors.New("malformed Team Cross invitation")
	ErrExpired      = errors.New("Team Cross invitation expired")
	ErrVersion      = errors.New("unsupported Team Cross invitation version")
	ErrScope        = errors.New("unsupported Team Cross invitation scope")
	ErrNonCanonical = errors.New("Team Cross invitation is not canonical CBOR")
)

// InvitationV1 is encoded as canonical CBOR and then raw-base64url encoded.
// Byte slices deliberately remain byte strings in CBOR instead of being
// converted to text: both secret and SPKI digest are uniformly random bytes.
type InvitationV1 struct {
	Version          uint64             `cbor:"version" json:"version"`
	ShareID          string             `cbor:"shareId" json:"shareId"`
	ExpiresAt        int64              `cbor:"expiresAt" json:"expiresAt"`
	ServerSPKISHA256 []byte             `cbor:"serverSPKISHA256" json:"serverSPKISHA256"`
	Secret           []byte             `cbor:"secret" json:"secret"`
	Scope            string             `cbor:"scope" json:"scope"`
	Capabilities     []string           `cbor:"capabilities" json:"capabilities"`
	LAN              LANCandidates      `cbor:"lan" json:"lan"`
	Tailscale        *TailnetCandidates `cbor:"tailscale,omitempty" json:"tailscale,omitempty"`
	Tailcat          *TailcatCandidate  `cbor:"tailcat,omitempty" json:"tailcat,omitempty"`
}

type LANCandidates struct {
	MDNSInstance string   `cbor:"mdnsInstance" json:"mdnsInstance"`
	Endpoints    []string `cbor:"endpoints" json:"endpoints"`
}

type TailnetCandidates struct {
	DNSName   string   `cbor:"dnsName,omitempty" json:"dnsName,omitempty"`
	Endpoints []string `cbor:"endpoints" json:"endpoints"`
}

type TailcatCandidate struct {
	ConnBlob       string `cbor:"connBlob" json:"connBlob"`
	VirtualPort    uint16 `cbor:"virtualPort" json:"virtualPort"`
	LibraryVersion string `cbor:"libraryVersion" json:"libraryVersion"`
}

var (
	encMode cbor.EncMode
	decMode cbor.DecMode
)

func init() {
	var err error
	encMode, err = cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		panic(fmt.Sprintf("initialize canonical CBOR encoder: %v", err))
	}
	decMode, err = (cbor.DecOptions{
		DupMapKey:         cbor.DupMapKeyEnforcedAPF,
		IndefLength:       cbor.IndefLengthForbidden,
		TagsMd:            cbor.TagsForbidden,
		ExtraReturnErrors: cbor.ExtraDecErrorUnknownField,
		MaxNestedLevels:   8,
		MaxArrayElements:  128,
		MaxMapPairs:       64,
	}).DecMode()
	if err != nil {
		panic(fmt.Sprintf("initialize strict CBOR decoder: %v", err))
	}
}

// Encode validates inv and emits tcx1.<base64url(canonical-CBOR)>.
func Encode(inv InvitationV1) (string, error) {
	if err := inv.Validate(time.Time{}); err != nil {
		return "", err
	}
	payload, err := encMode.Marshal(inv)
	if err != nil {
		return "", fmt.Errorf("%w: encode CBOR: %v", ErrMalformed, err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	if len(encoded) > maxEncodedSize {
		return "", fmt.Errorf("%w: encoded invitation exceeds %d bytes", ErrMalformed, maxEncodedSize)
	}
	return Prefix + encoded, nil
}

// Decode parses a token and validates it at now. Passing a zero now skips only
// the expiry check, which is useful when displaying an archived invitation.
func Decode(token string, now time.Time) (InvitationV1, error) {
	var inv InvitationV1
	if !strings.HasPrefix(token, Prefix) {
		return inv, fmt.Errorf("%w: missing %q prefix", ErrMalformed, Prefix)
	}
	encoded := strings.TrimPrefix(token, Prefix)
	if encoded == "" || len(encoded) > maxEncodedSize {
		return inv, fmt.Errorf("%w: invalid payload size", ErrMalformed)
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return inv, fmt.Errorf("%w: invalid base64url: %v", ErrMalformed, err)
	}
	if err := decMode.Unmarshal(payload, &inv); err != nil {
		return InvitationV1{}, fmt.Errorf("%w: invalid CBOR: %v", ErrMalformed, err)
	}
	canonical, err := encMode.Marshal(inv)
	if err != nil {
		return InvitationV1{}, fmt.Errorf("%w: re-encode CBOR: %v", ErrMalformed, err)
	}
	if !bytes.Equal(payload, canonical) {
		return InvitationV1{}, ErrNonCanonical
	}
	if err := inv.Validate(now); err != nil {
		return InvitationV1{}, err
	}
	return inv, nil
}

// Validate checks the stable protocol invariants. It intentionally does not
// impose policy such as a maximum TTL; share creation owns that policy.
func (inv InvitationV1) Validate(now time.Time) error {
	if inv.Version != Version {
		return fmt.Errorf("%w: got %d, want %d", ErrVersion, inv.Version, Version)
	}
	if strings.TrimSpace(inv.ShareID) == "" {
		return fmt.Errorf("%w: empty shareId", ErrMalformed)
	}
	if inv.ExpiresAt <= 0 {
		return fmt.Errorf("%w: invalid expiresAt", ErrMalformed)
	}
	if !now.IsZero() && !now.Before(time.Unix(inv.ExpiresAt, 0)) {
		return ErrExpired
	}
	if len(inv.ServerSPKISHA256) != 32 {
		return fmt.Errorf("%w: SPKI digest is %d bytes, want 32", ErrMalformed, len(inv.ServerSPKISHA256))
	}
	if len(inv.Secret) != 32 {
		return fmt.Errorf("%w: secret is %d bytes, want 32", ErrMalformed, len(inv.Secret))
	}
	if inv.Scope != ScopeCollaborate {
		return fmt.Errorf("%w: %q", ErrScope, inv.Scope)
	}
	if len(inv.Capabilities) == 0 {
		return fmt.Errorf("%w: no capabilities", ErrMalformed)
	}
	seen := make(map[string]bool, len(inv.Capabilities))
	for _, capability := range inv.Capabilities {
		if capability == "" || seen[capability] {
			return fmt.Errorf("%w: invalid or duplicate capability %q", ErrMalformed, capability)
		}
		seen[capability] = true
	}
	if inv.LAN.MDNSInstance == "" && len(inv.LAN.Endpoints) == 0 && inv.Tailscale == nil && inv.Tailcat == nil {
		return fmt.Errorf("%w: no connection candidates", ErrMalformed)
	}
	if inv.Tailscale != nil && len(inv.Tailscale.Endpoints) == 0 {
		return fmt.Errorf("%w: empty tailscale candidate", ErrMalformed)
	}
	if inv.Tailcat != nil {
		if inv.Tailcat.ConnBlob == "" || inv.Tailcat.VirtualPort == 0 {
			return fmt.Errorf("%w: incomplete tailcat candidate", ErrMalformed)
		}
		if inv.Tailcat.LibraryVersion != TailcatLibraryV040 {
			return fmt.Errorf("%w: tailcat library %q", ErrVersion, inv.Tailcat.LibraryVersion)
		}
	}
	return nil
}
