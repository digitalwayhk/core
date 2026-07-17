package public

// InvalidateProductCache 清理所有商品查询条件的缓存。
func InvalidateProductCache() {
	if info := (&GetProducts{}).RouterInfo(); info != nil {
		info.FailureCache(nil)
	}
}

// InvalidateSupplierCaches 清理供应商缓存以及依赖供应商状态的商品缓存。
func InvalidateSupplierCaches() {
	if info := (&GetSuppliers{}).RouterInfo(); info != nil {
		info.FailureCache(nil)
	}
	InvalidateProductCache()
}
