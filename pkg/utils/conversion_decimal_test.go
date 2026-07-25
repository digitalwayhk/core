// 本文件验证 Manage 查询使用的 decimal.Decimal 转换保持精度并显式返回错误。
package utils

import (
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestAnyToTypeDataPreservesDecimalText(t *testing.T) {
	got, err := AnyToTypeData("12345678901234567890.1234500", reflect.TypeOf(decimal.Decimal{}))
	require.NoError(t, err)
	value, ok := got.(decimal.Decimal)
	require.True(t, ok)
	require.True(t, value.Equal(decimal.RequireFromString("12345678901234567890.1234500")))
}

func TestAnyToTypeDataRejectsInvalidDecimalText(t *testing.T) {
	_, err := AnyToTypeData("not-a-decimal", reflect.TypeOf(decimal.Decimal{}))
	require.Error(t, err)
}
