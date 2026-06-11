# Digitalway Core Backend API Reference

This reference is based on the current repository implementation, especially:

- `pkg/server/router/servicerouter.go`
- `service/manage/types.go`
- `pkg/persistence/entity/model.go`
- `pkg/persistence/entity/basemodel.go`
- `pkg/persistence/entity/modellist.go`
- `examples/01-hello-router`
- `examples/demo`

## Router Shape

All API endpoints implement:

```go
type IRouter interface {
    Parse(req types.IRequest) error
    Validation(req types.IRequest) error
    Do(req types.IRequest) (interface{}, error)
    RouterInfo() *types.RouterInfo
}
```

Common request methods:

- `req.Bind(v)` binds request JSON/query values into a struct.
- `req.GetValue("key")` reads query/form values.
- `req.GetUser()` returns the authenticated user id and name.
- `req.GetClaims("key")` reads JWT claims.
- `req.CallService(router, cb...)` calls another route.
- `req.ServiceName()` returns the current service.

## Route Path Rules

`router.DefaultRouterInfo(own)` derives service name, API type, auth, and path from the package and struct name.

Current ordinary service route paths are:

```text
/api/{serviceName}/{structNameLower}
```

Examples:

- `examples/01-hello-router/api/public.Ping` -> `/api/hello/ping`
- `examples/demo/api/public.GetOrder` -> `/api/demo/getorder`
- `examples/demo/api/private.AddOrder` -> `/api/demo/addorder`

The package path still matters:

- `api/public` sets `PathType = types.PublicType` and no route auth.
- `api/private` sets `PathType = types.PrivateType` and `Auth = true`.
- `api/manage` ordinary routes should usually use `manage.RouterInfo`, not `router.DefaultRouterInfo`.

Standard manage route paths are:

```text
/api/manage/{serviceName}/{manageStructLower}/{operationLower}
```

For `TokenManage`, examples include:

- `/api/manage/demo/tokenmanage/view`
- `/api/manage/demo/tokenmanage/search`
- `/api/manage/demo/tokenmanage/add`
- `/api/manage/demo/tokenmanage/edit`
- `/api/manage/demo/tokenmanage/remove`
- `/api/manage/demo/tokenmanage/submit`
- `/api/manage/demo/tokenmanage/release`

Server management routes use:

```text
/api/servermanage/{structNameLower}
```

`pkg/server/api/public/TestToken` uses this server-router mechanism and sets `GET`. Use:

```text
GET /api/servermanage/testtoken?userid={id}&type=0
```

Token type values:

- `0`: normal user token
- `1`: manage token
- `2`: server manage token

## Public API Template

```go
package public

import (
    "github.com/digitalwayhk/core/pkg/server/router"
    "github.com/digitalwayhk/core/pkg/server/types"
)

type Ping struct {
    Name string `json:"name" desc:"caller name"`
}

func (own *Ping) Parse(req types.IRequest) error {
    return req.Bind(own)
}

func (own *Ping) Validation(req types.IRequest) error {
    if own.Name == "" {
        own.Name = "guest"
    }
    return nil
}

func (own *Ping) Do(req types.IRequest) (interface{}, error) {
    return map[string]string{"message": "hello " + own.Name}, nil
}

func (own *Ping) RouterInfo() *types.RouterInfo {
    return router.DefaultRouterInfo(own)
}
```

For GET routes:

```go
func (own *GetOrder) RouterInfo() *types.RouterInfo {
    info := router.DefaultRouterInfo(own)
    info.Method = "GET"
    return info
}
```

## Private API Template

Private routes live under `api/private`, use the same handler shape, and automatically require auth.

```go
func (own *AddOrder) Validation(req types.IRequest) error {
    if userID, _ := req.GetUser(); userID == "" {
        return errors.New("userid is empty")
    }
    return nil
}

func (own *AddOrder) Do(req types.IRequest) (interface{}, error) {
    userID, _ := req.GetUser()
    list := entity.NewModelList[models.OrderModel](nil)
    order := list.NewItem()
    order.UserID = userID
    if err := list.Add(order); err != nil {
        return nil, err
    }
    return order, list.Save()
}
```

Do not trust a client-supplied user id for authenticated identity.

## Models and ModelList

Use `entity.Model` for records that do not need `Code`/state semantics:

```go
type OrderModel struct {
    *entity.Model
    UserID string
}

func NewOrderModel() *OrderModel {
    return &OrderModel{Model: entity.NewModel()}
}

func (own *OrderModel) NewModel() {
    if own.Model == nil {
        own.Model = entity.NewModel()
    }
}
```

Use `entity.BaseModel` when the record has `Code`, `Name`, and lifecycle state:

```go
type TokenModel struct {
    *entity.BaseModel
    Price decimal.Decimal
}

func (own *TokenModel) NewModel() {
    if own.BaseModel == nil {
        own.BaseModel = entity.NewBaseModel()
    }
}
```

Important `BaseModel` behavior:

- `AddValid()` requires non-zero `ID` and non-empty `Code`.
- `UpdateValid()` requires non-empty `Code`.
- `RemoveValid()` rejects records with `State > 0`.
- `GetHash()` returns a hash of `Code`.

If the entity does not naturally have a unique `Code`, prefer `entity.Model`. If compatibility requires `BaseModel`, ensure code/hash semantics are intentionally handled.

Common persistence methods:

```go
list := entity.NewModelList[models.OrderModel](nil)

item := list.NewItem()
err := list.Add(item)
err = list.Save()

rows, total, err := list.SearchAll(page, size)
rows, err = list.SearchWhere("UserID", userID)
one, err := list.SearchId(id)
err = list.Update(item)
err = list.Remove(item)
err = list.Save()
```

`SearchWhere` defaults to size `500` if the caller does not change `SearchItem.Size`. Use `SearchAll` for paginated APIs or pass a callback that sets search size/sort/preload.

## Manage CRUD

Standard manage service:

```go
package manage

import (
    managepkg "github.com/digitalwayhk/core/service/manage"
    "github.com/digitalwayhk/core/service/manage/view"
    "yourmodule/models"
)

type TokenManage struct {
    *managepkg.ManageService[models.TokenModel]
}

func NewTokenManage() *TokenManage {
    own := &TokenManage{}
    own.ManageService = managepkg.NewManageService[models.TokenModel](own)
    return own
}

func (own *TokenManage) ViewModel(v *view.ViewModel) {
    v.AutoLoad = true
}
```

Register it:

```go
routers = append(routers, manage.NewTokenManage().Routers()...)
```

Generated operations: `View`, `Search`, `Add`, `Edit`, `Remove`, `Submit`, `Release`.

Hooks:

- `ParseBefore`, `ParseAfter`
- `ValidationBefore`, `ValidationAfter`
- `DoBefore`, `DoAfter`
- `SearchBefore`, `SearchAfter`
- `ForeignSearchBefore`, `ForeignSearchAfter`
- `ChildSearchBefore`, `ChildSearchAfter`
- `ViewModel`, `ViewFieldModel`, `ViewCommandModel`, `ViewChildModel`

`DoBefore` and search hooks return `(result, error, stop)`. If `stop` is true, the framework returns the hook result and skips default processing.

## Custom Manage Operation

Embed `manage.Operation[T]` as a value:

```go
type ExportData[T pt.IModel] struct {
    manage.Operation[T]
}

func NewExportData[T pt.IModel](instance interface{}) *ExportData[T] {
    return &ExportData[T]{Operation: manage.NewOperation[T](instance)}
}

func (own *ExportData[T]) Parse(req st.IRequest) error {
    return nil
}

func (own *ExportData[T]) Validation(req st.IRequest) error {
    return own.Operation.Validation(req)
}

func (own *ExportData[T]) Do(req st.IRequest) (interface{}, error) {
    return "https://example.com/export.csv", nil
}

func (own *ExportData[T]) RouterInfo() *st.RouterInfo {
    return manage.RouterInfo(own)
}
```

Append it from the manage `Routers()` method and customize the command in `ViewCommandModel` using the lowercase operation struct name.

## Service Registration

```go
type DemoService struct{}

func (own *DemoService) ServiceName() string {
    return "demo"
}

func (own *DemoService) Routers() []types.IRouter {
    routers := make([]types.IRouter, 0)
    routers = append(routers, &public.GetOrder{})
    routers = append(routers, &private.AddOrder{})
    routers = append(routers, manage.NewTokenManage().Routers()...)
    return routers
}

func (own *DemoService) SubscribeRouters() []*types.ObserveArgs {
    return []*types.ObserveArgs{}
}
```

Optional lifecycle hooks include `Start()`, `Stop()`, and `IsCloseServerManage() bool`.

## Server Startup

```go
func main() {
    server := run.NewWebServer()
    server.AddIService(&demo.DemoService{}, &types.ServerOption{
        IsCors: true,
        IsWebSocket: true,
    })
    server.Start()
}
```

Configuration is generated on first start under `etc/{serviceName}.json` in current versions.

## WebSocket and Observe

Use `SubscribeRouters()` for route-success notifications:

```go
return []*types.ObserveArgs{
    types.NewObserveArgs(
        &private.AddOrder{},
        types.ObserveResponse,
        func(args *types.NotifyArgs) error {
            api := &public.GetOrder{}
            info := api.RouterInfo()
            order := router.GetResponseData[models.OrderModel](args.Response)
            info.NoticeWebSocket(order)
            return nil
        },
    ),
}
```

WebSocket subscriptions should use the actual `RouterInfo().Path`, for example `/api/demo/getorder` for ordinary routes.

For targeted websocket push, implement hash/filter behavior on the subscribed router and call `NoticeWebSocket` on the same route info used for subscription.

## EventBridge, Cluster, and Transport

Use current implementations under:

- `pkg/server/event`
- `pkg/server/mq`
- `pkg/server/cluster`
- `pkg/server/transport`
- `examples/09-transport-selector`
- `examples/10-cluster-local`
- `examples/11-cluster-etcd-consul`
- `examples/12-mq-event-stream`

MQ is configured through EventBridge and MQ providers; it is not a direct transport selector value. Direct internal transport uses `grpc`, `http`, or `socket` plus fallback configuration.

## Frontend/API Notes

- Ordinary public/private HTTP calls use `/api/{service}/{router}`.
- Manage CRUD calls use `/api/manage/{service}/{manage}/{operation}`.
- Use `/api/servermanage/testtoken?userid={id}&type=0` for private API test tokens and `type=1` for manage API test tokens.
- The response is framework wrapped by server code; when writing clients, inspect actual responses or generated OpenAPI for field names.
- WayPage/JWT/Umi route integration is marked unstable in project instructions and should not be generated by default.

## Version References

Other systems can reference a pushed branch directly during development or testing:

```sh
go get github.com/digitalwayhk/core@codex/optimize-code-cleanup
```

Go resolves branch names to pseudo-versions in `go.mod`. Branch references move when new commits are pushed, so production systems should prefer a tag or exact commit:

```sh
go get github.com/digitalwayhk/core@71d9e11
go get github.com/digitalwayhk/core@v0.0.247
```

For scripts or CI that need the source tree directly, checkout the branch:

```sh
git clone -b codex/optimize-code-cleanup git@github.com:digitalwayhk/core.git
```

## Validation Commands

Prefer targeted checks:

```sh
go test ./service/manage/...
go test ./pkg/server/router/...
go test ./examples/01-hello-router/...
```

For broader changes:

```sh
gofmt -w <edited-go-files>
go test ./...
```
