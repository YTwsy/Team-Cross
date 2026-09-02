package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"
)

var ErrSPKIMismatch = errors.New("server SPKI fingerprint mismatch")

// TLSIdentity is an ephemeral self-signed Ed25519 certificate and the digest
// clients pin. The certificate is an identity binding, not a public PKI name.
type TLSIdentity struct {
	Certificate tls.Certificate
	Leaf        *x509.Certificate
	SPKISHA256  [sha256.Size]byte
}

func GenerateTLSIdentity(now, expiresAt time.Time) (*TLSIdentity, error) {
	if expiresAt.IsZero() || !expiresAt.After(now) {
		return nil, errors.New("TLS identity expiry must be after creation")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate Ed25519 key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	var serial *big.Int
	for serial == nil || serial.Sign() == 0 {
		serial, err = rand.Int(rand.Reader, serialLimit)
		if err != nil {
			return nil, fmt.Errorf("generate certificate serial: %w", err)
		}
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Team Cross ephemeral share"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     expiresAt.Add(5 * time.Minute),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"teamcross.invalid"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse generated certificate: %w", err)
	}
	certificate := tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  privateKey,
		Leaf:        leaf,
	}
	return &TLSIdentity{
		Certificate: certificate,
		Leaf:        leaf,
		SPKISHA256:  sha256.Sum256(leaf.RawSubjectPublicKeyInfo),
	}, nil
}

func (id *TLSIdentity) ServerConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{id.Certificate},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"http/1.1"},
	}
}

// PinnedClientTLSConfig authenticates the exact ephemeral public key. PKI
// verification is intentionally disabled because the self-signed certificate
// is distributed through the invitation's SPKI digest.
func PinnedClientTLSConfig(want []byte) (*tls.Config, error) {
	if len(want) != sha256.Size {
		return nil, fmt.Errorf("SPKI digest is %d bytes, want %d", len(want), sha256.Size)
	}
	wantCopy := append([]byte(nil), want...)
	return &tls.Config{
		InsecureSkipVerify: true, // replaced by the pin check below
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"http/1.1"},
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("%w: peer did not present a certificate", ErrSPKIMismatch)
			}
			got := sha256.Sum256(state.PeerCertificates[0].RawSubjectPublicKeyInfo)
			if !equalBytes(got[:], wantCopy) {
				return fmt.Errorf("%w: got %s", ErrSPKIMismatch, hex.EncodeToString(got[:]))
			}
			return nil
		},
	}, nil
}

func DialPinnedTLS(ctx context.Context, raw net.Conn, pin []byte) (*tls.Conn, error) {
	config, err := PinnedClientTLSConfig(pin)
	if err != nil {
		return nil, err
	}
	conn := tls.Client(raw, config)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return conn, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var different byte
	for i := range a {
		different |= a[i] ^ b[i]
	}
	return different == 0
}
