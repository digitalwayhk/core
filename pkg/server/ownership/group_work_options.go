// 本文件定义原Runtime显式组模式；发现只返回组身份，真实组首在worker接纳后读取。
package ownership

import "context"

// GroupWorkRequest给发现回调有限额度和当前禁止重入的组；回调不得claim/执行原件。
type GroupWorkRequest struct {
	Limit          int
	ExcludedGroups []string
}

// GroupWorkOptions在同scope/key内按不透明group独立执行，不能与旧Step同时声明。
type GroupWorkOptions struct {
	Budget     *GroupWorkBudget
	NextGroups func(context.Context, Credential, GroupWorkRequest) ([]string, error)
	StepGroup  func(context.Context, Credential, string) (bool, error)
}
