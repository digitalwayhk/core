// 本包仅由显式benchmark构建标签启用，提供本机性能取证；不是生产运维接口。
package orderprofile

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"runtime/trace"
	"strconv"
	"sync"
	"time"
)

// Start 在显式测试端口开启loopback CPU/阻塞/互斥profile，生命周期由测试进程退出回收。
func Start() func() {
	port := os.Getenv("SHOP_PROFILE_PORT")
	if port == "" {
		return func() {}
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
	previousMutexRate := runtime.SetMutexProfileFraction(10)
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	var recorder *trace.FlightRecorder
	if os.Getenv("SHOP_FLIGHT_RECORDER") == "1" {
		// MaxBytes是Go运行时提示值，采集方仍须限制快照下载大小。
		recorder = trace.NewFlightRecorder(trace.FlightRecorderConfig{MinAge: 30 * time.Second, MaxBytes: 32 << 20})
		if err := recorder.Start(); err != nil {
			_ = listener.Close()
			panic(err)
		}
		mux.HandleFunc("/debug/pprof/flight", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", http.MethodGet)
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Trailer", "X-Trace-Error")
			if _, err := recorder.WriteTo(w); err != nil {
				w.Header().Set("X-Trace-Error", err.Error())
			}
		})
	}
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = server.Close()
			if recorder != nil {
				recorder.Stop()
			}
			runtime.SetBlockProfileRate(0)
			runtime.SetMutexProfileFraction(previousMutexRate)
		})
	}
}
