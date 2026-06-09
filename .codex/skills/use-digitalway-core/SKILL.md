---
name: use-digitalway-core
description: Build, modify, review, or explain services that use github.com/digitalwayhk/core. Use when working with digitalway.hk core services, IRouter APIs, entity.Model/BaseModel/ModelList, ManageService CRUD, api/public, api/private, api/manage, TestToken, WebSocket observes, EventBridge/MQ, cluster, transport, or examples under this repository.
---

# Use Digitalway Core

## Start Here

Treat `github.com/digitalwayhk/core` as a Go service framework. Before changing or generating code, inspect the closest existing example or sibling service and prefer the repository's current behavior over older Copilot notes.

For full API and implementation rules, read `references/core-backend-api.md`.

## Verified Core Rules

- Define API handlers as `types.IRouter`: `Parse(req)`, `Validation(req)`, `Do(req)`, and `RouterInfo()`.
- Use `router.DefaultRouterInfo(own)` for ordinary service routes under `api/public` and `api/private`.
- Ordinary public/private paths are `/api/{service}/{structnameLower}`. The package path controls auth type, but `public` and `private` are not included in the URL.
- Standard manage CRUD paths use `manage.RouterInfo`: `/api/manage/{service}/{manageStructLower}/{operationLower}`.
- Server management routes use `api.ServerRouterInfo`: `/api/servermanage/{structnameLower}`, then the framework rewrites the service segment when registering per service.
- In private handlers, get the current user with `req.GetUser()`.
- Use `entity.NewModelList[T](nil)` for persistence. `SearchWhere` defaults to a 500-row cap unless the caller changes `SearchItem.Size`; use `SearchAll(page, size)` for explicit pagination.
- Every model that embeds `*entity.Model` or `*entity.BaseModel` must initialize it in `NewModel()`, so `ModelList.NewItem()` can create usable records.
- `entity.BaseModel.GetHash()` hashes `Code`; if a model has no stable `Code`, use `entity.Model` or override hash behavior deliberately to avoid collisions and validation failures.
- Register all public/private/manage routers from `Service.Routers()`. Register post-route notifications in `SubscribeRouters()`.
- Do not depend on WayPage, JWT/Umi route generation, or unstable frontend integration helpers unless the user explicitly asks and the current code supports it.

## Workflow

1. Read the local target files first: `README.md`, `examples/*`, and the matching package under `pkg/`, `service/`, or `internal/core/{service}`.
2. If implementing a backend API, mirror the closest existing model, router, manage, and service registration pattern.
3. Keep one model file focused on one table and keep table operations near that model or its service layer.
4. Put parameter binding in `Parse`, validation/defaults in `Validation`, and side effects in `Do`.
5. Prefer service/model-list wrappers when a project provides them; do not bypass local DB routing, market/user parsing, or parent validation.
6. Verify with targeted `go test` or `go test ./...` when feasible, then run `gofmt` on edited Go files.

## Review Checklist

- Wrong URL assumptions, especially adding `/public` or `/private` to ordinary route calls.
- Missing `NewModel()` initialization on models.
- `BaseModel` without `Code` or hash override.
- `ManageService[T]` created with the wrong instance, causing hooks/view customization not to fire.
- Custom manage operation embedding `*manage.Operation[T]` instead of value `manage.Operation[T]`.
- Private route using request fields instead of `req.GetUser()` for authenticated identity.
- Skipped parent `Parse`, `Validation`, or hook methods in local base APIs.
- Unregistered routers, stale observe subscriptions, or websocket channels that use outdated paths.
