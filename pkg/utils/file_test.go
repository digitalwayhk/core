package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsExista_Exists(t *testing.T) {
	// Use a temp file
	f, err := os.CreateTemp("", "core-utils-test-*")
	require.NoError(t, err)
	f.Close()
	defer os.Remove(f.Name())

	assert.True(t, IsExista(f.Name()))
}

func TestIsExista_NotExists(t *testing.T) {
	assert.False(t, IsExista("/tmp/this-file-does-not-exist-xyz123"))
}

func TestIsDir_TrueForDir(t *testing.T) {
	assert.True(t, IsDir(os.TempDir()))
}

func TestIsDir_FalseForFile(t *testing.T) {
	f, err := os.CreateTemp("", "core-utils-dir-test-*")
	require.NoError(t, err)
	f.Close()
	defer os.Remove(f.Name())

	assert.False(t, IsDir(f.Name()))
}

func TestIsDir_FalseForMissing(t *testing.T) {
	assert.False(t, IsDir("/tmp/missing-dir-xyz999"))
}

func TestIsFile_TrueForFile(t *testing.T) {
	f, err := os.CreateTemp("", "core-utils-isfile-*")
	require.NoError(t, err)
	f.Close()
	defer os.Remove(f.Name())

	assert.True(t, IsFile(f.Name()))
}

func TestIsFile_FalseForDir(t *testing.T) {
	assert.False(t, IsFile(os.TempDir()))
}

func TestReadFile(t *testing.T) {
	f, err := os.CreateTemp("", "core-utils-read-*")
	require.NoError(t, err)
	content := "hello from test"
	_, err = f.WriteString(content)
	require.NoError(t, err)
	f.Close()
	defer os.Remove(f.Name())

	result, err := ReadFile(f.Name())
	require.NoError(t, err)
	assert.Equal(t, content, result)
}

func TestReadFile_NotFound(t *testing.T) {
	_, err := ReadFile("/tmp/definitely-missing-file-xyz.txt")
	assert.Error(t, err)
}

func TestDeleteFile(t *testing.T) {
	f, err := os.CreateTemp("", "core-utils-delete-*")
	require.NoError(t, err)
	f.Close()
	name := f.Name()

	err = DeleteFile(name)
	require.NoError(t, err)
	assert.False(t, IsExista(name))
}

func TestDeleteFile_NotFound(t *testing.T) {
	err := DeleteFile("/tmp/missing-for-delete-xyz.txt")
	assert.Error(t, err)
}

func TestCreateDir(t *testing.T) {
	// Set TESTPATH so Getpath() uses it
	tmpBase, err := os.MkdirTemp("", "core-createdir-base-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpBase)

	TESTPATH = tmpBase
	created, err := CreateDir("subdir")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmpBase, "subdir"), created)
	assert.True(t, IsDir(created))

	// Calling again on existing dir should succeed
	created2, err := CreateDir("subdir")
	require.NoError(t, err)
	assert.Equal(t, created, created2)
}
