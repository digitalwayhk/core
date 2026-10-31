// 本文件验证S2拒绝溢出的外部字典，避免进入上游已修复的越界编码路径。
package compat

import (
	"encoding/binary"
	"github.com/klauspost/compress/s2"
	"github.com/stretchr/testify/require"
	"testing"
)

// TestS2RejectsOverflowedDictionaryRepeat 覆盖GHSA-259r-337f-4rfw；不在进程内执行危险的Encode。
func TestS2RejectsOverflowedDictionaryRepeat(t *testing.T) {
	for _, repeat := range []uint64{1 << 63, 12687765596934209639, ^uint64(0)} {
		raw := binary.AppendUvarint(nil, repeat)
		raw = append(raw, make([]byte, s2.MinDictSize)...)
		require.Nil(t, s2.NewDict(raw), "溢出repeat必须在转换int和编码之前被拒绝")
	}
	valid := binary.AppendUvarint(nil, 0)
	valid = append(valid, make([]byte, s2.MinDictSize)...)
	require.NotNil(t, s2.NewDict(valid), "有效字典不能被一并禁用")
}
