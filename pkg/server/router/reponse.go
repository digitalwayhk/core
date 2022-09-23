package router

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

//Response 接口响应
type Response struct {
	err          error
	TraceID      string        `json:"traceid"`      //追踪ID
	ErrorCode    int           `json:"errorCode"`    //错误状态码
	ErrorMessage string        `json:"errorMessage"` //错误消息
	Success      bool          `json:"success"`      //成功标志
	Duration     time.Duration `json:"duration"`     //请求处理的时长(纳秒)
	Data         interface{}   `json:"data"`         //业务数据
	Host         string        `json:"host"`         //后端服务的host
	ShowType     int           `json:"showType"`     //展示类型
}

//NewResponse 一次请求的响应
func (own *Request) NewResponse(data interface{}, err error) types.IResponse {
	suc := true
	msg := ""
	if err != nil {
		suc = false
		msg = err.Error()
	}
	res := &Response{
		err:          err,
		TraceID:      own.GetTraceId(),
		ErrorCode:    200,
		ErrorMessage: msg,
		Data:         data,
		Success:      suc,
		Duration:     time.Since(own.startTime),
	}
	if own.service != nil {
		res.Host = own.service.Config.RunIp
	}
	return res
}

func (own *Response) GetData(instanceType ...interface{}) interface{} {
	if len(instanceType) > 0 {
		ti := instanceType[0]
		if utils.IsPtr(ti) {
			items, _ := json.Marshal(own.Data)
			json.Unmarshal(items, ti)
			return ti
		}
	}
	return own.Data
}

func (own *Response) GetMessage() string {
	return own.ErrorMessage
}
func (own *Response) GetSuccess() bool {
	return own.Success
}
func (own *Response) GetError() error {
	if own.err == nil {
		return errors.New(own.ErrorMessage)
	}
	if te, ok := own.err.(*types.TypeError); ok {
		own.ErrorCode = te.Code
		own.ErrorMessage = te.Message + te.Suggest
	}
	return own.err
}
