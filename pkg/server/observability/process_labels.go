package observability

import (
	"fmt"
	"sync"
)

var (
	processLabelMu      sync.Mutex
	processService      string
	processInstanceID   string
	processLabelsReady  bool
)

// RegisterProcessLabels 注册进程级 service / service_instance_id。
// 生产上每个进程通常只承载一个服务实例；相同值可重复调用。
// 多服务同进程（测试或 all-in-one）时保留首次注册，后续不同值返回 ErrProcessLabelsConflict。
var ErrProcessLabelsConflict = fmt.Errorf("process labels already registered")

func RegisterProcessLabels(service, serviceInstanceID string) error {
	service = NormalizeServiceLabel(service)
	serviceInstanceID = NormalizeServiceLabel(serviceInstanceID)
	if service == "unknown" || serviceInstanceID == "unknown" {
		return fmt.Errorf("process labels require non-empty service and service_instance_id")
	}
	if !IsSafePromLabel(service) || !IsSafePromLabel(serviceInstanceID) {
		return fmt.Errorf("process labels contain unsafe characters")
	}

	processLabelMu.Lock()
	defer processLabelMu.Unlock()
	if processLabelsReady {
		if processService != service || processInstanceID != serviceInstanceID {
			return fmt.Errorf("%w: %s/%s", ErrProcessLabelsConflict, processService, processInstanceID)
		}
		return nil
	}
	processService = service
	processInstanceID = serviceInstanceID
	processLabelsReady = true
	return nil
}

// ProcessLabels 返回已注册的进程标签；未注册时 ok=false。
func ProcessLabels() (service, serviceInstanceID string, ok bool) {
	processLabelMu.Lock()
	defer processLabelMu.Unlock()
	if !processLabelsReady {
		return "", "", false
	}
	return processService, processInstanceID, true
}

// ResetProcessLabelsForTest 仅测试使用。
func ResetProcessLabelsForTest() {
	processLabelMu.Lock()
	defer processLabelMu.Unlock()
	processService = ""
	processInstanceID = ""
	processLabelsReady = false
}
