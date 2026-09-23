package models

import persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"

// QueryByItemID 查询指定资料条目的规格参数。
func (own *CatalogSpec) QueryByItemID(itemID uint) ([]*CatalogSpec, error) {
	if itemID == 0 {
		return nil, nil
	}
	if err := ensureCatalogSpecTable(); err != nil {
		return nil, err
	}
	search := newCatalogSpecSearch(own, 500)
	search.AddWhereN("ItemID", itemID)
	var specs []*CatalogSpec
	err := getDataAction().Load(search, &specs)
	return specs, err
}

func newSeedSpec(sortNo int, name, value string) *CatalogSpec {
	spec := NewCatalogSpec()
	spec.SortNo = sortNo
	spec.SpecName = name
	spec.SpecValue = value
	spec.SetModelState(1)
	return spec
}

func newCatalogSpecSearch(model *CatalogSpec, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}

func ensureCatalogSpecTable() error {
	var specs []*CatalogSpec
	return getDataAction().Load(newCatalogSpecSearch(NewCatalogSpec(), 1), &specs)
}
