package mq

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMQResourceName_NormalizesUnsafeCharacters 验证 Broker 资源名不包含业务载荷型字符。
func TestMQResourceName_NormalizesUnsafeCharacters(t *testing.T) {
	name := mqResourceName("digitalway/core", "order:changed/用户", 249)

	assert.Regexp(t, regexp.MustCompile(`^[A-Za-z0-9._-]+$`), name)
	assert.NotContains(t, name, "/")
	assert.NotContains(t, name, ":")
}

// TestMQResourceName_LongValuesRemainDistinct 验证截断后的长 subject 仍通过短哈希防碰撞。
func TestMQResourceName_LongValuesRemainDistinct(t *testing.T) {
	prefix := strings.Repeat("p", 80)
	a := mqResourceName(prefix, strings.Repeat("a", 300)+"x", 249)
	b := mqResourceName(prefix, strings.Repeat("a", 300)+"y", 249)

	require.NotEqual(t, a, b)
	require.LessOrEqual(t, len(a), 249)
	require.LessOrEqual(t, len(b), 249)
}

// TestMQResourceName_Stable 验证相同输入总是生成相同 Broker 资源名。
func TestMQResourceName_Stable(t *testing.T) {
	first := mqResourceName("core", "order.changed", 80)
	second := mqResourceName("core", "order.changed", 80)
	assert.Equal(t, first, second)
}
