package oltp

import "testing"

func TestSqliteMmapSizeUsesBoundedDefaultAndExplicitOverride(t *testing.T) {
	sqlite := NewSqlite()
	if got := sqlite.effectiveMmapSize(); got != 256<<20 {
		t.Fatalf("默认 mmap_size 应为 256MiB，实际为 %d", got)
	}

	sqlite.MmapSize = 64 << 20
	if got := sqlite.effectiveMmapSize(); got != 64<<20 {
		t.Fatalf("显式 mmap_size 未生效，实际为 %d", got)
	}

	sqlite.MmapSize = -1
	if got := sqlite.effectiveMmapSize(); got != 0 {
		t.Fatalf("负值应关闭 mmap，实际为 %d", got)
	}
}
