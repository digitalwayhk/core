package rest

import (
	"net/http"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 定义标准错误码
const (
	StatusSuccess             = 200 // 成功
	StatusBadRequest          = 400 // 请求参数错误
	StatusUnauthorized        = 401 // 未认证
	StatusForbidden           = 403 // 无权限
	StatusNotFound            = 404 // 资源不存在
	StatusConflict            = 409 // 资源冲突
	StatusUnprocessableEntity = 422 // 业务逻辑错误（推荐）
	StatusTooManyRequests     = 429 // 请求过于频繁
	StatusInternalServerError = 500 // 服务器内部错误
	StatusServiceUnavailable  = 503 // 服务不可用
)

// 错误类型定义
type ErrorType int

const (
	ErrorTypeValidation ErrorType = iota // 验证错误 -> 400
	ErrorTypeBusiness                    // 业务错误 -> 422
	ErrorTypeConflict                    // 冲突错误 -> 409
	ErrorTypeSystem                      // 系统错误 -> 500
)

// 扩展 Response 接口
type ErrorResponse struct {
	Success bool         `json:"success"`
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Data    interface{}  `json:"data,omitempty"`
	Error   *ErrorDetail `json:"error,omitempty"`
}

type ErrorDetail struct {
	Type    string            `json:"type"`              // 错误类型
	Field   string            `json:"field,omitempty"`   // 错误字段
	Details map[string]string `json:"details,omitempty"` // 详细信息
}

// 统一的响应处理
func HandleResponse(w http.ResponseWriter, res types.IResponse) {
	if res.GetSuccess() {
		// 成功响应 200
		httpx.OkJson(w, res)
		return
	}

	// 失败响应 - 根据错误类型返回不同状态码
	statusCode := determineStatusCode(res)
	httpx.WriteJson(w, statusCode, res)
}

// 根据错误类型确定状态码
func determineStatusCode(res types.IResponse) int {
	err := res.GetError()
	if err == nil {
		return StatusInternalServerError
	}

	errMsg := err.Error()

	// 🔧 根据错误信息判断类型
	switch {
	case contains(errMsg, "validation", "invalid", "required", "format"):
		return StatusBadRequest // 400 - 验证错误

	case contains(errMsg, "not found", "does not exist"):
		return StatusNotFound // 404 - 资源不存在

	case contains(errMsg, "conflict", "duplicate", "already exists"):
		return StatusConflict // 409 - 冲突

	case contains(errMsg, "insufficient", "not enough", "exceed", "业务"):
		return StatusUnprocessableEntity // 422 - 业务逻辑错误 ⭐

	case contains(errMsg, "unauthorized", "token", "authentication"):
		return StatusUnauthorized // 401 - 未认证

	case contains(errMsg, "forbidden", "permission", "access denied"):
		return StatusForbidden // 403 - 无权限

	default:
		return StatusUnprocessableEntity // 422 - 默认业务错误 ⭐
	}
}

// 辅助函数：检查字符串是否包含关键词
func contains(str string, keywords ...string) bool {
	lowerStr := strings.ToLower(str)
	for _, keyword := range keywords {
		if strings.Contains(lowerStr, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

// 写入错误响应
func writeErrorResponse(w http.ResponseWriter, statusCode int, message string, err error) {
	response := &ErrorResponse{
		Success: false,
		Code:    statusCode,
		Message: message,
	}

	if err != nil {
		response.Error = &ErrorDetail{
			Type:    getErrorType(statusCode),
			Details: map[string]string{"error": err.Error()},
		}
	}

	httpx.WriteJson(w, statusCode, response)
}

// 获取错误类型名称
func getErrorType(statusCode int) string {
	switch statusCode {
	case StatusBadRequest:
		return "ValidationError"
	case StatusUnauthorized:
		return "AuthenticationError"
	case StatusForbidden:
		return "AuthorizationError"
	case StatusNotFound:
		return "NotFoundError"
	case StatusConflict:
		return "ConflictError"
	case StatusUnprocessableEntity:
		return "BusinessError"
	case StatusInternalServerError:
		return "SystemError"
	default:
		return "UnknownError"
	}
}
