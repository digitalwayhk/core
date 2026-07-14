package types

import (
	"errors"
	"fmt"
)

type TypeError struct {
	Code        int    `json:"code"`
	Message     string `json:"message"`
	ServiceName string `json:"-"` //服务名
	Path        string `json:"-"` // 路径
	Type        string `json:"-"` //操作类型
	Suggest     string `json:"suggest"`
	cause       error
}

func NewTypeError(serviceName, path, ot, mes string, code int) *TypeError {
	return &TypeError{
		Code:        code,
		Message:     mes,
		ServiceName: serviceName,
		Path:        path,
		Type:        ot,
		Suggest:     "",
	}
}

func NewTypeErrorWithCause(serviceName, path, ot, mes string, code int, cause error) *TypeError {
	err := NewTypeError(serviceName, path, ot, mes, code)
	err.cause = cause
	return err
}
func (own *TypeError) Error() string {
	return fmt.Sprintf("[%s] %s %s (code=%d): %s", own.ServiceName, own.Type, own.Path, own.Code, own.Message)
}

func (own *TypeError) Unwrap() error {
	if own == nil {
		return nil
	}
	return own.cause
}

func (own *TypeError) PublicErrorContract() PublicErrorContract {
	if own == nil {
		return defaultPublicErrorContract(ErrorKindInternal)
	}
	if own.cause != nil {
		var provider publicErrorProvider
		if errors.As(own.cause, &provider) {
			contract := provider.PublicErrorContract()
			if own.Code != 0 {
				contract.Code = own.Code
			}
			return contract
		}
	}
	var contract PublicErrorContract
	switch own.Type {
	case "parse", "validation":
		contract = defaultPublicErrorContract(ErrorKindValidation)
	case "do":
		contract = defaultPublicErrorContract(ErrorKindBusiness)
	default:
		contract = defaultPublicErrorContract(ErrorKindInternal)
	}
	if own.Code != 0 {
		contract.Code = own.Code
	}
	return contract
}

func (own *TypeError) GetSuggest() string {
	return ""
}
