package manage

import (
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	st "github.com/digitalwayhk/core/pkg/server/types"
)

// ISubmitHook can be implemented by a ManageService controller to handle
// the Submit state transition with full type safety, eliminating the need
// to type-assert on the generic DoBefore(sender interface{}, ...) parameter.
//
// If a controller implements ISubmitHook[T], Submit.Do will call OnSubmit
// instead of IManageService.DoBefore.  The generic DoBefore hook is still
// invoked as a fallback for controllers that have not yet migrated.
//
// Usage:
//
//	func (own *ArticleManage) OnSubmit(item *Article, req types.IRequest) error {
//	    item.PublishedAt = time.Now()
//	    return own.List.Update(item)
//	}
type ISubmitHook[T pt.IModel] interface {
	OnSubmit(item *T, req st.IRequest) error
}

// IReleaseHook is the typed counterpart of ISubmitHook for the Release operation.
// Implement it to react to the Release state transition without type assertions.
//
// Usage:
//
//	func (own *ArticleManage) OnRelease(item *Article, req types.IRequest) error {
//	    // custom release logic
//	    return nil
//	}
type IReleaseHook[T pt.IModel] interface {
	OnRelease(item *T, req st.IRequest) error
}
