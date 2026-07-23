package basedata

import (
	"strings"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/common"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/internal/store"
)

func codeOrNameExists(model interface{}, code, name string, excludeID uint) (bool, error) {
	if err := store.EnsureModel(model); err != nil {
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
		return false, common.NewBusinessError("不支持的基础资料模型")
	}
	if err := store.Get().Load(store.NewSearch(model, 500), result); err != nil {
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
