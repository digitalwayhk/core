// 本文件只在守护已判失败并停发后采集诊断；不改变正式窗口守护或验收门槛。
package shoporderscalemultiprocess

import (
	"fmt"
	integration "github.com/digitalwayhk/core/examples/integration"
	"path/filepath"
	"testing"
	"time"
)

func captureOrderLoadDiagnostics(t *testing.T, evidence string, processes []*integration.Suite, reason string) {
	t.Helper()
	results := map[string]interface{}{"trigger": reason, "at": time.Now().UTC(), "after_guardian_cancel": true}
	for i, p := range processes {
		target := filepath.Join(evidence, fmt.Sprintf("guardian-process-%d-goroutines.dump", i))
		if err := p.DumpGoroutines(target); err != nil {
			results[fmt.Sprintf("process_%d", i)] = err.Error()
		} else {
			results[fmt.Sprintf("process_%d", i)] = "CAPTURED"
		}
	}
	writeLoadJSON(t, filepath.Join(evidence, "guardian-diagnostics.json"), results)
}
