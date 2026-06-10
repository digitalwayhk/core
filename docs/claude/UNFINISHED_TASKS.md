# Unfinished Tasks — AI Working Document

> Auto-generated from all docs/codex/ and docs/copilot/ documents. Last scan: 2026-06-10.

## Summary

| Category | Total Items | Status |
|----------|-------------|--------|
| Codex Release Plans | 18 TODO items | All open |
| Copilot Bug Rounds | 5 remaining issues | 3 rounds fixed, Round 4 open |
| Test Coverage Gaps | 4 remaining | Round 4 open |

---

## 1. Copilot Bug Review — Remaining (Round 3→4)

### P1: TransportConfig quic/mq silently skipped by BuildSelector
- **Source**: `docs/copilot/COPILOT_REVIEW_BUGS_ROUND3.md`
- **Problem**: Config allows `quic`/`mq` as valid transport values, but `transport.BuildSelector` only builds `grpc`/`http`/`socket`. Silently skips with just a log.
- **Fix**: Either implement quic/mq adapters, or return explicit error instead of silent skip.
- **Location**: `pkg/server/transport/factory.go:16-22`

### P1: service/manage missing CRUD happy-path tests (Round 4)
- **Source**: `docs/copilot/COPILOT_TEST_COVERAGE_BUGS_ROUND4.md`
- **Missing tests**:
  - `TestSearch_HappyPath_LoadsListAndCallsSearchAfter`
  - `TestEdit_HappyPath_UpdatesOldItemAndSaves`
  - `TestSubmit_HappyPath_SetsStateAndSaves`
- **Location**: `service/manage/crud_test.go`

### P1: MQ/event-stream framework-level integration missing
- **Source**: `docs/copilot/COPILOT_TEST_COVERAGE_BUGS_ROUND4.md`
- **Problem**: `pkg/server/event.Stream` still uses local memory bus, not wired through `MQManager`. No test proving `event.Envelope` → MQ provider → event handler.
- **Location**: `pkg/server/event/stream.go`, `pkg/server/mq/factory.go`

### P1: WayPlus frontend tests not runnable
- **Source**: `docs/copilot/COPILOT_TEST_COVERAGE_BUGS_ROUND4.md`
- **Problem**: `web/admin` has no `node_modules`, no lockfile, `jest: command not found`. Test files exist but cannot execute.
- **Location**: `web/admin/`, `web/admin/src/components/WayPlus/__tests__/*.test.tsx`

### P2: Missing Transport.Internal=mq panic test in ServiceContext
- **Source**: `docs/copilot/COPILOT_TEST_COVERAGE_BUGS_ROUND4.md`
- **Need**: `TestNewServiceContextWithConfig_TransportMQ_Panics`
- **Location**: `pkg/server/router/servicecontext_test.go`

---

## 2. Codex Release Plans — All TODO

### Cluster / MQ / Transport Rollout (CLUSTER_MQ_TRANSPORT_ROLLOUT_PLAN.md)

| Scenario | Status |
|----------|--------|
| request/reply — timeout/success/failure observable | TODO |
| publish/subscribe — no message loss, replayable | TODO |
| provider switch — config change controllable, rollback safe | TODO |
| node up/down — draining doesn't lose events | TODO |

### Release Readiness (CORE_RELEASE_READINESS_PLAN.md)

| ID | Gap | Status |
|----|-----|--------|
| CORE-P0-001 | Go toolchain compatibility matrix for dependent projects | TODO |
| CORE-P0-002 | Docker local env for Redis/NATS/etcd/Consul tests | TODO |
| CORE-P0-003 | WebSocket/Notice multi-node verification | TODO |
| CORE-P0-004 | Manage/Persistence compatibility verification | TODO |
| CORE-P0-005 | MQ/EventBridge production config documentation | TODO |

### Dependent Services Risk (DEPENDENT_SERVICES_RISK_PLAN.md)

| Service | Risk | Status |
|---------|------|--------|
| futures | Amount precision, user/market parsing, push loss | TODO |
| omni-flow-ai-grok | GetHash/search compat, manage CRUD, trace audit | TODO |
| ai-ops-platform | Trading service health/metrics/events completeness | TODO |
| Compatibility matrix (Go version, route, storage, MQ) | All 4 items | TODO |

### WebSocket / Notice Acceptance (WEBSOCKET_NOTICE_ACCEPTANCE_PLAN.md)

| Scenario | Status |
|----------|--------|
| public subscribe — idempotent, subscribe/unsubscribe | TODO |
| private subscribe — token/user verification, no cross-user | TODO |
| hash push — only target market/user/symbol | TODO |
| cross-node notice — no loss, no duplicate | TODO |
| node drain — connection migration or reconnect | TODO |

---

## 3. Verification Commands

Quick checks to run for any change:

```bash
# Unit tests
go test ./...
go test -short ./...

# Targeted tests
go test ./pkg/server/...
go test ./pkg/persistence/...
go test ./service/manage/...

# Integration (needs env vars + permissions)
CORE_TEST_CLUSTER_LOCAL=1 go test -tags=integration ./tests/integration/... -run TestClusterLocal
CORE_TEST_REDIS_STREAM=1 CORE_TEST_NATS=1 go test -tags=integration ./tests/integration/... -run TestMQ
CORE_TEST_ETCD=1 go test -tags=integration ./tests/integration/... -run TestClusterEtcd
CORE_TEST_CONSUL=1 go test -tags=integration ./tests/integration/... -run TestClusterConsul
CORE_TEST_CLUSTER_LOCAL=1 go test -tags=integration ./tests/integration/... -run 'Test.*WebSocket|TestCrossNode'

# Smoke tests
go run ./examples/01-hello-router/main -p 18081
go run ./examples/demo/main -p 18080
```

---

## 4. Related Documents

- `docs/claude/REVIEW_STATUS.md` — Bug fix round status tracking
- `docs/copilot/COPILOT_REVIEW_BUGS_ROUND3.md` — Last bug review (P1 quic/mq)
- `docs/copilot/COPILOT_TEST_COVERAGE_BUGS_ROUND4.md` — Last coverage review
- `docs/codex/CORE_RELEASE_READINESS_PLAN.md` — Release plan with 5 P0 gaps
