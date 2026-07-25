// 本文件验证 Manage View 查询元数据到持久化 SearchItem 的类型转换。
package view

import (
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSearchItemToSearchItemConvertsJSONNumberToDecimal(t *testing.T) {
	search := (&SearchItem{
		View: &ViewModel{Fields: []*FieldModel{{
			Field:     "price",
			PropField: "Price",
			Type:      "decimal",
			FieldType: reflect.TypeOf(decimal.Decimal{}),
		}}},
		WhereList: []*SearchWhere{{
			Name:   "price",
			Symbol: ">",
			Value:  float64(15),
		}},
	}).ToSearchItem()

	require.Len(t, search.WhereList, 1)
	where := search.WhereList[0]
	require.Equal(t, "Price", where.Column)
	require.Equal(t, ">", where.Symbol)
	value, ok := where.Value.(decimal.Decimal)
	require.True(t, ok)
	require.True(t, value.Equal(decimal.NewFromInt(15)))
}
