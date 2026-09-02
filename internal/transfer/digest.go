package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// TreeDigest includes ignored and untracked files, unlike git diff. Continuing
// a sealed Round must not silently use later dependency/configuration changes.
func TreeDigest(root string) (string, error) {
	if err := AuditTree(root); err != nil {
		return "", err
	}
	hash := sha256.New()
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root {
			return nil
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(relative), info.Mode())
		if entry.IsDir() {
			return nil
		}
		var content []byte
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filename)
			if err != nil {
				return err
			}
			content = []byte(target)
		} else {
			input, err := os.Open(filename)
			if err != nil {
				return err
			}
			defer input.Close()
			fileHash := sha256.New()
			if _, err := io.Copy(fileHash, input); err != nil {
				return err
			}
			content = fileHash.Sum(nil)
		}
		fmt.Fprintf(hash, "%x\x00", content)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
