// Package cluster 提供服务发现和水平扩展身份分配能力。
package cluster

import (
	"context"
	"fmt"
)

type contextMachineIDAllocator interface {
	AllocateMachineID(ctx context.Context, serviceName string, dataCenterID int64) (int64, error)
}

// AllocateMachineID 从当前发现 Provider 中查找可用 MachineID。
func AllocateMachineID(ctx context.Context, provider DiscoveryProvider, serviceName string, dataCenterID, maxMachineID int64) (int64, error) {
	if provider == nil {
		return -1, fmt.Errorf("cluster: provider is nil")
	}
	if maxMachineID < 0 {
		return -1, fmt.Errorf("cluster: max MachineID must be >= 0")
	}
	if local, ok := provider.(*LocalProvider); ok {
		id := local.AllocateMachineID(serviceName, dataCenterID, maxMachineID)
		if id < 0 {
			return -1, fmt.Errorf("cluster: all MachineID slots are full for service=%s DataCenterID=%d", serviceName, dataCenterID)
		}
		return id, nil
	}
	if allocator, ok := provider.(contextMachineIDAllocator); ok {
		id, err := allocator.AllocateMachineID(ctx, serviceName, dataCenterID)
		if err != nil {
			return -1, err
		}
		if id > maxMachineID {
			return -1, fmt.Errorf("cluster: allocated MachineID=%d exceeds max=%d for service=%s DataCenterID=%d", id, maxMachineID, serviceName, dataCenterID)
		}
		return id, nil
	}
	nodes, err := provider.List(ctx, serviceName, NodeStatusRunning)
	if err != nil {
		return -1, err
	}
	used := make(map[int64]bool, len(nodes))
	for _, node := range nodes {
		if node != nil && node.ServiceName == serviceName && node.DataCenterID == dataCenterID {
			used[node.MachineID] = true
		}
	}
	for id := int64(0); id <= maxMachineID; id++ {
		if !used[id] {
			return id, nil
		}
	}
	return -1, fmt.Errorf("cluster: all MachineID slots are full for service=%s DataCenterID=%d", serviceName, dataCenterID)
}
