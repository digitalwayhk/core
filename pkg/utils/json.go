// 本文件提供保留旧错误语义的 JSON 序列化辅助函数。
package utils

import "encoding/json"

// PrintObj 将对象序列化为 JSON；序列化失败时沿用旧行为返回空字符串。
func PrintObj(o interface{}) string {
	b, _ := json.Marshal(o)
	return string(b)
}
