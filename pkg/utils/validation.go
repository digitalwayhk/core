// 本文件提供邮箱和手机号文本的兼容校验函数。
package utils

import (
	"net/mail"
	"strconv"
	"strings"
)

// IsEmail 报告文本能否被标准库解析为邮件地址。
func IsEmail(device string) bool {
	_, err := mail.ParseAddress(device)
	return err == nil
}

// IsMobile 按旧的“区号 空格 号码”格式判断手机号文本。
func IsMobile(device string) bool {
	if !strings.Contains(device, " ") {
		return false
	}
	phone := strings.Split(device, " ")
	areaCode, phoneNo := phone[0], phone[1]
	if strings.HasPrefix(areaCode, "+") {
		areaCode = TrimFirstRune(areaCode)
	}
	phoneNo = areaCode + phoneNo
	_, err := strconv.Atoi(phoneNo)
	return err == nil
}
