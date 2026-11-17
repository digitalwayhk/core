// 本文件定义归属组工作的实例级总回调并发与预取预算，不定义业务状态或持久队列。
package ownership

// GroupWorkBudgetOptions限定同实例共用的固定worker与全部预留/排队/执行身份数。
type GroupWorkBudgetOptions struct {
	Workers  int
	Prefetch int
}

func (o GroupWorkBudgetOptions) validate() error {
	if o.Workers < 1 || o.Workers > 64 || o.Prefetch < o.Workers || o.Prefetch > 1024 {
		return ErrCapacity
	}
	return nil
}
