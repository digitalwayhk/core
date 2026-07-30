package router

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// Response 接口响应
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

// NewResponse 一次请求的响应
func (own *Request) NewResponse(data interface{}, err error) types.IResponse {
	if own.service != nil {
		if newres, ok := own.service.Service.Instance.(types.INewResponse); ok {
			res := newres.NewResponse(data, err)
			if setres, ok := res.(types.ISetResponseData); ok {
				setres.SetTraceId(own.GetTraceId())
				setres.SetDuration(time.Since(own.startTime))
				setres.SetHost(own.service.RuntimeAddress())
			}
			return res
		}
	}
	suc := true
	res := &Response{
		err:       err,
		TraceID:   own.GetTraceId(),
		ErrorCode: 200,
		Data:      data,
		Success:   suc,
		Duration:  time.Since(own.startTime),
	}
	if err != nil {
		res.Success = false
		contract := types.ResolvePublicError(err)
		res.ErrorCode = contract.Code
		res.ErrorMessage = contract.Message
	}
	if own.service != nil {
		res.Host = own.service.RuntimeAddress()
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
		if kind, ok := responseErrorKind(own.ErrorCode); ok {
			return types.NewPublicError(
				kind,
				own.ErrorCode,
				own.ErrorMessage,
				errors.New("downstream service returned an error"),
			)
		}
		return errors.New(own.ErrorMessage)
	}
	return own.err
}

func responseErrorKind(code int) (types.ErrorKind, bool) {
	switch code {
	case 600, 700, types.PublicCodeValidation:
		return types.ErrorKindValidation, true
	case 800, types.PublicCodeBusiness:
		return types.ErrorKindBusiness, true
	case types.PublicCodeUnauthenticated, types.PublicCodeRefreshInvalid, types.PublicCodeRefreshRevoked:
		return types.ErrorKindUnauthenticated, true
	case types.PublicCodeForbidden:
		return types.ErrorKindForbidden, true
	case types.PublicCodeNotFound:
		return types.ErrorKindNotFound, true
	case types.PublicCodeConflict:
		return types.ErrorKindConflict, true
	case types.PublicCodeRateLimited:
		return types.ErrorKindRateLimited, true
	case types.PublicCodeUnavailable, types.PublicCodeAuthDependencyUnavailable:
		return types.ErrorKindUnavailable, true
	case types.PublicCodeInternal:
		return types.ErrorKindInternal, true
	default:
		return "", false
	}
}
func (own *Response) SetCode(code int) {
	own.ErrorCode = code
}
func (own *Response) SetPublicError(code int, message string) {
	own.ErrorCode = code
	own.ErrorMessage = message
}
func (own *Response) SetTraceId(traceId string) {
	own.TraceID = traceId
}
func (own *Response) SetDuration(duration time.Duration) {
	own.Duration = duration
}
func (own *Response) SetHost(host string) {
	own.Host = host
}
