# Shop Microservices Service Boundary Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild example 06 around the approved User, Supplier, and Order service boundaries, while adding a framework-level trusted internal-caller allowlist that works for same-process calls and remote gRPC/mTLS calls.

**Architecture:** Implement the Core security primitive first, then migrate shared contracts and each service in dependency order: Supplier read models and catalog, Order facts and reliable events, then User facade and buyer workflow. Preserve Redis discovery, Core ServiceResolver, gRPC transport, Outbox/Inbox delivery, Manage hooks, and real-process tests; constrained Public routes fail before Parse/Validation/Do unless the framework supplies a trusted caller identity.

**Tech Stack:** Go 1.26, Digitalway Core RouterInfo/ServiceContext/Manage, go-zero REST and zrpc, grpc-go mTLS, GORM-backed SQLite adapters, Redis Streams/EventBridge, shopspring/decimal, testify, Docker Compose.

---

## Delivery map

The work is one dependent delivery rather than three independent projects. Core caller authentication is required before service routes can be safely exposed; Supplier must expose the product snapshot before Order can create orders; Order must publish the canonical event before Supplier projection and User notification can be verified.

| Stage | Working result |
| --- | --- |
| 1. Core metadata | Routes can declare and snapshot immutable internal caller allowlists. |
| 2. Core enforcement | HTTP, spoofed local calls, insecure remote calls, and invalid mTLS identities fail before route parsing. |
| 3. Shared contract | All services compile against numeric business IDs, explicit events, revisions, and stable errors. |
| 4. Supplier | Supplier/Product Manage, internal catalog APIs, deletion protection, and permanent order projection work. |
| 5. Order | Internal order/payment APIs, idempotency, snapshots, state commands, and reliable events work. |
| 6. User | User/Address Manage, public facades, private buyer workflow, caching, and WebSocket isolation work. |
| 7. Deployment/docs | All-in-one and three-process tests prove the boundaries; Core docs and the skill describe the new capability. |

### Task 1: Freeze and snapshot internal caller metadata

**Files:**
- Modify: `pkg/server/types/routerinfo.go`
- Modify: `pkg/server/router/routerinfooption.go`
- Modify: `pkg/server/run/openapi.go`
- Modify: `internal/compat/compat.go`
- Modify: `internal/compat/compat_test.go`
- Modify: `internal/compat/fixture/api/public/getthing.go`
- Modify: `internal/compat/testdata/routes.golden.json`
- Modify: `internal/compat/testdata/openapi.golden.json`
- Test: `pkg/server/router/servicerouter_registry_test.go`

- [ ] **Step 1: Add failing RouterInfo option and freeze tests**

Append tests that require normalization, deduplication, defensive copies, and freeze checking:

```go
type internalCallerOptionRouter struct{}

func (*internalCallerOptionRouter) Parse(types.IRequest) error                   { return nil }
func (*internalCallerOptionRouter) Validation(types.IRequest) error              { return nil }
func (*internalCallerOptionRouter) Do(types.IRequest) (interface{}, error)        { return "ok", nil }
func (r *internalCallerOptionRouter) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(r)
}

func TestInternalCallersAreNormalizedFrozenAndDefensivelyCopied(t *testing.T) {
	info := router.DefaultRouterInfoWithOptions(&internalCallerOptionRouter{},
		router.WithInternalCallers(" shop-user ", "shop-order", "shop-user", ""),
	)
	info.Freeze("fixture")

	got := info.GetInternalCallers()
	require.Equal(t, []string{"shop-order", "shop-user"}, got)
	got[0] = "mutated"
	require.Equal(t, []string{"shop-order", "shop-user"}, info.GetInternalCallers())
	require.Panics(t, func() { info.InternalCallers = []string{"changed"}; _ = info.GetPath() })
}
```

- [ ] **Step 2: Run the focused test and verify the missing API failure**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/router -run TestInternalCallersAreNormalizedFrozenAndDefensivelyCopied -count=1`

Expected: FAIL because `WithInternalCallers`, `RouterInfo.InternalCallers`, and `GetInternalCallers` do not exist.

- [ ] **Step 3: Add the immutable metadata and option**

Add the field to `RouterInfo`, include it in `routerMetadata`, compare it in `assertMetadataFrozenLocked`, and return a copy:

```go
// InternalCallers is a registration-time allowlist for trusted service callers.
// Deprecated: registration code should use router.WithInternalCallers and readers
// should use GetInternalCallers.
InternalCallers []string

type routerMetadata struct {
	// existing fields remain unchanged
	internalCallers string
}

func normalizeInternalCallers(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (own *RouterInfo) GetInternalCallers() []string {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return append([]string(nil), own.InternalCallers...)
}
```

Store `strings.Join(normalizeInternalCallers(own.InternalCallers), "\x00")` in `currentMetadataLocked`, and normalize `InternalCallers` immediately before assigning `frozenMetadata` in `Freeze`.

Add the option:

```go
func WithInternalCallers(serviceNames ...string) RouterInfoOption {
	values := append([]string(nil), serviceNames...)
	return routerInfoOptionFunc(func(info *types.RouterInfo) {
		info.InternalCallers = values
	})
}
```

- [ ] **Step 4: Add internal callers to the compatibility snapshot**

Extend `RouteEntry` and snapshot production:

```go
type RouteEntry struct {
	Service         string   `json:"service"`
	Method          string   `json:"method"`
	Path            string   `json:"path"`
	PathType        string   `json:"pathType"`
	Auth            bool     `json:"auth"`
	InternalCallers []string `json:"internalCallers,omitempty"`
}
```

Set `InternalCallers: info.GetInternalCallers()` in `SnapshotRoutes`, update `newFixtureServiceRouter` to copy `spec.InternalCallers`, and add one fixture route with `[]string{"shop-user"}` so the golden proves the security property is serialized.

In `pkg/server/run/openapi.go`, add the same property to every constrained operation produced from a RouterInfo:

```go
if callers := info.GetInternalCallers(); len(callers) > 0 {
	if operation.Extensions == nil {
		operation.Extensions = make(map[string]interface{})
	}
	operation.Extensions["x-internal-callers"] = callers
}
```

Extend `TestOpenAPISnapshotIgnoresRuntimeHostAndPort` to assert the constrained fixture operation contains `x-internal-callers`, then explicitly regenerate and review both golden files with `UPDATE_GOLDEN=1 GOCACHE=/private/tmp/core-codex-gocache go test ./internal/compat -run 'TestRouteSnapshotIsSortedAndMatchesGolden|TestOpenAPISnapshotIgnoresRuntimeHostAndPort' -count=1`.

- [ ] **Step 5: Run metadata and compatibility tests**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/router ./internal/compat -count=1`

Expected: PASS; `routes.golden.json` contains `"internalCallers": ["shop-user"]` and `openapi.golden.json` contains `"x-internal-callers": ["shop-user"]` on the constrained fixture route.

- [ ] **Step 6: Commit the Core metadata slice**

```bash
git add pkg/server/types/routerinfo.go pkg/server/router/routerinfooption.go pkg/server/router/servicerouter_registry_test.go pkg/server/run/openapi.go internal/compat/compat.go internal/compat/compat_test.go internal/compat/fixture/api/public/getthing.go internal/compat/testdata/routes.golden.json internal/compat/testdata/openapi.golden.json
git commit -m "feat(router): add frozen internal caller metadata"
```

### Task 2: Enforce trusted callers before Parse, Validation, and Do

**Files:**
- Create: `pkg/server/types/internalcaller.go`
- Create: `pkg/server/router/internalcaller_test.go`
- Modify: `pkg/server/types/interface.go`
- Modify: `pkg/server/types/routerinfo.go`
- Modify: `pkg/server/router/request.go`
- Modify: `pkg/server/router/servicecontext.go`

- [ ] **Step 1: Write route execution tests for HTTP, local success, spoofing, and early rejection**

Create a counting router whose Parse/Validation/Do methods increment counters. Cover these cases:

```go
func TestConstrainedRouteRejectsHTTPBeforeParse(t *testing.T) {
	api, info := newCountingRoute("shop-order", []string{"shop-user"})
	req := NewRequest(newHTTPServiceRouter(t, info), httptest.NewRequest(http.MethodPost, info.GetPath(), strings.NewReader(`{}`)))

	response := info.Exec(req)

	require.False(t, response.GetSuccess())
	require.ErrorIs(t, response.GetError(), ErrInternalCallerForbidden)
	require.Equal(t, routeCounters{}, api.counters)
}

func TestLocalDispatchUsesSourceContextNotPayloadClaim(t *testing.T) {
	target, api := newLocalConstrainedService(t, "shop-order", "shop-user")
	allowed := newSourceContext(t, "shop-user")
	_, err := allowed.CallService(&types.PayLoad{SourceService: "spoofed", TargetService: "shop-order", TargetPath: api.RouterInfo().GetPath(), Instance: api})
	require.NoError(t, err)

	wrong := newSourceContext(t, "shop-supplier")
	_, err = wrong.CallService(&types.PayLoad{SourceService: "shop-user", TargetService: "shop-order", TargetPath: api.RouterInfo().GetPath(), Instance: api})
	require.ErrorIs(t, err, ErrInternalCallerForbidden)
	require.Equal(t, 1, api.counters.parse)
	_ = target
}
```

- [ ] **Step 2: Run the new tests and verify they fail**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/router -run 'TestConstrainedRoute|TestLocalDispatchUsesSourceContext' -count=1`

Expected: FAIL because constrained routes currently accept HTTP and trust only payload fields.

- [ ] **Step 3: Add a typed trusted-caller context and optional request interface**

Create `pkg/server/types/internalcaller.go`:

```go
package types

import "context"

type trustedInternalCallerKey struct{}

func ContextWithTrustedInternalCaller(ctx context.Context, service string) context.Context {
	return context.WithValue(ctx, trustedInternalCallerKey{}, service)
}

func TrustedInternalCallerFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	service, ok := ctx.Value(trustedInternalCallerKey{}).(string)
	return service, ok && service != ""
}
```

Add without changing stable `IRequest`:

```go
type IRequestInternalCaller interface {
	TrustedInternalCaller() (string, bool)
}
```

Add `trustedInternalCaller string` to `Request`, populate it from the context in a new `ToRequestContext(ctx, payload)` function, keep `ToRequest(payload)` as a compatibility wrapper using `context.Background()`, and implement:

```go
func (own *Request) TrustedInternalCaller() (string, bool) {
	return own.trustedInternalCaller, own.trustedInternalCaller != ""
}
```

- [ ] **Step 4: Add one authorization method and invoke it at every execution boundary**

Add the stable sentinel and check in the router package:

```go
var ErrInternalCallerForbidden = errors.New("trusted internal caller is not allowed")

func authorizeInternalCaller(info *types.RouterInfo, req types.IRequest) error {
	allowed := info.GetInternalCallers()
	if len(allowed) == 0 {
		return nil
	}
	callerReq, ok := req.(types.IRequestInternalCaller)
	if !ok {
		return ErrInternalCallerForbidden
	}
	caller, trusted := callerReq.TrustedInternalCaller()
	if !trusted || !slices.Contains(allowed, caller) {
		return ErrInternalCallerForbidden
	}
	return nil
}
```

Call it at the beginning of `RouterInfo.Exec`, before `api.Parse(req)`, and at the beginning of `RouterInfo.ExecDo`, before `api.Validation(req)`. Change `dispatchLocal` to accept the invocation context and authorize before `info.ParseNew(payload.Instance)`:

```go
req := ToRequestContext(ctx, payload)
if err := authorizeInternalCaller(info, req); err != nil {
	return nil, err
}
```

Pass `ctx` unchanged from `invokePayload` into `dispatchLocal(ctx, payload, local)`. At the outbound `ServiceContext.CallService` boundary, overwrite the source claim and mark the invocation context from the actual source ServiceContext before calling `invokePayload`:

```go
if own.Service != nil {
	payload.SourceService = own.Service.Name
}
ctx := types.ContextWithTrustedInternalCaller(context.Background(), own.Service.Name)
values, err := own.invokePayload(ctx, payload)
```

Apply the same context construction inside the asynchronous callback branch. This distinction is required: a local outbound call gets trust from its real source ServiceContext, while `HandleInternalPayload` passes the gRPC context supplied by Task 3 and must never replace it with the target service identity.

- [ ] **Step 5: Run request, router pool, and resolver tests**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/router ./pkg/server/types -count=1`

Expected: PASS; unconstrained routes remain behavior-compatible and all constrained-route failures occur with zero Parse/Validation/Do counters.

- [ ] **Step 6: Commit early caller enforcement**

```bash
git add pkg/server/types/internalcaller.go pkg/server/types/interface.go pkg/server/types/routerinfo.go pkg/server/router/request.go pkg/server/router/servicecontext.go pkg/server/router/internalcaller_test.go
git commit -m "feat(router): enforce trusted internal callers"
```

### Task 3: Bind remote caller identity to verified mTLS certificates

**Files:**
- Create: `pkg/server/transport/grpc/caller_identity.go`
- Modify: `pkg/server/transport/grpc/server.go`
- Modify: `pkg/server/transport/grpc/security_test.go`
- Test: `pkg/server/router/internalcaller_test.go`

- [ ] **Step 1: Add certificate identity unit tests**

Use the existing test PKI helper to assert exact behavior:

```go
func TestTrustedCallerFromPeerRequiresVerifiedMatchingSAN(t *testing.T) {
	ctx := verifiedPeerContext(t, []string{"shop-user", "localhost"})
	caller, err := trustedCallerFromPeer(ctx, "shop-user")
	require.NoError(t, err)
	require.Equal(t, "shop-user", caller)

	_, err = trustedCallerFromPeer(ctx, "shop-order")
	require.ErrorIs(t, err, errCallerIdentityMismatch)
	_, err = trustedCallerFromPeer(context.Background(), "shop-user")
	require.ErrorIs(t, err, errTrustedPeerRequired)
}
```

Add an RPC test that sends `SourceService=shop-user` with a `shop-order` client certificate and proves the target route's Parse/Validation/Do counters remain zero.

- [ ] **Step 2: Run the gRPC security tests and verify the helper is missing**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./pkg/server/transport/grpc -run 'TestTrustedCallerFromPeer|TestServerCallRejectsMismatchedCaller' -count=1`

Expected: FAIL because peer identity is not extracted or checked.

- [ ] **Step 3: Implement verified peer extraction**

Create:

```go
package grpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

var (
	errTrustedPeerRequired    = errors.New("verified mTLS peer certificate is required")
	errCallerIdentityMismatch = errors.New("mTLS peer identity does not match source service")
)

func trustedCallerFromPeer(ctx context.Context, claimed string) (string, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "", errTrustedPeerRequired
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.PeerCertificates) == 0 {
		return "", errTrustedPeerRequired
	}
	if claimed == "" || tlsInfo.State.PeerCertificates[0].VerifyHostname(claimed) != nil {
		return "", errCallerIdentityMismatch
	}
	return claimed, nil
}
```

- [ ] **Step 4: Mark only verified matching peers as trusted**

In `Server.Call`, after `pbToPayload` and before invoking the handler:

```go
payload := pbToPayload(req)
if caller, err := trustedCallerFromPeer(ctx, payload.SourceService); err == nil {
	ctx = coretypes.ContextWithTrustedInternalCaller(ctx, caller)
}
data, err := s.handler(ctx, payload)
```

Do not convert absence of mTLS into an RPC transport failure: unconstrained routes remain callable in existing insecure/mesh modes, while constrained routes reject the request at the route boundary. A verified certificate with a mismatched SAN is not marked trusted and therefore fails before Parse.

- [ ] **Step 5: Add remote constrained-route integration at the handler boundary**

Extend `internalcaller_test.go` so `HandleInternalPayload` receives contexts for: matching `shop-user`, wrong `shop-supplier`, no peer marker, and spoofed payload. Assert only the matching context reaches Parse/Do.

- [ ] **Step 6: Run Core security regression**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./pkg/server/transport/grpc ./pkg/server/router ./internal/compat -count=1`

Expected: PASS, including existing TLS, mTLS handshake, lifecycle, route freeze, and compatibility tests.

- [ ] **Step 7: Commit remote identity binding**

```bash
git add pkg/server/transport/grpc/caller_identity.go pkg/server/transport/grpc/server.go pkg/server/transport/grpc/security_test.go pkg/server/router/internalcaller_test.go
git commit -m "feat(grpc): bind internal callers to mtls identity"
```

### Task 4: Replace the shared shop contract with numeric IDs and explicit events

**Files:**
- Modify: `examples/06-shop-microservices/contract/error.go`
- Modify: `examples/06-shop-microservices/contract/event.go`
- Modify: `examples/06-shop-microservices/dto/user/user.go`
- Modify: `examples/06-shop-microservices/dto/supplier/supplier.go`
- Modify: `examples/06-shop-microservices/dto/order/order.go`
- Modify: `examples/06-shop-microservices/dto/event/event.go`
- Modify: `examples/06-shop-microservices/dto/contract_test.go`

- [ ] **Step 1: Rewrite DTO contract tests first**

Require numeric business IDs, no `authUserID`, full fulfillment snapshots, and monotonic revision fields:

```go
func TestOrderEventUsesNumericBusinessIDsAndFullSnapshot(t *testing.T) {
	payload := eventdto.OrderChanged{
		Metadata: eventdto.Metadata{EventID: "event-1", SchemaVersion: 1},
		OrderRevision: 2, OrderID: 10, UserID: 20, SupplierID: 30, ProductID: 40,
		Address: userdto.AddressSnapshot{Recipient: "A", Phone: "1", Region: "R", Detail: "D"},
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NotContains(t, string(data), "authUserID")
	require.Contains(t, string(data), `"supplierID":30`)
	require.Contains(t, string(data), `"orderRevision":2`)
	require.Contains(t, string(data), `"detail":"D"`)
}
```

- [ ] **Step 2: Run DTO tests and verify type failures**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./examples/06-shop-microservices/dto -count=1`

Expected: FAIL because current UserID/SupplierID fields are strings and events lack snapshots/revisions.

- [ ] **Step 3: Define the stable contract**

Use these event names and stable errors:

```go
const (
	EventOrderCreated       = "shop.order.created"
	EventOrderStatusChanged = "shop.order.status.changed"
	EventPaymentChanged     = "shop.payment.changed"
	EventPaymentTypeChanged = "shop.payment-type.changed"
)

const (
	SubjectOrderCreated       = "shop.events.order.created"
	SubjectOrderStatusChanged = "shop.events.order.status.changed"
	SubjectPaymentChanged     = "shop.events.payment.changed"
	SubjectPaymentTypeChanged = "shop.events.payment-type.changed"
)

var (
	ErrSubjectDisabled       = errors.New("主体已禁用，只允许查看")
	ErrResourceInUse         = errors.New("资源已被使用，只能禁用")
	ErrIdempotencyKeyReused  = errors.New("幂等键已用于不同请求")
	ErrInternalOnly          = errors.New("接口仅允许内部服务调用")
)
```

Define DTO IDs as `uint`; keep `AuthUserID` out of every DTO. Define `OrderChanged` with `SchemaVersion`, `OrderRevision`, numeric IDs, product/supplier/unit-price snapshots, quantity, total, payment/order status, full `AddressSnapshot`, and created/updated timestamps. Define `SupplierOrder` with the same fulfillment data.

- [ ] **Step 4: Run DTO contract tests**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./examples/06-shop-microservices/dto -count=1`

Expected: PASS.

- [ ] **Step 5: Commit shared contracts**

```bash
git add examples/06-shop-microservices/contract examples/06-shop-microservices/dto
git commit -m "refactor(example-06): define numeric service contracts"
```

### Task 5: Rebuild Supplier persistence and order projection

**Files:**
- Modify: `examples/06-shop-microservices/supplier-service/models/models.go`
- Create: `examples/06-shop-microservices/supplier-service/models/supplier_order.go`
- Create: `examples/06-shop-microservices/supplier-service/models/supplier_order_test.go`
- Modify: `examples/06-shop-microservices/supplier-service/business/product.go`
- Modify: `examples/06-shop-microservices/supplier-service/business/product_test.go`

- [ ] **Step 1: Add failing identity, default-state, deletion, and projection tests**

Write tests proving:

```go
func TestApplyOrderEventIsIdempotentAndRevisionMonotonic(t *testing.T) {
	event := orderEvent(100, 1, 200, 300)
	require.NoError(t, ApplyOrderEvent(event))
	require.NoError(t, ApplyOrderEvent(event))

	older := event
	older.OrderRevision = 0
	require.NoError(t, ApplyOrderEvent(older))
	stored, err := FindSupplierOrder(100)
	require.NoError(t, err)
	require.Equal(t, uint64(1), stored.OrderRevision)
	require.Equal(t, "完整地址", stored.AddressDetail)
}

func TestUsedProductAndSupplierCannotBeDeleted(t *testing.T) {
	supplier, product := insertSupplierAndProduct(t)
	require.NoError(t, ApplyOrderEvent(orderEvent(101, 1, supplier.ID, product.ID)))
	require.ErrorIs(t, DeleteProduct(product), contract.ErrResourceInUse)
	require.ErrorIs(t, DeleteSupplier(supplier), contract.ErrResourceInUse)
}
```

- [ ] **Step 2: Run Supplier model/business tests and verify failures**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./examples/06-shop-microservices/supplier-service/models ./examples/06-shop-microservices/supplier-service/business -count=1`

Expected: FAIL because Supplier uses string identity, no SupplierOrder table exists, and deletion checks do not query a permanent projection.

- [ ] **Step 3: Change Supplier and Product facts**

Use these model contracts:

```go
type Supplier struct {
	*entity.Model
	AuthUserID  string `gorm:"not null;uniqueIndex" json:"-"`
	Code        string `gorm:"not null;uniqueIndex"`
	Name        string `gorm:"not null;uniqueIndex"`
	Description string
	Enabled     bool
}

type Product struct {
	*entity.Model
	SupplierID uint            `gorm:"not null;index:idx_product_supplier_code,unique"`
	Code       string          `gorm:"not null;index:idx_product_supplier_code,unique"`
	Name       string          `gorm:"not null"`
	Price      decimal.Decimal `gorm:"type:text;not null"`
	Enabled    bool
}
```

`EnsureSupplier(authUserID, name)` must normalize the auth ID, return the existing row on repeat, create a numeric row with `Enabled=true`, and never expose the auth ID through DTO conversion. `CreateProduct` must inject the trusted SupplierID and force `Enabled=false`.

- [ ] **Step 4: Implement permanent projection and atomic Inbox**

Define `SupplierOrder` with unique `OrderID`, `OrderRevision uint64`, numeric IDs, all snapshots/status/address fields. Implement `ApplyOrderEvent` in one transaction:

```go
func ApplyOrderEvent(event eventdto.OrderChanged) error {
	if err := validateOrderEvent(event); err != nil {
		return err
	}
	return RunTransaction(func(tx persistencetypes.IDataAction) error {
		if existsInbox(tx, event.EventID) {
			return nil
		}
		current, err := findSupplierOrderWith(tx, event.OrderID)
		if err != nil {
			return err
		}
		if current == nil || event.OrderRevision > current.OrderRevision {
			if err := upsertSupplierOrderWith(tx, current, event); err != nil {
				return err
			}
		}
		return insertInboxWith(tx, event.EventID, event.EventType)
	})
}
```

Register `SupplierOrder` in `EnsureStorage`. Deletion methods query only local Product/SupplierOrder tables; they never call Order.

- [ ] **Step 5: Keep Supplier/Product changes transactional with Outbox**

Update business methods so normalized create/edit/enable/disable and the matching `SupplierChanged` or `ProductChanged` Outbox record share one SQLite transaction. Only publish after commit through the existing worker.

- [ ] **Step 6: Run Supplier domain tests with race detection**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/06-shop-microservices/supplier-service/models ./examples/06-shop-microservices/supplier-service/business -count=1`

Expected: PASS; duplicate EventID creates one Inbox and one projection, old revisions do not roll back data, and used rows reject deletion.

- [ ] **Step 7: Commit Supplier facts**

```bash
git add examples/06-shop-microservices/supplier-service/models examples/06-shop-microservices/supplier-service/business
git commit -m "refactor(example-06): add supplier facts and order projection"
```

### Task 6: Replace Supplier Private/Call APIs with unified Manage and internal Public APIs

**Files:**
- Delete: `examples/06-shop-microservices/supplier-service/api/call/getproductsnapshot.go`
- Delete: `examples/06-shop-microservices/supplier-service/api/private/orders.go`
- Delete: `examples/06-shop-microservices/supplier-service/api/private/product.go`
- Replace: `examples/06-shop-microservices/supplier-service/api/manage/manage.go`
- Create: `examples/06-shop-microservices/supplier-service/api/manage/hooks_test.go`
- Create: `examples/06-shop-microservices/supplier-service/api/public/getsuppliers.go`
- Modify: `examples/06-shop-microservices/supplier-service/api/public/getproducts.go`
- Modify: `examples/06-shop-microservices/supplier-service/service.go`
- Create: `examples/06-shop-microservices/supplier-service/service_test.go`

- [ ] **Step 1: Add Manage permission-matrix tests**

Exercise `SearchBefore` and `DoBefore` using supplier and platform-admin requests. Assert owner filters, immutable fields, disabled read-only behavior, admin-only supplier enable/disable, owner-or-admin product enable/disable, and read-only SupplierOrder routers:

```go
func TestProductManageSearchScopesSupplierButNotAdmin(t *testing.T) {
	manage := NewProductManage()
	supplierReq := requestFor("supplier-auth-1")
	search, err, stop := manage.SearchBefore(manage.Search, supplierReq)
	require.NoError(t, err)
	require.False(t, stop)
	requireSearchWhere(t, search, "SupplierID", numericSupplierID(t, "supplier-auth-1"))

	adminSearch, err, stop := manage.SearchBefore(manage.Search, requestFor(contract.PlatformAdminUserID))
	require.NoError(t, err)
	require.False(t, stop)
	requireNoSearchWhere(t, adminSearch, "SupplierID")
}
```

- [ ] **Step 2: Run Manage tests and verify missing hook behavior**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./examples/06-shop-microservices/supplier-service/api/manage -count=1`

Expected: FAIL because current Manage has no owner-scoped hook matrix or SupplierOrder projection.

- [ ] **Step 3: Implement one Manage per model using Core hooks**

Construct routers explicitly:

```go
func (own *SupplierManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Edit, own.Remove, NewSetSupplierEnabled(own)}
}

func (own *ProductManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove, NewSetProductEnabled(own)}
}

func (own *OrderManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search}
}
```

In `SearchBefore`, map non-admin auth UID to numeric Supplier.ID and add `SupplierID` or `ID` predicates. In `DoBefore`, detect the concrete Add/Edit/Remove/command sender, inject owner IDs, reject immutable `AuthUserID`, `SupplierID`, and `Enabled` changes, block writes when the supplier is disabled, and call local deletion guards. `DoAfter` invalidates Supplier Public caches only after successful commits.

- [ ] **Step 4: Implement constrained Public catalog routers**

Both routes must declare stable names, paths, cache, and allowlists:

```go
func (g *GetSuppliers) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(g,
		router.WithServiceName(contract.SupplierServiceName),
		router.WithPath("/api/shop-supplier/getsuppliers"),
		router.WithMethod(http.MethodGet),
		router.WithInternalCallers(contract.UserServiceName),
	)
	info.UseCache(30 * time.Second)
	return info
}

func (g *GetProducts) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(g,
		router.WithServiceName(contract.SupplierServiceName),
		router.WithPath("/api/shop-supplier/getproducts"),
		router.WithMethod(http.MethodGet),
		router.WithInternalCallers(contract.UserServiceName, contract.OrderServiceName),
	)
	info.UseCache(30 * time.Second)
	return info
}
```

Parse id/code/name and supplierID, normalize strings in `GetCacheKey`, return only enabled suppliers and products whose supplier is enabled, and return independent DTOs.

- [ ] **Step 5: Register only Manage and Public routes and consume order events**

`Service.Routers()` must contain `GetSuppliers`, `GetProducts`, `SupplierManage`, `ProductManage`, and read-only `OrderManage`; it must contain no Private or Call router. `OnAuth` auto-creates a supplier only for non-admin identities. Subscribe to OrderCreated, OrderStatusChanged, and PaymentChanged; validate/unmarshal then call `models.ApplyOrderEvent`. Required subscriptions remain all-or-none through `SubscribeExternalControls`.

- [ ] **Step 6: Run Supplier API and service tests**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/06-shop-microservices/supplier-service/... -count=1`

Expected: PASS; the service route inventory has zero Private routes and no `api/call` package.

- [ ] **Step 7: Commit Supplier boundary**

```bash
git add -A examples/06-shop-microservices/supplier-service
git commit -m "refactor(example-06): expose supplier manage and internal catalog"
```

### Task 7: Rebuild Order facts, idempotency, payment attempts, and events

**Files:**
- Modify: `examples/06-shop-microservices/order-service/models/models.go`
- Modify: `examples/06-shop-microservices/order-service/models/payment_record_test.go`
- Modify: `examples/06-shop-microservices/order-service/business/order.go`
- Modify: `examples/06-shop-microservices/order-service/business/payment.go`
- Modify: `examples/06-shop-microservices/order-service/business/order_test.go`

- [ ] **Step 1: Replace business tests with approved state and idempotency cases**

Cover same-key convergence, different-payload rejection, concurrent insert convergence, snapshot preservation, cancellation without deletion, one Processing payment, used payment-type restrictions, revision increments, and Outbox atomicity:

```go
func TestCreateOrderRejectsIdempotencyKeyReuseWithDifferentFingerprint(t *testing.T) {
	command := createOrderCommand("buyer-request-1", 10, 2)
	first, err := CreateOrder(command, fixedProductSnapshot())
	require.NoError(t, err)

	changed := command
	changed.Quantity = 3
	second, err := CreateOrder(changed, fixedProductSnapshot())
	require.ErrorIs(t, err, contract.ErrIdempotencyKeyReused)
	require.Nil(t, second)
	require.Equal(t, uint64(1), first.OrderRevision)
}
```

- [ ] **Step 2: Run Order tests and verify semantic failures**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./examples/06-shop-microservices/order-service/models ./examples/06-shop-microservices/order-service/business -count=1`

Expected: FAIL because current Order uses string IDs, DeleteOrder physically models cancellation, and idempotency keys are not client-stable fingerprints.

- [ ] **Step 3: Replace Order and Payment models**

Define numeric `UserID`, `SupplierID`, `ProductID`; unique `IdempotencyKey`; persisted request fingerprint; `OrderRevision uint64`; supplier/product/code/name/unit-price snapshots; quantity/total; full address snapshot; payment and order statuses. Define PaymentRecord with `Attempt uint`, unique PaymentID, and a hash that includes OrderID plus Attempt/PaymentID. PaymentType defaults disabled.

Remove Order Inbox registration because Order consumes no business event.

- [ ] **Step 4: Implement convergent CreateOrder transaction**

Compute a deterministic fingerprint from ProductID, Quantity, and normalized address snapshot. Before insert, return an existing matching order; on unique conflict, re-read and compare. The transaction inserts Order revision 1 and an OrderCreated Outbox event containing the full snapshot. Different fingerprints return `ErrIdempotencyKeyReused`.

- [ ] **Step 5: Implement cancellation and payment state transactions**

`CancelOrder` re-reads by numeric UserID, keeps the row, transitions unpaid orders to Cancelled, and starts refund state for paid orders. `CreatePayment` requires an enabled type and no Processing attempt, increments attempt number, and inserts PaymentRecord plus PaymentChanged Outbox. Confirm/fail/refund commands re-read state, increment OrderRevision, and insert a full snapshot event in the same transaction.

- [ ] **Step 6: Run Order domain tests under race**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/06-shop-microservices/order-service/models ./examples/06-shop-microservices/order-service/business -count=1`

Expected: PASS; concurrent same-key commands return one Order ID, all successful state changes have increasing revisions, and no cancelled order is deleted.

- [ ] **Step 7: Commit Order facts**

```bash
git add examples/06-shop-microservices/order-service/models examples/06-shop-microservices/order-service/business
git commit -m "refactor(example-06): make order and payment facts reliable"
```

### Task 8: Make every Order operation an internal Public API and constrain Manage

**Files:**
- Delete: `examples/06-shop-microservices/order-service/api/private/orders.go`
- Create: `examples/06-shop-microservices/order-service/api/public/orders.go`
- Modify: `examples/06-shop-microservices/order-service/api/public/getpaymenttypes.go`
- Replace: `examples/06-shop-microservices/order-service/api/manage/manage.go`
- Create: `examples/06-shop-microservices/order-service/api/manage/manage_test.go`
- Modify: `examples/06-shop-microservices/order-service/service.go`
- Create: `examples/06-shop-microservices/order-service/service_test.go`

- [ ] **Step 1: Add route inventory and Manage command tests**

Assert there are no Private routers; all five Public routers list only `shop-user`; PaymentType exposes CRUD plus enable/disable; Order and PaymentRecord expose View/Search plus controlled commands and no generic Add/Edit/Remove.

- [ ] **Step 2: Run API tests and verify current boundary fails**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./examples/06-shop-microservices/order-service/api/... ./examples/06-shop-microservices/order-service -count=1`

Expected: FAIL because CreateOrder/GetOrders/CreatePayment/DeleteOrder are Private and route names do not match the approved API.

- [ ] **Step 3: Implement one route helper for internal Public operations**

```go
func orderPublicRoute(api interface{}, name, method string) *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(api,
		router.WithServiceName(contract.OrderServiceName),
		router.WithPath("/api/"+contract.OrderServiceName+"/"+name),
		router.WithPathType(servertypes.PublicType),
		router.WithAuth(false),
		router.WithMethod(method),
		router.WithInternalCallers(contract.UserServiceName),
	)
}
```

Define CreateOrder, CancelOrder, CreatePayment, GetOrders, and GetPaymentTypes. CreateOrder calls the real Supplier `public.GetProducts{ID: productID}` router; require exactly one enabled result and use that trusted DTO snapshot. GetOrders accepts only the numeric UserID supplied by the trusted User service call.

- [ ] **Step 4: Implement admin-only Manage routers**

Use Search/Do hooks to reject non-`platform-admin`. PaymentType Edit cannot change Enabled and cannot change Code after usage. OrderManage exposes View/Search/Cancel/Refund; PaymentRecordManage exposes View/Search/Confirm/Fail/ConfirmRefund. Each command calls the business state machine rather than generic ModelList updates.

- [ ] **Step 5: Publish payment-type events and invalidate local cache after commit**

PaymentType changes insert `PaymentTypeChanged` Outbox in the same transaction. `GetPaymentTypes` uses a 30-second cache; successful Manage After hooks invalidate it. Order worker publishes the three explicit order event subjects and payment-type subject.

- [ ] **Step 6: Run Order API/service tests**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/06-shop-microservices/order-service/... -count=1`

Expected: PASS; HTTP-style requests to constrained Public routes are rejected by Core before Parse and service-context calls from shop-user pass.

- [ ] **Step 7: Commit Order boundary**

```bash
git add -A examples/06-shop-microservices/order-service
git commit -m "refactor(example-06): expose order as internal public service"
```

### Task 9: Add User identity/address models and unified Manage hooks

**Files:**
- Modify: `examples/06-shop-microservices/user-service/models/models.go`
- Modify: `examples/06-shop-microservices/user-service/models/models_test.go`
- Create: `examples/06-shop-microservices/user-service/api/manage/manage.go`
- Create: `examples/06-shop-microservices/user-service/api/manage/manage_test.go`

- [ ] **Step 1: Add failing numeric identity and Manage permission tests**

Require `AuthUserID -> User.ID`, default enabled users, no physical User remove router, trusted Address.UserID injection, owner/admin search behavior, and disabled-user read-only behavior.

```go
func TestEnsureUserMapsAuthIdentityToStableNumericID(t *testing.T) {
	first, err := EnsureUser("auth-buyer-1", "Buyer")
	require.NoError(t, err)
	second, err := EnsureUser("auth-buyer-1", "Buyer")
	require.NoError(t, err)
	require.NotZero(t, first.ID)
	require.Equal(t, first.ID, second.ID)
	require.True(t, first.Enabled)
}
```

- [ ] **Step 2: Run User model/Manage tests and verify failures**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./examples/06-shop-microservices/user-service/models ./examples/06-shop-microservices/user-service/api/manage -count=1`

Expected: FAIL because current UserID is the auth string and no User Manage package exists.

- [ ] **Step 3: Implement numeric User and Address ownership**

Use:

```go
type User struct {
	*entity.Model
	AuthUserID string `gorm:"not null;uniqueIndex" json:"-"`
	Name       string `gorm:"not null"`
	Enabled    bool   `gorm:"not null"`
}

type Address struct {
	*entity.Model
	UserID    uint `gorm:"not null;index"`
	Recipient string
	Phone     string
	Region    string
	Detail    string
}
```

EnsureUser creates enabled rows idempotently. Address find/list functions accept numeric UserID. Address snapshots retain all address fields so deletion never affects historical orders.

- [ ] **Step 4: Implement UserManage and AddressManage hooks**

UserManage exposes View/Search/Edit plus admin-only enable/disable and no Remove. AddressManage exposes View/Search/Add/Edit/Remove. Search maps non-admin auth identity to numeric User.ID. Do hooks inject Address.UserID, reject owner/Enabled changes through generic Edit, and block every Address write when the User is disabled.

- [ ] **Step 5: Run User model and Manage tests**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/06-shop-microservices/user-service/models ./examples/06-shop-microservices/user-service/api/manage -count=1`

Expected: PASS.

- [ ] **Step 6: Commit User models and Manage**

```bash
git add examples/06-shop-microservices/user-service/models examples/06-shop-microservices/user-service/api/manage
git commit -m "refactor(example-06): add user and address manage hooks"
```

### Task 10: Rebuild User facades, buyer commands, cache, and WebSocket isolation

**Files:**
- Delete: `examples/06-shop-microservices/user-service/api/private/address.go`
- Modify: `examples/06-shop-microservices/user-service/api/private/orders.go`
- Modify: `examples/06-shop-microservices/user-service/api/private/payment.go`
- Create: `examples/06-shop-microservices/user-service/api/private/orders_test.go`
- Create: `examples/06-shop-microservices/user-service/api/public/getsuppliers.go`
- Modify: `examples/06-shop-microservices/user-service/api/public/getproducts.go`
- Modify: `examples/06-shop-microservices/user-service/api/public/getpaymenttypes.go`
- Modify: `examples/06-shop-microservices/user-service/service.go`
- Create: `examples/06-shop-microservices/user-service/service_test.go`

- [ ] **Step 1: Add failing buyer workflow and WebSocket tests**

Cover client-required requestID, disabled-user write rejection, trusted numeric UserID, owned address, same-key forwarding, cancel rather than delete, 10-second per-user cache key, and notification filtering by numeric UserID:

```go
func TestAddOrderRequiresClientRequestID(t *testing.T) {
	api := &AddOrder{ProductID: 1, Quantity: 1, AddressID: 1}
	err := api.Validation(enabledBuyerRequest(t))
	require.ErrorContains(t, err, "requestID")
}

func TestGetOrdersNoticeOnlyMatchesNumericUserID(t *testing.T) {
	subscription := &GetOrders{subscriptionUserID: 20}
	match, _ := subscription.NoticeFiltersRouter(&eventdto.OrderChanged{UserID: 20}, subscription)
	other, _ := subscription.NoticeFiltersRouter(&eventdto.OrderChanged{UserID: 21}, subscription)
	require.True(t, match)
	require.False(t, other)
}
```

- [ ] **Step 2: Run User API/service tests and verify failures**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./examples/06-shop-microservices/user-service/api/... ./examples/06-shop-microservices/user-service -count=1`

Expected: FAIL because current AddOrder generates request IDs server-side, addresses are Private APIs, and UserIDs are strings.

- [ ] **Step 3: Implement the three Public facades**

GetSuppliers calls Supplier Public GetSuppliers; GetProducts calls Supplier Public GetProducts; GetPaymentTypes calls Order Public GetPaymentTypes. They are ordinary external User Public routes with no internal caller allowlist. Each returns its own DTO slice and uses 30-second cache with normalized complete filter keys.

- [ ] **Step 4: Implement the four Private buyer APIs**

AddOrder accepts `requestID`, ProductID, Quantity, AddressID; maps token auth UID to enabled numeric User, loads owned Address, and calls Order CreateOrder with `IdempotencyKey = strconv.FormatUint(uint64(user.ID), 10) + ":" + requestID`. CancelOrder and CreatePayment require enabled User and forward numeric UserID. GetOrders permits disabled users, forwards numeric UserID, caches for 10 seconds by numeric UserID, and owns the WebSocket subscription.

- [ ] **Step 5: Consume cache and order events idempotently**

SupplierChanged/ProductChanged invalidate the matching facade caches; PaymentTypeChanged invalidates payment types. OrderCreated/OrderStatusChanged/PaymentChanged execute inside User Inbox idempotency: invalidate only that numeric user's GetOrders key and call `NoticeWebSocket` with the event. Unknown schema versions return an error and remain pending.

- [ ] **Step 6: Register the correct route inventory and auth lifecycle**

`Service.Routers()` contains UserManage, AddressManage, three Public facades, and four Private buyer APIs. It contains no Private address CRUD. `OnAuth` creates a User only for non-admin identity; disabled existing users can authenticate for reads but write APIs and Manage Do hooks fail closed.

- [ ] **Step 7: Run User tests under race**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/06-shop-microservices/user-service/... -count=1`

Expected: PASS; WebSocket filters never accept a client-supplied user ID and disabled users can read history and receive existing-order events.

- [ ] **Step 8: Commit User boundary**

```bash
git add -A examples/06-shop-microservices/user-service
git commit -m "refactor(example-06): expose buyer facade and private workflow"
```

### Task 11: Rewrite all-in-one integration tests around Manage ownership and internal-only routes

**Files:**
- Modify: `examples/integration/06-shop-microservices/helpers_test.go`
- Modify: `examples/integration/06-shop-microservices/manage_test.go`
- Modify: `examples/integration/06-shop-microservices/public_test.go`
- Modify: `examples/integration/06-shop-microservices/private_test.go`
- Modify: `examples/integration/06-shop-microservices/uat_test.go`

- [ ] **Step 1: Replace route-level integration expectations**

Add assertions for:

```go
func TestInternalPublicRoutesRejectDirectHTTP(t *testing.T) {
	for _, path := range []string{
		"/api/shop-supplier/getsuppliers",
		"/api/shop-supplier/getproducts",
		"/api/shop-order/getpaymenttypes",
		"/api/shop-order/createorder",
	} {
		response := suite.RequestJSON(t, http.MethodGet, path, "", nil)
		require.False(t, response.Success, path)
	}
}
```

Manage tests must create a Supplier via TestToken, update it through Manage, create a disabled Product through Manage, enable it with the command, verify cross-supplier isolation, and verify platform-admin visibility. User tests do the same for User/Address ownership and disable/read-only behavior.

- [ ] **Step 2: Run all-in-one integration and observe old-route failures**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./examples/integration/06-shop-microservices -count=1 -v`

Expected: FAIL until tests and services agree on Manage paths, requestID, CancelOrder, and facade-only external access.

- [ ] **Step 3: Implement the full buyer UAT**

The test sequence is fixed: supplier TestToken registration -> Supplier Manage edit -> Product Manage add and enable -> user registration -> Address Manage add -> User Public catalog/payment query -> AddOrder with requestID -> duplicate AddOrder returns same ID -> Supplier OrderManage sees full fulfillment projection -> CreatePayment -> admin confirms -> User WebSocket receives only buyer event -> CancelOrder keeps the row -> disabled buyer can still GetOrders but cannot create/cancel/pay/address-write.

- [ ] **Step 4: Add deletion and revision acceptance tests**

After OrderCreated projection arrives, assert Product Remove and Supplier Remove fail; Product disable succeeds; Supplier disable only works for admin. Publish duplicate and older-revision order events and assert the Supplier projection remains single and newest.

- [ ] **Step 5: Run all-in-one with race detection**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/integration/06-shop-microservices -count=1 -v`

Expected: PASS with real HTTP/TestToken/WebSocket and local insecure gRPC dispatch; constrained route trust comes only from the same-process source ServiceContext.

- [ ] **Step 6: Commit all-in-one acceptance**

```bash
git add examples/integration/06-shop-microservices
git commit -m "test(example-06): verify service boundaries all in one"
```

### Task 12: Prove remote mTLS identity, discovery, reliable events, and hidden Order HTTP

**Files:**
- Modify: `examples/integration/06-shop-microservices-three-process/three_process_test.go`
- Modify: `examples/06-shop-microservices/deploy/docker-compose.yml`
- Modify: `examples/06-shop-microservices/deploy/certs/README.md`

- [ ] **Step 1: Update the three-process test to use Manage and facade routes**

Create/enable products through Supplier Manage, create addresses through User Manage, query only User Public facade routes externally, and use User Private AddOrder/CancelOrder/CreatePayment/GetOrders. Never call Supplier or Order constrained Public routes over HTTP as a success path.

- [ ] **Step 2: Add negative remote identity tests**

Use raw gRPC PayloadRequest calls to the Order service for these cases: correct shop-user cert/source succeeds; shop-supplier cert claiming shop-user fails; shop-user cert claiming shop-supplier fails; certificate without matching service SAN fails; no client certificate fails handshake; valid mTLS but unlisted shop-supplier caller fails before Parse. Verify the Order count is unchanged after every rejected CreateOrder.

- [ ] **Step 3: Assert reliable projection and event fan-out**

After AddOrder, poll Supplier OrderManage until the OrderID and full address snapshot appear. Confirm payment and cancel through the intended services, then poll both User GetOrders and Supplier OrderManage until they show the same highest OrderRevision and statuses. Restart one consumer process and prove pending/replayed events remain idempotent.

- [ ] **Step 4: Remove Order host port exposure from Compose**

Keep no `ports` entry on `order`. Keep User and Supplier ports only for their intended external/management test entry points. Order remains reachable through the Compose network for internal gRPC and the internal management gateway/network contract.

- [ ] **Step 5: Run the real three-process suite**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/integration/06-shop-microservices-three-process -count=1 -v`

Expected: PASS; transport stats show User -> Order and Order -> Supplier gRPC, zero HTTP transport selection for service calls, rejected spoof attempts, and converged Supplier/User views.

- [ ] **Step 6: Validate Compose structure**

Run: `docker compose -f examples/06-shop-microservices/deploy/docker-compose.yml config`

Expected: PASS; rendered `order` service has no published host ports.

- [ ] **Step 7: Commit remote/deployment acceptance**

```bash
git add examples/integration/06-shop-microservices-three-process examples/06-shop-microservices/deploy
git commit -m "test(example-06): verify mtls calls and hidden order service"
```

### Task 13: Rewrite example and Core documentation

**Files:**
- Modify: `examples/06-shop-microservices/README.md`
- Modify: `docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- Modify: `docs/codex/ROUTERINFO_RUNTIME_GUIDE.md`
- Modify: `docs/codex/GRPC_TRANSPORT_MIGRATION.md`
- Modify: `docs/codex/API_COMPATIBILITY_SURFACE.md`
- Modify: `docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md`
- Modify: `docs/codex/CI_QUALITY_GATE_MATRIX.md`
- Modify: `.codex/skills/use-digitalway-core/SKILL.md`
- Modify: `.codex/skills/use-digitalway-core/references/core-backend-api.md`
- Create: `internal/compat/docs_contract_test.go`

- [ ] **Step 1: Add documentation contract checks**

Create a table-driven repository documentation test:

```go
func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return root
}

func TestCurrentDocsDescribeTrustedShopBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	required := map[string][]string{
		"examples/06-shop-microservices/README.md": {"WithInternalCallers", "SupplierOrder", "requestID"},
		"docs/codex/ROUTERINFO_RUNTIME_GUIDE.md": {"trusted internal caller", "x-internal-callers"},
		"docs/codex/GRPC_TRANSPORT_MIGRATION.md": {"mTLS SAN", "SourceService"},
		".codex/skills/use-digitalway-core/SKILL.md": {"WithInternalCallers", "SupplierOrder"},
	}
	for name, fragments := range required {
		contents, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		for _, fragment := range fragments {
			require.Contains(t, string(contents), fragment, name)
		}
	}
	readme, err := os.ReadFile(filepath.Join(root, "examples/06-shop-microservices/README.md"))
	require.NoError(t, err)
	require.NotContains(t, string(readme), "supplier-service/api/call")
}
```

- [ ] **Step 2: Rewrite the example README as an operational walkthrough**

Document the three audiences, exact route matrix, numeric identity mapping, Manage hook ownership, disabled read-only rules, requestID idempotency, local SupplierOrder projection, cache TTL/invalidation, Outbox/Inbox topology, same-process versus remote trust, and commands for unit/all-in-one/three-process verification.

- [ ] **Step 3: Add the Core capability to current guides**

Document this canonical declaration:

```go
func (g *GetProducts) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g,
		router.WithInternalCallers("shop-user", "shop-order"),
	)
}
```

State explicitly that SourceService is a claim, same-process trust comes from Source ServiceContext, remote trust requires verified mTLS SAN equality, HTTP has no internal identity, insecure/mesh remote calls cannot satisfy a constrained route without an independently verified mesh identity implementation, and rejection occurs before Parse.

- [ ] **Step 4: Merge the pattern into the repository skill**

Update the 06 example row and non-negotiable contracts in `SKILL.md`. In the reference, add the exact option/getter, caller trust matrix, Manage ownership pattern, permanent projection pattern, negative tests, and compatibility/release commands. Remove the obsolete statement that Supplier `api/call` is the recommended target-router location.

- [ ] **Step 5: Run documentation and compatibility gates**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test ./internal/compat -count=1`

Run: `./scripts/test.sh api-compat`

Run: `./scripts/test.sh release-contract`

Expected: all commands PASS and the route golden records internal caller allowlists.

- [ ] **Step 6: Commit documentation and skill updates**

```bash
git add examples/06-shop-microservices/README.md docs/codex .codex/skills/use-digitalway-core internal/compat/docs_contract_test.go
git commit -m "docs: document trusted internal service boundaries"
```

### Task 14: Run complete release verification and reconcile the design status

**Files:**
- Modify: `docs/superpowers/specs/2026-07-17-shop-microservices-service-boundary-redesign.md`
- Modify only if generated output changed: `internal/compat/testdata/*.golden.json`

- [ ] **Step 1: Format all changed Go files**

Run: `git diff --name-only c990ca6..HEAD -- '*.go' | xargs gofmt -w`

Expected: command exits 0 and changes only formatting in files already touched by this implementation.

- [ ] **Step 2: Run all example 06 tests**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/06-shop-microservices/... -count=1`

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/integration/06-shop-microservices -count=1`

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./examples/integration/06-shop-microservices-three-process -count=1`

Expected: all PASS.

- [ ] **Step 3: Run Core regression and static checks**

Run: `GOCACHE=/private/tmp/core-codex-gocache go test -race ./pkg/server/types ./pkg/server/router ./pkg/server/transport/grpc ./internal/compat -count=1`

Run: `go vet ./pkg/server/types ./pkg/server/router ./pkg/server/transport/grpc ./examples/06-shop-microservices/...`

Run: `./scripts/check-logging.sh`

Expected: all PASS; no event payload, token, claims, full address, or request body is logged.

- [ ] **Step 4: Run release gates**

Run: `./scripts/test.sh api-compat`

Run: `./scripts/test.sh release-contract`

Expected: PASS with no unreviewed public API, route, OpenAPI, config, or consumer compatibility drift.

- [ ] **Step 5: Update the design document status and completion evidence**

Change section 1 status to `Implemented and verified`. Add a short evidence list containing the exact successful commands from Steps 2-4 and the final commit hashes. Do not change approved business decisions.

- [ ] **Step 6: Inspect the final diff for unrelated user changes**

Run: `git status --short`

Run: `git diff --stat`

Run: `git diff --check`

Expected: no whitespace errors; only files listed by this plan are staged for the final commit, while pre-existing unrelated workspace changes remain untouched.

- [ ] **Step 7: Commit final verification evidence**

```bash
git add docs/superpowers/specs/2026-07-17-shop-microservices-service-boundary-redesign.md internal/compat/testdata/routes.golden.json internal/compat/testdata/openapi.golden.json
git commit -m "chore(example-06): record boundary redesign verification"
```

## Final acceptance checklist

- [ ] Supplier has Manage + constrained Public only; no Private and no `api/call`.
- [ ] User has unified User/Address Manage, external Public facades, four Private buyer APIs, and the only end-user order WebSocket.
- [ ] Order has admin Manage + five shop-user-only Public APIs, no Private API, no WebSocket, and no host-published HTTP port.
- [ ] AuthUserID appears only in User/Supplier persistence and identity lookup; cross-service IDs are numeric.
- [ ] Supplier/Product deletion checks use only permanent local SupplierOrder references.
- [ ] OrderCreated, OrderStatusChanged, PaymentChanged, and PaymentTypeChanged are transactional Outbox events; User/Supplier Inbox behavior is idempotent.
- [ ] Constrained routes reject HTTP, missing trust, wrong service, spoofed SourceService, no certificate, and mismatched certificate SAN before Parse/Validation/Do.
- [ ] Same-process all-in-one calls and remote mTLS three-process calls both pass through the real Core ServiceContext/ServiceResolver path.
- [ ] Route compatibility snapshots include internal caller allowlists and all release gates pass.
- [ ] Example README, current Core docs, and `use-digitalway-core` skill/reference describe the implemented behavior.
