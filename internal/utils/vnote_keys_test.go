package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVNoteKeyIsPersistentAndConcurrentCreationCannotReplaceIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("VOS file keys require POSIX permissions; use an explicit key on Windows")
	}
	path := filepath.Join(t.TempDir(), "integration", "key")
	keys := make([][]byte, 12)
	errs := make([]error, 12)
	var wg sync.WaitGroup
	for i := range keys {
		wg.Add(1)
		go func(i int) { defer wg.Done(); keys[i], errs[i] = loadOrCreateVNoteKey(path) }(i)
	}
	wg.Wait()
	for i := range keys {
		require.NoError(t, errs[i])
		require.Len(t, keys[i], 32)
		require.Equal(t, keys[0], keys[i])
	}
	again, err := loadOrCreateVNoteKey(path)
	require.NoError(t, err)
	require.Equal(t, keys[0], again)
	stat, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), stat.Mode().Perm())
}

func TestVNoteKeyRefusesCorruptionSymlinksAndPublicPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("VOS file keys require POSIX permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	require.NoError(t, os.WriteFile(path, []byte("broken"), 0600))
	_, err := loadOrCreateVNoteKey(path)
	require.Error(t, err, "never replace an existing key")
	unchanged, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "broken", string(unchanged))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(path, link))
	_, err = loadOrCreateVNoteKey(link)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("a", 64)), 0600))
	require.NoError(t, os.Chmod(path, 0644))
	_, err = loadOrCreateVNoteKey(path)
	require.Error(t, err)
}

func TestVNoteKeyDoesNotChangeExistingSecretsAndRetainsLegacyReadKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("VOS file keys require POSIX permissions")
	}
	t.Setenv("HYBRAG_VNOTE_ENCRYPTION_KEY", "")
	t.Setenv("HYBRAG_VNOTE_KEY_FILE", filepath.Join(t.TempDir(), "key"))
	legacy := strings.Repeat("k", 32)
	t.Setenv("SYSTEM_AES_KEY", legacy)
	t.Setenv("JWT_SECRET", "existing-session-secret")
	keys, err := VNoteCredentialKeys()
	require.NoError(t, err)
	require.NotEqual(t, []byte(legacy), keys[0])
	require.Equal(t, []byte(legacy), keys[1])
	require.Equal(t, legacy, os.Getenv("SYSTEM_AES_KEY"))
	require.Equal(t, "existing-session-secret", os.Getenv("JWT_SECRET"))
}
