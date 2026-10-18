// 本文件定义原生合批队列中每个请求的独立回执。
package reference

type submission struct {
	order *Order
	done  chan outcome
}
