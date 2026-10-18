// 本包仅由显式benchmark构建标签启用，提供本机性能取证；不是生产运维接口。
package orderprofile

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"strconv"
)

// Start 在显式测试端口开启loopback CPU/阻塞/互斥profile，生命周期由测试进程退出回收。
func Start() {
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
