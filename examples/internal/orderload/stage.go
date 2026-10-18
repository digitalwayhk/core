// 本文件定义独立发送进程的受控输入；凭证仅经权限受限文件传入。
package orderload

import (
	"errors"
	"net/url"
	"regexp"
	"time"
)

// Stage 配置一次发送阶段，Output是本轮独占结果目录。
type Stage struct {
	URL        string    `json:"url"`
	Token      string    `json:"token"`
	ProductID  uint      `json:"productID"`
	Rate       int       `json:"rate"`
	DurationMS int64     `json:"durationMS"`
	Origin     time.Time `json:"origin"`
	Namespace  string    `json:"namespace"`
	Output     string    `json:"output"`
}

// Validate 拒绝无效或超出计划边界的负载配置，不在错误中回显凭证。
func (s Stage) Validate() error {
	u, err := url.Parse(s.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return errors.New("负载URL无效")
	}
	if s.ProductID == 0 || s.Rate <= 0 || s.Rate > 100000 || s.DurationMS <= 0 || s.DurationMS > 600000 || s.Output == "" || s.Origin.IsZero() {
		return errors.New("负载边界参数无效")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`).MatchString(s.Namespace) {
		return errors.New("负载namespace必须为1至32位字母、数字、下划线或连字符")
	}
	return nil
}
