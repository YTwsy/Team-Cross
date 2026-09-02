package transport

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"
	"time"
)

func TestPinnedTLS(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	identity, err := GenerateTLSIdentity(now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	config, err := PinnedClientTLSConfig(identity.SPKISHA256[:])
	if err != nil {
		t.Fatal(err)
	}
	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{identity.Leaf}}
	if err := config.VerifyConnection(state); err != nil {
		t.Fatalf("matching certificate was rejected: %v", err)
	}
}

func TestPinnedTLSRejectsWrongKey(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	identity, err := GenerateTLSIdentity(now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	wrong := make([]byte, sha256.Size)
	config, err := PinnedClientTLSConfig(wrong)
	if err != nil {
		t.Fatal(err)
	}
	state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{identity.Leaf}}
	if err := config.VerifyConnection(state); !errors.Is(err, ErrSPKIMismatch) {
		t.Fatalf("VerifyConnection error = %v, want ErrSPKIMismatch", err)
	}
}
