// 本文件保留启动核对时的原始版本，用于远端证明后的本地CAS删除。
package reference

type pendingFactOrphan struct {
	key   []byte
	value []byte
	order *Order
}
