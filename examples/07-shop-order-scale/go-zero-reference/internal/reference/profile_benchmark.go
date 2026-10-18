//go:build core_benchmark_profile

// 本文件仅用于显式benchmark构建；原生对照直接使用stdlib，不导入Core包。
package reference

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"strconv"
)

// init 只在测试端口显式配置时开启loopback性能采样。
func init() {
	port := os.Getenv("SHOP_PROFILE_PORT")
	if port == "" {
		return
	}
	number, err := strconv.Atoi(port)
	if err != nil || number <= 0 || number > 65535 {
		panic("测试profile端口无效")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		panic(err)
	}
	runtime.SetBlockProfileRate(1000000)
	runtime.SetMutexProfileFraction(10)
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
}
