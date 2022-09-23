package message

import (
	"github.com/digitalwayhk/core/pkg/message/api/manage"
	"github.com/digitalwayhk/core/pkg/server/types"
)

//type IMsgService interface {
//	Register()
//	Login(name, password string)
//}

type MsgService struct {
}

func (own *MsgService) ServiceName() string {
	return "msg"
}

func (own *MsgService) Routers() []types.IRouter {
	//todo:不优雅，应该全局或指定路径搜索继承IRoute接口的Struct自动加载，未找到方法实现
	routers := []types.IRouter{}
	//管理接口，按模块添加
	channelConfig := manage.NewChannelConfig()
	routers = append(routers, channelConfig.Routers()...)
	templateConfig := manage.NewTemplateConfig()
	routers = append(routers, templateConfig.Routers()...)
	messageLog := manage.NewMessageLog()
	routers = append(routers, messageLog.Routers()...)

	return routers
}

func (own *MsgService) SubscribeRouters() []*types.ObserveArgs {
	return []*types.ObserveArgs{
		//types.NewObserveArgs(
		//	&users.Register{},
		//	types.ObserveError,
		//	func(args *types.NotifyArgs) error {
		//		user := reflect.ValueOf(args.Instance).Interface().(map[string]interface{})
		//		otp := safe.GenerateOTP(user["userName"].(string))
		//		if otp == "" {
		//			return errors.New(config.Fail)
		//		}
		//
		//		notify := service.NewNotifyInfo(user["userName"].(string), args.TraceID)
		//		notify.TemplateId = 315107739031493
		//		notify.Data = otp
		//		return service.PreSend(notify)
		//	}),
		//types.NewObserveArgs(
		//	&users.Login{},
		//	types.ObserveResponse,
		//	func(args *types.NotifyArgs) error {
		//		log.Println("Login 接收到通知信息", args)
		//		return nil
		//	}),
	}
}
