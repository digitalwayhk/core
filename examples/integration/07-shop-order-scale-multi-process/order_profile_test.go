// 本文件仅在代表性短窗采集CPU/阻塞/互斥证据；正式容量不启用采样。
package shoporderscalemultiprocess

import (
	"fmt"
	integration "github.com/digitalwayhk/core/examples/integration"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func startOrderProfiles(t *testing.T, evidence string, processes []*integration.Suite) func() {
	t.Helper()
	var wg sync.WaitGroup
	for i, p := range processes {
		port := p.Environment["SHOP_PROFILE_PORT"]
		if port == "" {
			continue
		}
		wg.Go(func() {
			client := http.Client{Timeout: 30 * time.Second}
			for _, kind := range []string{"profile?seconds=10", "block", "mutex"} {
				resp, err := client.Get("http://127.0.0.1:" + port + "/debug/pprof/" + kind)
				if err != nil {
					t.Errorf("profile process=%d: %v", i, err)
					return
				}
				if resp.StatusCode != 200 {
					_ = resp.Body.Close()
					t.Errorf("profile process=%d status=%d", i, resp.StatusCode)
					return
				}
				name := kind
				if kind == "profile?seconds=10" {
					name = "cpu"
				}
				file, err := os.OpenFile(filepath.Join(evidence, fmt.Sprintf("process-%d-%s.pprof", i, name)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					_ = resp.Body.Close()
					t.Error(err)
					return
				}
				_, err = io.Copy(file, io.LimitReader(resp.Body, 32<<20))
				_ = resp.Body.Close()
				closeErr := file.Close()
				if err != nil {
					t.Error(err)
				}
				if closeErr != nil {
					t.Error(closeErr)
				}
			}
		})
	}
	return wg.Wait
}
