// 本文件定义显式原生GC维护的实际结果；重写次数不等于已释放字节或准入恢复。
package nosql

// StorageReclaimResult报告有限次Badger原生GC，NoRewrite表示本次无可重写文件，不证明库为空。
type StorageReclaimResult struct {
	Attempts       int
	RewrittenFiles int
	NoRewrite      bool
}
