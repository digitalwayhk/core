package public

import (
	"encoding/json"

	"github.com/digitalwayhk/core/pkg/utils"
)

func stableCacheKey(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return utils.HashCodes(string(data))
}

// InvalidateProductCache 清理全部商品查询条件的缓存。
func InvalidateProductCache() {
	if info := (&GetProducts{}).RouterInfo(); info != nil {
		info.FailureCache(nil)
	}
}

// InvalidateSupplierCaches 清理供应商及依赖供应商状态的商品缓存。
func InvalidateSupplierCaches() {
	if info := (&GetSuppliers{}).RouterInfo(); info != nil {
		info.FailureCache(nil)
	}
	InvalidateProductCache()
}

// InvalidatePaymentTypeCache 清理全部支付类型查询缓存。
func InvalidatePaymentTypeCache() {
	if info := (&GetPaymentTypes{}).RouterInfo(); info != nil {
		info.FailureCache(nil)
	}
}
