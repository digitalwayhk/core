// 本文件描述待核验的派生队列索引；它不是可丢弃的业务事实。
package reference

type queueOrphan struct {
	queueKey []byte
	dataKey  []byte
	id       uint
}
