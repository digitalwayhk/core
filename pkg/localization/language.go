package localization

import "fmt"

type LocalInterface interface {
	Info(format string, a ...interface{}) Info
	Error(format string, a ...interface{}) ErrorInfo
}
type Info string

func GetInfo(sender interface{}, format string, a ...interface{}) Info {
	return Info(fmt.Sprintf(format, a...))
}

type ErrorInfo struct {
	Msg string
}

func GetErrorInfo(sender interface{}, err error) *ErrorInfo {
	return &ErrorInfo{
		Msg: err.Error(),
	}
}
