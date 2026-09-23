package models

import (
	"strings"
	"time"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
)

// FindByID 查找单个资料条目。
func (own *CatalogItem) FindByID(id uint) (*CatalogItem, error) {
	if id == 0 {
		return nil, nil
	}
	search := newCatalogItemSearch(own, 1)
	search.AddWhereN("ID", id)
	var items []*CatalogItem
	if err := getDataAction().Load(search, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items[0], nil
}

// CodeExists 检查编码是否已被其他条目占用。
func (own *CatalogItem) CodeExists(code string, excludeID uint) (bool, error) {
	search := newCatalogItemSearch(own, 8)
	hashed := NewCatalogItem()
	hashed.Code = code
	search.AddWhereN("Hashcode", hashed.GetHash())
	var items []*CatalogItem
	if err := getDataAction().Load(search, &items); err != nil {
		return false, err
	}
	for _, item := range items {
		if item != nil && item.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

// SaveLines 按 modelState 持久化条目子表：1 新增、2 修改、3 删除。
func (own *CatalogItem) SaveLines() error {
	if own == nil || own.ID == 0 {
		return nil
	}
	if err := ensureCatalogLineTable(); err != nil {
		return err
	}
	for index, line := range own.Lines {
		if line == nil {
			continue
		}
		line.NewModel()
		line.ItemID = own.ID
		if line.LineNo == 0 {
			line.LineNo = index + 1
		}
		line.Name = strings.TrimSpace(line.Name)
		line.SetHashcode(line.GetHash())
		switch line.GetModelState() {
		case 3:
			if line.ID == 0 {
				continue
			}
			if err := getDataAction().Delete(line); err != nil {
				return err
			}
		case 1:
			if line.ID != 0 {
				if err := getDataAction().Update(line); err != nil {
					return err
				}
				continue
			}
			if err := line.AddValid(); err != nil {
				return err
			}
			if err := getDataAction().Insert(line); err != nil {
				return err
			}
		default:
			if line.ID == 0 {
				if err := line.AddValid(); err != nil {
					return err
				}
				if err := getDataAction().Insert(line); err != nil {
					return err
				}
				continue
			}
			if err := line.UpdateValid(nil); err != nil {
				return err
			}
			if err := getDataAction().Update(line); err != nil {
				return err
			}
		}
	}
	return nil
}

// SaveSpecs 按 modelState 持久化规格参数子表：1 新增、2 修改、3 删除。
func (own *CatalogItem) SaveSpecs() error {
	if own == nil || own.ID == 0 {
		return nil
	}
	if err := ensureCatalogSpecTable(); err != nil {
		return err
	}
	for index, spec := range own.Specs {
		if spec == nil {
			continue
		}
		spec.NewModel()
		spec.ItemID = own.ID
		if spec.SortNo == 0 {
			spec.SortNo = index + 1
		}
		spec.SpecName = strings.TrimSpace(spec.SpecName)
		spec.SpecValue = strings.TrimSpace(spec.SpecValue)
		spec.SetHashcode(spec.GetHash())
		switch spec.GetModelState() {
		case 3:
			if spec.ID == 0 {
				continue
			}
			if err := getDataAction().Delete(spec); err != nil {
				return err
			}
		case 1:
			if spec.ID != 0 {
				if err := getDataAction().Update(spec); err != nil {
					return err
				}
				continue
			}
			if err := spec.AddValid(); err != nil {
				return err
			}
			if err := getDataAction().Insert(spec); err != nil {
				return err
			}
		default:
			if spec.ID == 0 {
				if err := spec.AddValid(); err != nil {
					return err
				}
				if err := getDataAction().Insert(spec); err != nil {
					return err
				}
				continue
			}
			if err := spec.UpdateValid(nil); err != nil {
				return err
			}
			if err := getDataAction().Update(spec); err != nil {
				return err
			}
		}
	}
	return nil
}

// SaveChildren 同时持久化明细行和规格参数。
func (own *CatalogItem) SaveChildren() error {
	if err := own.SaveLines(); err != nil {
		return err
	}
	return own.SaveSpecs()
}

// LoadChildren 读取子表；空表时写入演示数据，供展开行和编辑表单展示多页签。
func (own *CatalogItem) LoadChildren() error {
	if own == nil || own.ID == 0 {
		return nil
	}
	lines, err := NewCatalogLine().QueryByItemID(own.ID)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		own.Lines = DefaultLinesFor(own.Code)
		if err := own.SaveLines(); err != nil {
			return err
		}
	} else {
		own.Lines = lines
	}
	specs, err := NewCatalogSpec().QueryByItemID(own.ID)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		own.Specs = DefaultSpecsFor(own.Code)
		if err := own.SaveSpecs(); err != nil {
			return err
		}
	} else {
		own.Specs = specs
	}
	return nil
}

// DefaultCatalogItems 按已存在分类构造首次空表时的演示条目。
func DefaultCatalogItems(categories []*Category) []*CatalogItem {
	if len(categories) == 0 {
		return nil
	}
	electronics := categories[0]
	books := electronics
	if len(categories) > 1 {
		books = categories[1]
	}
	published := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	phone := newSeedItem("sku-phone-case", "演示手机壳", electronics, KindAccessory, "19.90", 12, true, "secret-1", "用于验证外键、子表和字段控件", &published)
	book := newSeedItem("sku-go-book", "演示 Go 实战", books, KindBook, "88.00", 3, true, "secret-2", "用于验证枚举筛选和高级搜索", &published)
	return []*CatalogItem{phone, book}
}

// DefaultLinesFor 返回指定条目编码的演示明细。
func DefaultLinesFor(code string) []*CatalogLine {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "sku-phone-case":
		return []*CatalogLine{
			newSeedLine(1, "黑色", 8, "9.90"),
			newSeedLine(2, "白色", 4, "10.00"),
		}
	case "sku-go-book":
		return []*CatalogLine{newSeedLine(1, "纸质版", 3, "88.00")}
	default:
		return nil
	}
}

// DefaultSpecsFor 返回指定条目编码的演示规格参数。
func DefaultSpecsFor(code string) []*CatalogSpec {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "sku-phone-case":
		return []*CatalogSpec{
			newSeedSpec(1, "材质", "TPU"),
			newSeedSpec(2, "兼容机型", "iPhone 15"),
		}
	case "sku-go-book":
		return []*CatalogSpec{
			newSeedSpec(1, "开本", "16开"),
			newSeedSpec(2, "页数", "320"),
		}
	default:
		return nil
	}
}

func newSeedItem(code, name string, category *Category, kind int, price string, stock int, enabled bool, secret, note string, publishedAt *time.Time) *CatalogItem {
	item := NewCatalogItem()
	item.Code = code
	item.Name = name
	item.Kind = kind
	item.Price = decimal.RequireFromString(price)
	item.Stock = stock
	item.Enabled = enabled
	item.Secret = secret
	item.Note = note
	item.PublishedAt = publishedAt
	if category != nil {
		item.CategoryID = category.ID
		item.Category = category
	}
	return item
}

func newCatalogItemSearch(model *CatalogItem, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}
