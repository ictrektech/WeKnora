package utils

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrVNoteCredentialKey = errors.New("V-Note credential encryption is unavailable")

// This key is independent of JWT signing, presigned URLs, and model secrets.
// VOS sets a file inside its persistent runtime volume; standalone operators
// may supply an explicit key. Legacy SYSTEM_AES_KEY remains a read candidate.
func VNoteCredentialKeys() ([][]byte, error) {
	var keys [][]byte
	if raw := os.Getenv("HYBRAG_VNOTE_ENCRYPTION_KEY"); raw != "" {
		if len(raw) != 32 || raw == "your-32-byte-long-encryption-key!" {
			return nil, ErrVNoteCredentialKey
		}
		keys = append(keys, []byte(raw))
	}
	if path := strings.TrimSpace(os.Getenv("HYBRAG_VNOTE_KEY_FILE")); path != "" {
		key, err := loadOrCreateVNoteKey(path)
		if err != nil && len(keys) == 0 {
			return nil, ErrVNoteCredentialKey
		}
		if err == nil {
			keys = append(keys, key)
		}
	}
	for _, raw := range []string{os.Getenv("SYSTEM_AES_KEY"), os.Getenv("HYBRAG_VNOTE_ENCRYPTION_KEY_PREVIOUS"), os.Getenv("SYSTEM_AES_KEY_PREVIOUS")} {
		if len(raw) == 32 && raw != "your-32-byte-long-encryption-key!" {
			keys = append(keys, []byte(raw))
		}
	}
	if len(keys) == 0 {
		return nil, ErrVNoteCredentialKey
	}
	return keys, nil
}

func loadOrCreateVNoteKey(path string) ([]byte, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrVNoteCredentialKey
	}
	read := func() ([]byte, error) {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, ErrVNoteCredentialKey
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, 67))
		if err != nil || len(raw) > 66 {
			return nil, ErrVNoteCredentialKey
		}
		key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(key) != 32 {
			return nil, ErrVNoteCredentialKey
		}
		return key, nil
	}
	if key, err := read(); !os.IsNotExist(err) {
		return key, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".vnote-key-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(hex.EncodeToString(key)); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	// Publish a fully written file without overwriting another instance's key.
	if err := os.Link(f.Name(), path); err != nil {
		if os.IsExist(err) {
			return read()
		}
		return nil, err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err := parent.Sync(); err != nil {
		return nil, err
	}
	return key, nil
}
