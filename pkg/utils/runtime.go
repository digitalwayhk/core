// 本文件提供测试运行环境判断。
package utils

import "flag"

// IsTest 报告当前进程是否注册了 Go test 或 testify 的测试参数。
func IsTest() bool {
	return flag.Lookup("test.v") != nil || flag.Lookup("testify.m") != nil
}
