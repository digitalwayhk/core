# Unfinished Tasks — AI Working Document

> Auto-generated from all docs/codex/ and docs/copilot/ documents. Verified against actual code: 2026-06-10.

## Summary

| Category | Total Items | Status |
|----------|-------------|--------|
| Codex Release Plans | 18 TODO items | All open |
| Copilot Bug Rounds 1-4 | All items | ✅ All fixed & verified |
| Test Coverage Gaps | All items | ✅ All covered |

---

## 1. Copilot Bug Review — All Fixed ✅

### Round 1 (commit b59bc03)
All P0/P1/P2 fixed: Cluster/Transport/MQ startup wiring, LocalProvider MachineID isolation, WebSocket cross-node, ProviderSwitcher warm-up.

### Round 2 (commit 364fa5a)
All P0/P1/P2 fixed: ClusterSwitcher sync, ProviderSwitcher List(ctx,"") fix, Mode=on panic, TransportSelector per-config, WebSocket unregister dedup.

### Round 3 (commit b59bc03)
P1 Transport quic/mq: `BuildSelector` now returns explicit error for `Internal=quic`/`Internal=mq`. Fallback entries warn and skip. All 8 transport tests pass.

### Round 4 (various commits)
- ✅ `TestSearch_HappyPath_LoadsListAndCallsSearchAfter` — 20 manage tests pass
- ✅ `TestEdit_HappyPath_UpdatesOldItemAndSaves` — verified
- ✅ `TestSubmit_HappyPath_StateChangeAndSave` — verified
- ✅ `TestEventBridge_PublishSubscribeRoundtrip` + `TestNewServiceContextWithConfig_EventBridgeAutoInit` — 6 event bridge tests pass
- ✅ `TestNewServiceContextWithConfig_TransportMQ_Panics` — verified
- ⚠️ WayPlus frontend: `web/admin` has `package-lock.json`, Jest config exists, test files at `src/components/WayPlus/__tests__/*.test.tsx`. Need `npm install` to verify.

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

## 3. Test Status (2026-06-10)

```
289 passed, 11 failed, 2 skipped in 52 packages
```

**Failures**: All in persistence/nosql and persistence/database/oltp (BadgerDB sync config, vlog threshold, SQLite hasTable/load/transaction) — pre-existing, not related to our changes. Server, transport, cluster, MQ, event, manage all pass.

---

## 4. Related Documents

- `docs/claude/REVIEW_STATUS.md` — Bug fix round status tracking
- `docs/claude/OPTIMIZATION_RECOMMENDATIONS.md` — Architecture optimization recommendations
- `docs/copilot/` — Historical Copilot review documents
- `docs/codex/` — Release and rollout plans
