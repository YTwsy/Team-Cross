package invite

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

func validInvitation(now time.Time) InvitationV1 {
	return InvitationV1{
		Version:          Version,
		ShareID:          "share-01",
		ExpiresAt:        now.Add(time.Hour).Unix(),
		ServerSPKISHA256: bytes.Repeat([]byte{1}, 32),
		Secret:           bytes.Repeat([]byte{2}, 32),
		Scope:            ScopeCollaborate,
		Capabilities:     []string{"view", "annotate", "send", "steer", "interrupt"},
		LAN: LANCandidates{
			MDNSInstance: "share-01",
			Endpoints:    []string{"192.168.1.7:41234"},
		},
		Tailscale: &TailnetCandidates{
			DNSName:   "host.example.ts.net.",
			Endpoints: []string{"100.64.0.1:41234"},
		},
		Tailcat: &TailcatCandidate{
			ConnBlob:       "tcopaque",
			VirtualPort:    443,
			LibraryVersion: TailcatLibraryV040,
		},
	}
}

func TestEncodeDecodeRoundTripAndCanonical(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	want := validInvitation(now)
	tokenA, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	tokenB, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	if tokenA != tokenB {
		t.Fatalf("canonical encoding changed: %q != %q", tokenA, tokenB)
	}
	got, err := Decode(tokenA, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ShareID != want.ShareID || !bytes.Equal(got.Secret, want.Secret) || got.Tailcat == nil || got.Tailcat.ConnBlob != "tcopaque" {
		t.Fatalf("round trip mismatch: %#v", got)
	}
}

func TestDecodeExpired(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	inv := validInvitation(now)
	inv.ExpiresAt = now.Add(-time.Second).Unix()
	token, err := Encode(inv)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decode(token, now)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("Decode error = %v, want ErrExpired", err)
	}
}

func TestDecodeRejectsNonCanonicalCBOR(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	inv := validInvitation(now)
	nonCanonical, err := cbor.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := encMode.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(nonCanonical, canonical) {
		t.Skip("default encoder happened to emit canonical ordering")
	}
	token := Prefix + base64.RawURLEncoding.EncodeToString(nonCanonical)
	_, err = Decode(token, now)
	if !errors.Is(err, ErrNonCanonical) {
		t.Fatalf("Decode error = %v, want ErrNonCanonical", err)
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	inv := validInvitation(now)
	token, err := Encode(inv)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, Prefix))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := cbor.Unmarshal(payload, &raw); err != nil {
		t.Fatal(err)
	}
	raw["future"] = true
	payload, err = encMode.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decode(Prefix+base64.RawURLEncoding.EncodeToString(payload), now)
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("Decode error = %v, want ErrMalformed", err)
	}
}
