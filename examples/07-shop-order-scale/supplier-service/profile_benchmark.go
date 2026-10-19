//go:build core_benchmark_profile

// 本文件只在显式诊断构建中启用本机profile。
package supplierservice

import "github.com/digitalwayhk/core/examples/internal/orderprofile"

func init() { orderprofile.Start() }
