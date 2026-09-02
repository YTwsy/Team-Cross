package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"teamcross/internal/domain"
)

// ObjectStore is an immutable, content-addressed filesystem store. Objects are
// sharded by the first two hexadecimal characters of their SHA-256 digest.
type ObjectStore struct {
	root string
}

func NewObjectStore(root string) (*ObjectStore, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create object store: %w", err)
	}
	return &ObjectStore{root: root}, nil
}

func (o *ObjectStore) Path(hash string) (string, error) {
	if len(hash) != sha256.Size*2 {
		return "", fmt.Errorf("invalid sha256 object id %q", hash)
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", fmt.Errorf("invalid sha256 object id: %w", err)
	}
	return filepath.Join(o.root, hash[:2], hash[2:]), nil
}

func (o *ObjectStore) Put(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	dst, _ := o.Path(hash)
	if _, err := os.Stat(dst); err == nil {
		return hash, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat object: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", fmt.Errorf("create object shard: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".object-*")
	if err != nil {
		return "", fmt.Errorf("create object temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", fmt.Errorf("chmod object: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write object: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("sync object: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close object: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		if _, statErr := os.Stat(dst); statErr == nil {
			return hash, nil
		}
		return "", fmt.Errorf("publish object: %w", err)
	}
	return hash, nil
}

func (o *ObjectStore) Get(hash string) ([]byte, error) {
	path, err := o.Path(hash)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	return b, nil
}

func (o *ObjectStore) Open(hash string) (io.ReadCloser, error) {
	path, err := o.Path(hash)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open object: %w", err)
	}
	return f, nil
}

func (s *Store) PutObject(ctx context.Context, data []byte, mime string) (domain.Object, error) {
	if mime == "" {
		mime = "application/octet-stream"
	}
	hash, err := s.objects.Put(data)
	if err != nil {
		return domain.Object{}, err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO objects(hash, size, mime, created_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(hash) DO NOTHING`, hash, len(data), mime, millis(now))
	if err != nil {
		return domain.Object{}, fmt.Errorf("record object: %w", err)
	}
	return domain.Object{Hash: hash, Size: int64(len(data)), MIME: mime, CreatedAt: now}, nil
}

func (s *Store) GetObject(ctx context.Context, hash string) ([]byte, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM objects WHERE hash = ?`, hash).Scan(&exists); err != nil {
		return nil, mapNotFound(err)
	}
	return s.objects.Get(hash)
}

func (s *Store) ObjectStore() *ObjectStore { return s.objects }
