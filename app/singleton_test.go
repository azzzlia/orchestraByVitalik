package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSingleInstance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	release, err := claimSingleInstance()
	require.NoError(t, err)
	path := filepath.Join(home, ".claude-squad", "squad.pid")
	_, err = os.Stat(path)
	require.NoError(t, err)
	// A stale pid (no such process) does not block.
	require.NoError(t, os.WriteFile(path, []byte("999999\n"), 0600))
	r2, err := claimSingleInstance()
	require.NoError(t, err)
	r2()
	release()
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}
