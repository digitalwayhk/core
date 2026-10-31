// 本文件把应用观测回调异常归为未知，不记录异常正文或将其伪造为零待办。
package ownership

import "context"

func (r *Runtime) observe(ctx context.Context, c Credential) (observation Observation, err error) {
	defer func() {
		if recover() != nil {
			observation = Observation{}
			err = ErrUnknown
		}
	}()
	return r.options.Observe(ctx, c)
}
