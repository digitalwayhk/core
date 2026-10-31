package private

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/10-reliable-multi-instance/contract"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type Submit struct {
	RequestID   string `json:"requestID"`
	Sequence    int    `json:"sequence"`
	DelayMillis int    `json:"delayMillis"`
	FailFirst   bool   `json:"failFirst"`
}

func (o *Submit) Parse(req types.IRequest) error { return req.Bind(o) }
func (o *Submit) Validation(req types.IRequest) error {
	uid, _ := req.GetUser()
	if uid == "" || strings.TrimSpace(o.RequestID) == "" || len(o.RequestID) > 64 || o.Sequence <= 0 || o.DelayMillis < 0 || o.DelayMillis > 5000 {
		return types.NewPublicError(types.ErrorKindValidation, types.PublicCodeValidation, "requestID、正数 sequence 或 delayMillis(0..5000) 无效", nil)
	}
	return nil
}
func (o *Submit) Do(req types.IRequest) (interface{}, error) {
	uid, _ := req.GetUser()
	job := contract.Job{UserID: uid, Sequence: o.Sequence, DelayMillis: o.DelayMillis, FailFirst: o.FailFirst}
	data, err := json.Marshal(job)
	if err != nil {
		return nil, err
	}
	env := event.NewEnvelope(contract.ProducerService, contract.EventType, data)
	digest := sha256.Sum256([]byte(uid + "\x00" + o.RequestID))
	env.ID = hex.EncodeToString(digest[:])
	env.IdempotencyKey = env.ID
	env.Subject = contract.Subject
	env.ShardKey = "user:" + uid
	sc := router.GetContext(contract.ProducerService)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = sc.EventBridge.Publish(ctx, contract.Subject, env); err != nil {
		return nil, err
	}
	return struct {
		EventID  string `json:"eventID"`
		Accepted bool   `json:"accepted"`
	}{env.ID, true}, nil
}
func (o *Submit) RouterInfo() *types.RouterInfo { return router.DefaultRouterInfo(o) }
