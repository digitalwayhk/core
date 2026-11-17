// 本文件提供显式共享当前读fence；原Check/Activate的排他合同不变。
package ownership

// SharedFenceTransaction必须在当前业务SQL事务中共享锁定已初始化的唯一fence行，保持至真实commit/rollback。
// LockFenceShared必须是锁定当前读，不得使用普通快照、缓存或无锁先查后插。
type SharedFenceTransaction interface {
	LockFenceShared(scope, key string) (FenceState, error)
}

// CheckFenceShared显式校验共享读凭据；新Activate仍必须使用原排他FenceTransaction。
func CheckFenceShared(tx SharedFenceTransaction, c Credential) error {
	if tx == nil || c.Validate() != nil {
		return ErrUnknown
	}
	s, err := tx.LockFenceShared(c.Scope, c.Key)
	if err != nil {
		return err
	}
	if s.Scope != c.Scope || s.Key != c.Key || s.Epoch != c.Epoch || s.Token != c.Token {
		return ErrFenced
	}
	return nil
}
