// 本文件定义发送器读取的最小兼容响应外壳。
package orderload

import "encoding/json"

type responseEnvelope struct {
	Success      bool            `json:"success"`
	ErrorMessage string          `json:"errorMessage"`
	Data         json.RawMessage `json:"data"`
}
