package models

import (
	"strings"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// newSearch 创建模型查询使用的统一分页条件。
func newSearch(model interface{}, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}

func codeOrNameExists(model interface{}, code, name string, excludeID uint) (bool, error) {
	action := cloneDataAction()
	if err := ensureModelWith(action, model); err != nil {
		return false, err
	}
	var result interface{}
	switch model.(type) {
	case *Supplier:
		model = NewSupplier()
		result = &[]*Supplier{}
	case *Product:
		model = NewProduct()
		result = &[]*Product{}
	case *PaymentType:
		model = NewPaymentType()
		result = &[]*PaymentType{}
	default:
		return false, NewBusinessError("不支持的基础资料模型")
	}
	if err := action.Load(newSearch(model, 500), result); err != nil {
		return false, err
	}
	code = strings.ToLower(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	switch items := result.(type) {
	case *[]*Supplier:
		for _, item := range *items {
			if item != nil && item.ID != excludeID && (strings.EqualFold(item.Code, code) || item.Name == name) {
				return true, nil
			}
		}
	case *[]*Product:
		for _, item := range *items {
			if item != nil && item.ID != excludeID && (strings.EqualFold(item.Code, code) || item.Name == name) {
				return true, nil
			}
		}
	case *[]*PaymentType:
		for _, item := range *items {
			if item != nil && item.ID != excludeID && (strings.EqualFold(item.Code, code) || item.Name == name) {
				return true, nil
			}
		}
	}
	return false, nil
}
