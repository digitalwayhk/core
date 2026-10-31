package private

import (
	"context"
	"os"

	"github.com/digitalwayhk/core/examples/10-reliable-multi-instance/business"
	"github.com/digitalwayhk/core/examples/10-reliable-multi-instance/contract"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type Progress struct{}

func (*Progress) Parse(types.IRequest) error      { return nil }
func (*Progress) Validation(types.IRequest) error { return nil }
func (*Progress) Do(req types.IRequest) (interface{}, error) {
	uid, _ := req.GetUser()
	sc := router.GetContext(contract.WorkerService)
	return map[string]interface{}{"pid": os.Getpid(), "instanceID": sc.ServiceInstanceID, "receipts": business.Current.ForUser(uid), "mq": sc.MQManager.RuntimeMetricSnapshot(context.Background())}, nil
}
func (o *Progress) RouterInfo() *types.RouterInfo { return router.DefaultRouterInfo(o) }
