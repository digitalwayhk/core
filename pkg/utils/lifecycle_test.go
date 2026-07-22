// 本文件约束 utils 不通过 init 接管整进程的内存和 GC 生命周期。
package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageDoesNotOwnReflectionMemoryMonitor(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		for _, forbidden := range []string{"startReflectionMemoryMonitor", "runtime.GC()"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s 仍包含无 owner 的生命周期逻辑 %q", file, forbidden)
			}
		}
	}
}
