package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSymbolConstants_Values 验证常量值与预期字符串完全匹配。
func TestSymbolConstants_Values(t *testing.T) {
	assert.Equal(t, "isnull", SymbolIsNull)
	assert.Equal(t, "isnotnull", SymbolIsNotNull)
	assert.Equal(t, "like", SymbolLike)
	assert.Equal(t, "notlike", SymbolNotLike)
	assert.Equal(t, "left", SymbolLeft)
	assert.Equal(t, "right", SymbolRight)
	assert.Equal(t, "in", SymbolIn)
	assert.Equal(t, "notin", SymbolNotIn)
	assert.Equal(t, "between", SymbolBetween)
}

// TestSymbolMap_ContainsAllSymbols 验证 symbolMap 包含所有常量 key。
func TestSymbolMap_ContainsAllSymbols(t *testing.T) {
	symbols := []string{
		SymbolIsNull, SymbolIsNotNull,
		SymbolLike, SymbolNotLike,
		SymbolLeft, SymbolRight,
		SymbolIn, SymbolNotIn,
		SymbolBetween,
	}
	for _, sym := range symbols {
		_, ok := symbolMap[sym]
		assert.True(t, ok, "symbolMap missing key: %s", sym)
	}
}

// TestWhereItem_LikeSetsSymbol 验证 WhereItem.Like() 使用常量而非硬编码字符串。
func TestWhereItem_LikeSetsSymbol(t *testing.T) {
	w := &WhereItem{}
	w.Like()
	assert.Equal(t, SymbolLike, w.Symbol)
}

// TestWhereItem_NotLikeSetsSymbol 验证 WhereItem.NotLike() 使用常量。
func TestWhereItem_NotLikeSetsSymbol(t *testing.T) {
	w := &WhereItem{}
	w.NotLike()
	assert.Equal(t, SymbolNotLike, w.Symbol)
}

// TestWhereItem_InSetsSymbol 验证 WhereItem.In() 使用常量。
func TestWhereItem_InSetsSymbol(t *testing.T) {
	w := &WhereItem{}
	w.In()
	assert.Equal(t, SymbolIn, w.Symbol)
}

// TestWhereItem_NotInSetsSymbol 验证 WhereItem.NotIn() 使用常量。
func TestWhereItem_NotInSetsSymbol(t *testing.T) {
	w := &WhereItem{}
	w.NotIn()
	assert.Equal(t, SymbolNotIn, w.Symbol)
}

// TestWhereItem_BetweenSetsSymbol 验证 WhereItem.Between() 使用常量。
func TestWhereItem_BetweenSetsSymbol(t *testing.T) {
	w := &WhereItem{}
	w.Between()
	assert.Equal(t, SymbolBetween, w.Symbol)
}

// TestWhereItem_IsNullSetsSymbol 验证 WhereItem.IsNull() 使用常量。
func TestWhereItem_IsNullSetsSymbol(t *testing.T) {
	w := &WhereItem{}
	w.IsNull()
	assert.Equal(t, SymbolIsNull, w.Symbol)
}

// TestWhereItem_IsNotNullSetsSymbol 验证 WhereItem.IsNotNull() 使用常量。
func TestWhereItem_IsNotNullSetsSymbol(t *testing.T) {
	w := &WhereItem{}
	w.IsNotNull()
	assert.Equal(t, SymbolIsNotNull, w.Symbol)
}
