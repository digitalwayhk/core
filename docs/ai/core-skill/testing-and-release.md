# 集成测试、UAT 与发布门禁

## 标准集成测试模板

集成测试是平台服务的标准能力，不是可选示例。为新服务创建集成测试时，必须优先复用以下两层模板。

### 公共测试能力

`examples/integration/helpers.go` 负责与业务无关的能力：

- `StartProcess(ProcessOptions)`：编译并启动真实服务进程。
- 为服务分配隔离端口和系统临时目录。
- 捕获服务日志并在失败时输出。
- `RequestJSON`：通过真实 HTTP 调用路由并解析统一响应信封。
- `TokenFor`：调用框架内建 `/api/servermanage/testtoken` 获取普通用户或管理员令牌。
- `WriteWebSocket`、`ReadWebSocket`、`StreamWebSocket`：使用真实 WebSocket 协议测试订阅和事件。
- `Stop`：关闭进程并清理临时目录。

不要在每个服务里重新实现进程管理、端口分配、TestToken、HTTP 信封或 WebSocket 通信。只有当验收目标本身是 Casdoor 登录、刷新、撤销或 Webhook 时，才以 `examples/integration/05-shop-casdoor-rbac` 为模板，在服务专属 Suite 中用 Fake Casdoor callback 覆盖 `TokenFor`；该测试不得回退到 TestToken。

### 服务专属 Suite

以 `examples/integration/01-simple-shop/helpers_test.go` 为模板：

```go
type serviceSuite struct {
	*integration.Suite
}

func startServiceSuite() (*serviceSuite, error) {
	base, err := integration.StartProcess(integration.ProcessOptions{
		BuildPackage: "./path/to/service/main",
		BinaryName:   "service-name",
		TempPrefix:   "core-service-name-",
		ServiceCount: 2,
		ServiceIndex: 1,
	})
	if err != nil {
		return nil, err
	}
	// 等待本服务真实业务路由可用；失败时 Stop。
	return &serviceSuite{Suite: base}, nil
}
```

服务专属目录只保存：

- 当前服务的测试 DTO。
- 业务路由辅助方法。
- 就绪探测。
- 业务 WebSocket 登录/订阅和事件解析。

业务 DTO 放在对应服务集成测试目录，不放入 `examples/integration/helpers.go`。

### TestMain 生命周期

一个服务测试目录启动一个真实进程，供三类测试共用：

```go
func TestMain(m *testing.M) {
	created, err := startServiceSuite()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	suite = created
	code := m.Run()
	if code != 0 {
		suite.PrintLog()
	}
	suite.Stop()
	os.Exit(code)
}
```

测试必须等待认证和真实业务路由可用，不能只等待端口监听。框架应在临时目录首次启动时自动生成配置；测试可验证必要配置存在，但不向源码目录写运行配置。

### 文件和测试分组

```text
manage_test.go
public_test.go
private_test.go
```

每个 API 或 Manage command 使用独立测试函数，整组入口保留并调用全部子测试：

```go
func TestPrivateAPIs(t *testing.T) {
	t.Run("AddOrder", testAddOrderAPI)
	t.Run("GetOrders", testGetOrdersAPI)
	t.Run("DeleteOrder", testDeleteOrderAPI)
	t.Run("GetOrdersWebSocket", testGetOrdersWebSocketAPI)
}
```

Manage 按 command 拆分，而不是把 view/search/add/edit/remove 堆在一个测试函数中。Public/Private 按 API 拆分。这样既能单独定位失败，也能一次运行整类能力。

只要是多服务业务，就必须提供真实多进程 UAT。单服务测试、同进程测试或单包 handler 测试不能证明跨服务发现、内部调用、事件投递、缓存失效和角色权限边界真的可用；任一业务角色都可能通过跨服务调用链才能确认完整能力。

多服务 UAT 必须按业务角色或调用方拆文件，每个角色文件保存本角色全部功能闭环和异常权限断言，并提供一个可单独 `go test -run` 的角色闭环测试。任何角色或服务只要实现 WebSocket 接口，该角色 UAT 就必须覆盖真实 WebSocket 登录、订阅、事件投递、身份隔离和异常边界。示例 06 三进程 UAT 是标准模板：`buyer_uat_test.go` 放普通用户注册模拟、资料/地址维护、下单、支付、本人订单查询、WebSocket 订单订阅和其他用户隔离；`supplier_uat_test.go` 放供应商注册模拟、商品维护/上架、本供应商订单投影查询和其他供应商隔离；`admin_uat_test.go` 放平台管理员支付类型配置和全量订单查询。完整三角色流程测试只负责启动三个真实进程并组合这些角色步骤；共享查找、进程启动、业务 DTO 转换等跨角色辅助可以放独立 helper 文件。不要把三种角色的 API 调用、断言和异常用例全部堆在一个 UAT 大文件中。

### 最低验收范围

Manage：

- 管理员鉴权。
- view/search 元数据与列表。
- add/edit/remove 的成功和业务校验。
- 只读 Manage 的写 command 未注册。

Public：

- 空筛选、单条件和组合条件。
- 非法参数公开错误。
- DTO 不泄露持久化字段。

Private：

- 未认证请求被拒绝。
- UserID 来自令牌，不接受客户端伪造。
- 资源所有权和跨用户隔离。
- 业务事实快照、唯一性和删除语义。
- HTTP DTO 不包含仅供事件使用的 action。

WebSocket：

- 只要服务实现 WebSocket 接口，集成测试和 UAT 都必须使用真实 WebSocket 覆盖该能力，不能只测 HTTP 或 handler。
- 匿名订阅被拒绝。
- 登录后按真实 RouterInfo 路径订阅。
- 创建和删除事件结构正确。
- 事件只投递给当前用户，其他用户无消息。
- 连接和读取具有明确超时，测试结束关闭连接。

### 标准命令

```bash
go test ./examples/integration/01-simple-shop -count=1
go test ./examples/integration/01-simple-shop -count=10
go test -race ./examples/integration/01-simple-shop -count=1
```

新服务将路径替换为自身集成测试目录。涉及并发、身份隔离或 WebSocket 时必须运行 race；需要稳定性证据时运行多次，不用无断言 sleep 或 retry 掩盖失败。

## 测试与发布

```bash
./scripts/test.sh quick
./scripts/test.sh security
./scripts/test.sh config-contract
./scripts/test.sh persistence-unit
./scripts/test.sh performance-contract
./scripts/test.sh release-contract
```

外部依赖默认 skip：

```bash
./scripts/test.sh integration-external-docker
./scripts/test.sh integration-persistence
```

### 内嵌前端产物与子模块指针

管理后台前端产物 `pkg/server/run/dist` 直接提交进仓库，由 `scripts/build-web-admin.sh` 生成，构建时把当时的 `web/admin` HEAD 写进 `dist/build-info.json` 的 `frontend_commit`。升级前端时，**子模块指针与重建后的 dist 必须在同一个提交里同时更新**；只推进 `web/admin` 而不重建 dist，服务内嵌的仍是旧前端，运行时不会报任何错。

必过门禁 `required/web-dist-sync` 守这条不变量，本地单独运行：

```bash
./scripts/check-web-dist-sync.sh   # 只读校验，约 1 秒，不需要 node/yarn
./scripts/test.sh web-dist-sync    # 契约测试 + 校验
```

它比对 `git ls-tree HEAD web/admin` 的**已提交**指针与 `build-info.json` 的 `frontend_commit`。不要改成读子模块工作区 HEAD——本地 checkout 会让它跟着漂移，从而放过已提交的不一致。

发布前不得自动创建 tag。开发消费方可临时引用分支或精确 commit：

```bash
go get github.com/digitalwayhk/core@codex/optimize-code-cleanup
go get github.com/digitalwayhk/core@<commit>
```

分支会移动并解析为伪版本；生产必须使用已发布 tag 或精确 commit。执行 `release-contract`，并遵循 `docs/RELEASE_POLICY.md` 与废弃登记。

