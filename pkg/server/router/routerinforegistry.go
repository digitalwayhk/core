package router

import (
	"fmt"
	"sync"

	"github.com/digitalwayhk/core/pkg/server/types"
)

type routerInfoRegistrationIndex struct {
	mu     sync.RWMutex
	routes map[string]map[string]*types.RouterInfo
}

func newRouterInfoRegistrationIndex() *routerInfoRegistrationIndex {
	return &routerInfoRegistrationIndex{
		routes: make(map[string]map[string]*types.RouterInfo),
	}
}

func routerInfoRegistrationKey(pack, instanceName, structName string) string {
	return pack + "\x00" + instanceName + "\x00" + structName
}

func (r *routerInfoRegistrationIndex) register(info *types.RouterInfo) {
	if info == nil || info.GetServiceName() == "" {
		return
	}
	serviceName := info.GetServiceName()
	key := routerInfoRegistrationKey(info.GetPackPath(), info.GetInstanceName(), info.GetStructName())
	r.mu.Lock()
	owners := r.routes[key]
	if owners == nil {
		owners = make(map[string]*types.RouterInfo)
		r.routes[key] = owners
	}
	owners[serviceName] = info
	r.mu.Unlock()
}

func (r *routerInfoRegistrationIndex) unregister(info *types.RouterInfo) {
	if info == nil || info.GetServiceName() == "" {
		return
	}
	serviceName := info.GetServiceName()
	key := routerInfoRegistrationKey(info.GetPackPath(), info.GetInstanceName(), info.GetStructName())
	r.mu.Lock()
	owners := r.routes[key]
	if owners[serviceName] == info {
		delete(owners, serviceName)
		if len(owners) == 0 {
			delete(r.routes, key)
		}
	}
	r.mu.Unlock()
}

func (r *routerInfoRegistrationIndex) resolve(pack, instanceName, structName string) *types.RouterInfo {
	key := routerInfoRegistrationKey(pack, instanceName, structName)
	r.mu.RLock()
	owners := r.routes[key]
	if len(owners) == 0 {
		r.mu.RUnlock()
		return nil
	}
	if len(owners) > 1 {
		r.mu.RUnlock()
		panic(fmt.Sprintf("router %s is registered by multiple services; resolve it through a ServiceContext", instanceName))
	}
	var info *types.RouterInfo
	for _, registered := range owners {
		info = registered
	}
	r.mu.RUnlock()
	return info
}

var registeredRouterInfos = newRouterInfoRegistrationIndex()
