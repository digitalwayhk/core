package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsExista(t *testing.T) {
	dir := t.TempDir()
	if !IsExista(dir) {
		t.Fatalf("IsExista(%q) = false, want true", dir)
	}
	missing := filepath.Join(dir, "no-such-thing")
	if IsExista(missing) {
		t.Fatalf("IsExista(%q) = true, want false", missing)
	}
}

func TestIsDirAndIsFile(t *testing.T) {
	dir := t.TempDir()
	if !IsDir(dir) {
		t.Fatalf("IsDir(%q) = false, want true", dir)
	}
	// IsFile is implemented as !IsDir; on a real file it should be true.
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsDir(f) {
		t.Fatalf("IsDir on file = true, want false")
	}
	if !IsFile(f) {
		t.Fatalf("IsFile on file = false, want true")
	}
	// On non-existent path, IsDir returns false and IsFile returns true (per current impl).
	missing := filepath.Join(dir, "nope")
	if IsDir(missing) {
		t.Fatalf("IsDir(missing) should be false")
	}
}

func TestCreateDir(t *testing.T) {
	// Use TESTPATH so CreateDir is rooted in our temp directory.
	dir := t.TempDir()
	prev := TESTPATH
	TESTPATH = dir
	defer func() { TESTPATH = prev }()

	created, err := CreateDir("sub")
	if err != nil {
		t.Fatalf("CreateDir error: %v", err)
	}
	if created != filepath.Join(dir, "sub") {
		t.Fatalf("CreateDir path = %q want %q", created, filepath.Join(dir, "sub"))
	}
	if !IsDir(created) {
		t.Fatalf("CreateDir did not create directory")
	}
	// Calling again should be idempotent.
	created2, err := CreateDir("sub")
	if err != nil || created2 != created {
		t.Fatalf("CreateDir second call: path=%q err=%v", created2, err)
	}
}

func TestReadFileAndDeleteFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "data.txt")
	want := "hello, world\nline two\n"
	if err := os.WriteFile(f, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(f)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if got != want {
		t.Fatalf("ReadFile got %q want %q", got, want)
	}
	if err := DeleteFile(f); err != nil {
		t.Fatalf("DeleteFile error: %v", err)
	}
	if IsExista(f) {
		t.Fatalf("file still exists after DeleteFile")
	}
}

func TestReadFile_Missing(t *testing.T) {
	if _, err := ReadFile(filepath.Join(t.TempDir(), "no-file")); err == nil {
		t.Fatal("expected error reading missing file")
	}
}

func TestReadFile_Large(t *testing.T) {
	// Exercise the buffered loop with content larger than the 1024-byte buffer.
	dir := t.TempDir()
	f := filepath.Join(dir, "large.bin")
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(data) {
		t.Fatal("ReadFile content mismatch for large file")
	}
}

func TestGetpath_Test(t *testing.T) {
	prev := TESTPATH
	TESTPATH = "/tmp/test-marker"
	defer func() { TESTPATH = prev }()
	if got := Getpath(); got != "/tmp/test-marker" {
		t.Fatalf("Getpath under test = %q want %q", got, "/tmp/test-marker")
	}
}
