package types

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/digitalwayhk/core/pkg/utils"
)

type ObserveState int

const (
	//Do方法执行前，并验证通过
	ObserveRequest ObserveState = iota
	//Do方法执行完成,获取到Response
	ObserveResponse
	//Do方法执行,但是发生错误
	ObserveError
)

type ObserveArgs struct {
	Topic          string                       //订阅路由路径
	OwnAddress     string                       //订阅者地址
	OwnProt        int                          //订阅者端口
	OwnSocketProt  int                          //订阅者socket协议端口
	State          ObserveState                 //触发时机，0：接收到请求时，1：请求完成时，2：异常发生时
	ServiceName    string                       //服务名称
	ReceiveService string                       //接收者服务名称（订阅注册时，订阅者服务名称）
	CallBack       func(args *NotifyArgs) error `json:"-"` //回调函数
	Router         IRouter                      `json:"-"` //订阅路由
	IsOk           bool                         //是否注册成功
	Error          error                        `json:"-"` //注册错误信息
	IsUnSub        bool                         `json:"-"` //是否取消订阅
}

func (own *ObserveArgs) GetHash() string {
	return utils.HashCodes(own.Topic, own.ServiceName, strconv.Itoa((int(own.State))))
}

type TargetInfo struct {
	TargetAddress    string
	TargetService    string
	TargetPort       int
	TargetSocketPort int
	TargetPath       string
	TargetToken      string //目标服务的token
}

// NewObserveArgs 创建对路由的订阅,router为订阅路由,state为订阅的触发时机，callback为回调函数
func NewObserveArgs(router IRouter, state ObserveState, callBack func(args *NotifyArgs) error) *ObserveArgs {
	info := router.RouterInfo()
	return &ObserveArgs{
		Topic:       info.GetPath(),
		ServiceName: info.GetServiceName(),
		Router:      router,
		State:       state,
		CallBack:    callBack,
	}
}

type NotifyArgs struct {
	TraceID           string //跟踪ID
	SendService       string
	ReceiveAddress    string
	ReceiveService    string
	ReceiveProt       int //接收者端口
	ReceiveSocketProt int //接收者socket协议端口
	Topic             string
	Instance          interface{}
	Response          interface{}
	State             ObserveState //触发时机，0：接收到请求时，1：请求完成时，2：异常发生时
}

func (own *NotifyArgs) GetInstance(instance interface{}) error {
	if utils.IsPtr(instance) {
		items, err := json.Marshal(own.Instance)
		if err != nil {
			return err
		}
		err = json.Unmarshal(items, instance)
		return err
	}
	return errors.New("instance must be ptr")
}
func (own *NotifyArgs) GetResponse(resp IResponse) error {
	if utils.IsPtr(resp) {
		items, err := json.Marshal(own.Response)
		if err != nil {
			return err
		}
		err = json.Unmarshal(items, resp)
		return err
	}
	return errors.New("response must be ptr")
}
func (own *ObserveArgs) Notify(args *NotifyArgs) error {
	if own.CallBack != nil {
		return own.CallBack(args)
	}
	return nil
}

func (own *ObserveArgs) NewNotifyArgs(instance interface{}, resp IResponse) *NotifyArgs {
	args := &NotifyArgs{
		ReceiveAddress:    own.OwnAddress,
		ReceiveProt:       own.OwnProt,
		ReceiveSocketProt: own.OwnSocketProt,
		SendService:       own.ServiceName,
		ReceiveService:    own.ReceiveService,
		Topic:             own.Topic,
		State:             own.State,
		Instance:          instance,
		Response:          resp,
	}
	return args
}

type Publisher interface {
	Publish(topic string, messages ...interface{}) error
	Close() error
}
type Subscriber interface {
	Subscribe(ctx context.Context, topic string) (<-chan interface{}, error)
	Close() error
}
