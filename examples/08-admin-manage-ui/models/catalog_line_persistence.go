package models

import (
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
)

// QueryByItemID 查询指定资料条目的明细行。
func (own *CatalogLine) QueryByItemID(itemID uint) ([]*CatalogLine, error) {
	if itemID == 0 {
		return nil, nil
	}
	if err := ensureCatalogLineTable(); err != nil {
		return nil, err
	}
	search := newCatalogLineSearch(own, 500)
	search.AddWhereN("ItemID", itemID)
	var lines []*CatalogLine
	err := getDataAction().Load(search, &lines)
	return lines, err
}

func newSeedLine(lineNo int, name string, quantity int, amount string) *CatalogLine {
	line := NewCatalogLine()
	line.LineNo = lineNo
	line.Name = name
	line.Quantity = quantity
	line.Amount = decimal.RequireFromString(amount)
	line.SetModelState(1)
	return line
}

func newCatalogLineSearch(model *CatalogLine, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}

func ensureCatalogLineTable() error {
	var lines []*CatalogLine
	return getDataAction().Load(newCatalogLineSearch(NewCatalogLine(), 1), &lines)
}
