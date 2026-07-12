package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsFileDistinguishesMissingFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing.db")
	file := filepath.Join(root, "existing.db")
	requireNoError(t, os.WriteFile(file, []byte("test"), 0o600))

	if IsFile(missing) {
		t.Fatalf("不存在的路径不应被识别为文件: %s", missing)
	}
	if IsFile(root) {
		t.Fatalf("目录不应被识别为文件: %s", root)
	}
	if !IsFile(file) {
		t.Fatalf("普通文件应被识别为文件: %s", file)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
