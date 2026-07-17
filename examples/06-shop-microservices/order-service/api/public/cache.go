package public

func InvalidatePaymentTypeCache() {
	if info := (&GetPaymentTypes{}).RouterInfo(); info != nil {
		info.FailureCache(nil)
	}
}
