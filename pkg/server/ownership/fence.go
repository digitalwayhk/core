// 本文件将应用SQL事务的fence操作约束为持锁读写，不拥有业务数据库或状态机。
package ownership

// FenceState 是应用事务内持久化的技术凭据，不可用本地缓存替代。
type FenceState struct {
	Scope string
	Key   string
	Epoch int64
	Token string `json:"-"`
}

// FenceTransaction 必须由应用当前SQL事务实现；锁从LockFence持有至事务提交/回滚。
// 首次行创建也必须串行化（例如锁定已存在的范围行），不能用无锁先查后插。
type FenceTransaction interface {
	LockFence(scope, key string) (FenceState, error)
	SaveFence(FenceState) error
}

// ActivateFence 在恢复前持业务SQL行锁切换epoch；新owner必须等旧事务实际释放锁。
func ActivateFence(tx FenceTransaction, c Credential) error {
	if tx == nil || c.Validate() != nil {
		return ErrUnknown
	}
	old, err := tx.LockFence(c.Scope, c.Key)
	if err != nil {
		return err
	}
	if old.Epoch == c.Epoch && old.Token == c.Token && old.Scope == c.Scope && old.Key == c.Key {
		return nil
	}
	if old.Epoch >= c.Epoch {
		return ErrFenced
	}
	return tx.SaveFence(FenceState{Scope: c.Scope, Key: c.Key, Epoch: c.Epoch, Token: c.Token})
}

// CheckFence 必须在同一个业务写事务内、执行副作用前调用，并保持锁至提交。
func CheckFence(tx FenceTransaction, c Credential) error {
	if tx == nil || c.Validate() != nil {
		return ErrUnknown
	}
	state, err := tx.LockFence(c.Scope, c.Key)
	if err != nil {
		return err
	}
	if state.Scope != c.Scope || state.Key != c.Key || state.Epoch != c.Epoch || state.Token != c.Token {
		return ErrFenced
	}
	return nil
}
