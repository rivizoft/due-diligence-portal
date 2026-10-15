package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialsEncryptionSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "storage.key")
	c := newCredentialCipher(path)
	creds := Credentials{AccessKeyID: "test-access", SecretAccessKey: "test-secret"}
	one, err := c.Seal(creds)
	require.NoError(t, err)
	two, err := c.Seal(creds)
	require.NoError(t, err)
	require.NotEqual(t, one, two)
	require.NotContains(t, string(one), creds.SecretAccessKey)
	got, err := newCredentialCipher(path).Open(one)
	require.NoError(t, err)
	require.Equal(t, creds, got)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	one[len(one)-1] ^= 1
	_, err = c.Open(one)
	require.Error(t, err)
	require.NoError(t, os.Remove(path))
	_, err = c.Open(two)
	require.Error(t, err)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}

func TestCredentialsRejectUnsafeKeyFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "permissions", "short"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "storage.key")
			target := filepath.Join(dir, "target")
			require.NoError(t, os.WriteFile(target, make([]byte, 32), 0600))
			switch kind {
			case "symlink":
				require.NoError(t, os.Symlink(target, path))
			case "permissions":
				require.NoError(t, os.WriteFile(path, make([]byte, 32), 0600))
				require.NoError(t, os.Chmod(path, 0644))
			case "short":
				require.NoError(t, os.WriteFile(path, []byte("bad"), 0600))
			}
			_, err := newCredentialCipher(path).Seal(Credentials{AccessKeyID: "access", SecretAccessKey: "secret"})
			require.Error(t, err)
		})
	}
}
