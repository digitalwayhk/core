//go:build core_benchmark_profile

// 本文件只在显式性能实验构建中启用本机profile；正式容量窗口不带此标签。
package orderservice

import "github.com/digitalwayhk/core/examples/internal/orderprofile"

func init() { orderprofile.Start() }
