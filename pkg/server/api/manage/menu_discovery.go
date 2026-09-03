package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	pathpkg "path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type serviceContextRequest interface {
	GetService() *router.ServiceContext
}

type remoteMenuSnapshotQuery func(context.Context, types.IRequest, *router.ServiceContext, string, *types.TargetInfo) (*smodels.MenuServiceSnapshot, error)

const menuDiscoveryTimeout = 2 * time.Minute
const maxMenuSnapshotResponseBytes int64 = 8 << 20

const (
	maxMenuSnapshotRouters  = 4096
	maxMenuSnapshotReports  = 1024
	maxMenuTitleBytes       = 1024
	maxMenuDescriptionBytes = 4096
)

var menuPathSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// discoverMenuServiceSnapshots 聚合当前进程与集群中所有运行服务的菜单快照。
//
// SystemManage 所在的 server 上下文可以使用本地 Provider，因此发现必须由
// 同进程的业务 ServiceContext 执行。远程服务必须返回自身进程内的
// QueryRouters 快照；任一发现或校验失败时整体失败闭合。
func discoverMenuServiceSnapshots(req types.IRequest) ([]*smodels.MenuServiceSnapshot, error) {
	local := router.GetContexts()
	if source := menuRequestServiceContext(req); source != nil && source.Service != nil {
		local[source.Service.Name] = source
	}
	return discoverMenuServiceSnapshotsFrom(req, local, queryRemoteMenuServiceSnapshot)
}

func discoverMenuServiceSnapshotsFrom(req types.IRequest, local map[string]*router.ServiceContext, query remoteMenuSnapshotQuery) ([]*smodels.MenuServiceSnapshot, error) {
	if req == nil {
		return nil, errors.New("menu discovery request unavailable")
	}
	if query == nil {
		return nil, errors.New("menu discovery query unavailable")
	}

	contexts := make(map[string]*router.ServiceContext, len(local))
	contextNames := make([]string, 0, len(local))
	serviceNames := make(map[string]struct{}, len(local))
	for _, sc := range local {
		if sc == nil || sc.Service == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(sc.Service.Name))
		if name == "" || name == "server" {
			continue
		}
		contexts[name] = sc
		contextNames = append(contextNames, name)
		serviceNames[name] = struct{}{}
	}
	sort.Strings(contextNames)
	if len(contextNames) == 0 {
		return nil, errors.New("menu discovery business service context unavailable")
	}

	ctx, cancel := context.WithTimeout(menuRequestContext(req), menuDiscoveryTimeout)
	defer cancel()
	authorities := make(map[string]*router.ServiceContext)
	remoteNodes := make(map[string]map[string]*cluster.NodeInfo)
	for _, contextName := range contextNames {
		sc := contexts[contextName]
		_, nodes, err := sc.ClusterProviderSnapshot(ctx, "", cluster.NodeStatusRunning)
		if err != nil {
			return nil, fmt.Errorf("discover menu services via %s: %w", contextName, err)
		}
		for _, node := range nodes {
			if node == nil {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(node.ServiceName))
			if name == "" || name == "server" {
				continue
			}
			serviceNames[name] = struct{}{}
			if authorities[name] == nil {
				authorities[name] = sc
			}
			if remoteNodes[name] == nil {
				remoteNodes[name] = make(map[string]*cluster.NodeInfo)
			}
			key := node.ID
			if key == "" {
				key = fmt.Sprintf("%s:%d:%d", node.Address, node.Port, node.GRPCPort)
			}
			remoteNodes[name][key] = node
		}
	}

	names := make([]string, 0, len(serviceNames))
	for name := range serviceNames {
		names = append(names, name)
	}
	sort.Strings(names)
	snapshots := make([]*smodels.MenuServiceSnapshot, 0, len(names))
	for _, name := range names {
		var snapshot *smodels.MenuServiceSnapshot
		var canonical []byte
		localContext := contexts[name]
		if localContext != nil {
			snapshot = public.NewMenuServiceSnapshot(localContext)
			canonical, _ = json.Marshal(snapshot)
		}
		nodeMap := remoteNodes[name]
		nodeKeys := make([]string, 0, len(nodeMap))
		for key, node := range nodeMap {
			if localContext != nil && isMenuLocalNode(localContext, node) {
				continue
			}
			nodeKeys = append(nodeKeys, key)
		}
		sort.Strings(nodeKeys)
		if len(nodeKeys) == 0 {
			if snapshot != nil {
				snapshots = append(snapshots, snapshot)
				continue
			}
			return nil, fmt.Errorf("discover menu service %s: running node unavailable", name)
		}
		authority := authorities[name]
		if authority == nil {
			return nil, fmt.Errorf("discover menu service %s: discovery authority unavailable", name)
		}
		for _, key := range nodeKeys {
			node := nodeMap[key]
			target := &types.TargetInfo{
				TargetService: name, TargetAddress: node.Address,
				TargetPort: node.Port, TargetGRPCPort: node.GRPCPort,
			}
			current, err := query(ctx, req, authority, name, target)
			if err != nil {
				return nil, err
			}
			if err := validateMenuServiceSnapshot(current, name); err != nil {
				return nil, fmt.Errorf("discover menu service %s: %w", name, err)
			}
			encoded, err := json.Marshal(current)
			if err != nil {
				return nil, fmt.Errorf("discover menu service %s: encode snapshot: %w", name, err)
			}
			if canonical != nil && string(encoded) != string(canonical) {
				return nil, fmt.Errorf("discover menu service %s: running node snapshots differ", name)
			}
			snapshot, canonical = current, encoded
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func isMenuLocalNode(sc *router.ServiceContext, node *cluster.NodeInfo) bool {
	if sc == nil || sc.Service == nil || node == nil {
		return false
	}
	if sc.ServiceInstanceID != "" && node.ServiceInstanceID != "" {
		return sc.ServiceInstanceID == node.ServiceInstanceID
	}
	if sc.Config == nil {
		return false
	}
	return node.Address == sc.RuntimeAddress() && node.Port == sc.Config.Port &&
		node.GRPCPort == sc.Config.Transport.GRPC.Port
}

func menuRequestServiceContext(req types.IRequest) *router.ServiceContext {
	if req == nil {
		return nil
	}
	if bound, ok := req.(serviceContextRequest); ok && bound.GetService() != nil {
		return bound.GetService()
	}
	return router.GetContext(req.ServiceName())
}

func menuRequestContext(req types.IRequest) context.Context {
	if httpReq, ok := req.(types.IRequestHttp); ok && httpReq.GetHttpRequest() != nil {
		return httpReq.GetHttpRequest().Context()
	}
	return context.Background()
}

// menuQueryRouters 固定远程菜单发现的路由元数据，避免同进程多个
// QueryRouters 注册者导致 RouterInfo 归属不唯一。
type menuQueryRouters struct {
	public.QueryRouters
	info *types.RouterInfo
}

func newMenuQueryRouters(serviceName string) *menuQueryRouters {
	query := &menuQueryRouters{QueryRouters: public.QueryRouters{ApiType: 3, ForMenu: true}}
	query.info = &types.RouterInfo{
		Path: "/api/servermanage/queryrouters", ServiceName: serviceName,
		PathType: types.ServerManagerType, Method: http.MethodPost,
		StructName: "QueryRouters", InstanceName: "QueryRouters",
	}
	query.info.SetInstance(query)
	return query
}

func (q *menuQueryRouters) RouterInfo() *types.RouterInfo { return q.info }
func (*menuQueryRouters) MaxResponseBytes() int64         { return maxMenuSnapshotResponseBytes }

func queryRemoteMenuServiceSnapshot(ctx context.Context, req types.IRequest, authority *router.ServiceContext, serviceName string, target *types.TargetInfo) (*smodels.MenuServiceSnapshot, error) {
	if authority == nil || authority.Service == nil {
		return nil, fmt.Errorf("discover menu service %s: discovery authority unavailable", serviceName)
	}
	response, err := authority.CallTargetNode(ctx, req.GetTraceId(), newMenuQueryRouters(serviceName), target)
	if err != nil {
		return nil, fmt.Errorf("discover menu service %s: %w", serviceName, err)
	}
	if response == nil {
		return nil, fmt.Errorf("discover menu service %s: empty response", serviceName)
	}
	if !response.GetSuccess() {
		responseErr := response.GetError()
		if responseErr == nil {
			responseErr = errors.New(response.GetMessage())
		}
		return nil, fmt.Errorf("discover menu service %s: %w", serviceName, responseErr)
	}
	raw, err := json.Marshal(response.GetData())
	if err != nil {
		return nil, fmt.Errorf("discover menu service %s: encode response: %w", serviceName, err)
	}
	snapshot := &smodels.MenuServiceSnapshot{}
	if err := json.Unmarshal(raw, snapshot); err != nil {
		return nil, fmt.Errorf("discover menu service %s: decode response: %w", serviceName, err)
	}
	return snapshot, nil
}

func validateMenuServiceSnapshot(snapshot *smodels.MenuServiceSnapshot, serviceName string) error {
	serviceName = strings.ToLower(strings.TrimSpace(serviceName))
	if !menuPathSegmentPattern.MatchString(serviceName) {
		return fmt.Errorf("invalid service name %q", serviceName)
	}
	if snapshot == nil {
		return errors.New("empty snapshot")
	}
	if !strings.EqualFold(strings.TrimSpace(snapshot.Name), serviceName) {
		return fmt.Errorf("response service mismatch %q", snapshot.Name)
	}
	snapshot.Name = serviceName
	if len(snapshot.Routers) > maxMenuSnapshotRouters || len(snapshot.Reports) > maxMenuSnapshotReports ||
		len(snapshot.Title) > maxMenuTitleBytes || len(snapshot.TitleEN) > maxMenuTitleBytes {
		return errors.New("menu snapshot exceeds size limits")
	}
	prefix := "/api/manage/" + serviceName + "/"
	for _, item := range snapshot.Routers {
		path := strings.TrimSpace(item.Path)
		lowerPath := strings.ToLower(path)
		if !menuPathSegmentPattern.MatchString(item.InstanceName) || len(path) > 512 ||
			len(item.Title) > maxMenuTitleBytes || len(item.TitleEN) > maxMenuTitleBytes ||
			!strings.HasPrefix(lowerPath, prefix) ||
			pathpkg.Clean(path) != path || strings.ContainsAny(path, `\%?#`) {
			return fmt.Errorf("invalid manage route %q", item.Path)
		}
		for _, segment := range strings.Split(path[len(prefix):], "/") {
			if !menuPathSegmentPattern.MatchString(segment) {
				return fmt.Errorf("invalid manage route %q", item.Path)
			}
		}
	}
	for _, item := range snapshot.Reports {
		if !strings.EqualFold(strings.TrimSpace(item.Service), serviceName) ||
			!menuPathSegmentPattern.MatchString(item.Code) || item.Path != stats.ReportPath(serviceName, item.Code) ||
			len(item.Title) > maxMenuTitleBytes || len(item.Description) > maxMenuDescriptionBytes {
			return fmt.Errorf("invalid report route %q", item.Path)
		}
	}
	return nil
}
