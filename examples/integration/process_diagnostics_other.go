//go:build !linux && !darwin && !freebsd

package integration

import "fmt"

// DumpGoroutines 在不支持go-zero SIGUSR1的平台显式报告不可用。
func (s *Suite) DumpGoroutines(string) error { return fmt.Errorf("当前平台不支持SIGUSR1诊断") }
