package types

import "testing"

func TestRouterInfoFreezeRejectsMetadataMutation(t *testing.T) {
	info := &RouterInfo{
		Path:        "/api/test/frozen",
		ServiceName: "test",
		Auth:        true,
		Method:      "POST",
		PathType:    PrivateType,
	}
	info.SetInstance(&plainPoolRouter{})
	info.Freeze("test")

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("冻结后 SetInstance 必须 fail closed")
			}
		}()
		info.SetInstance(&plainPoolRouter{})
	}()

	info.Path = "/api/test/changed"
	defer func() {
		if recover() == nil {
			t.Fatal("冻结后的公开元数据被修改时，读取必须 fail closed")
		}
	}()
	_ = info.GetPath()
}
