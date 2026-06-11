# Unfinished Tasks — AI Working Document

> Last verified against actual code: 2026-06-10

## Summary

| Category | Total | Fixed | Remaining |
|----------|-------|-------|-----------|
| Copilot Bug Rounds 1-4 | All items | ✅ All fixed | 0 |
| P0 Optimization | 3 | ✅ All fixed | 0 |
| P1 Optimization | 5 | ✅ All fixed | 0 |
| P2 Optimization | 4 | 1 | 3 |
| Codex Release Plans | 18 | 0 | 18 |

---

## ✅ Fixed This Session

- P0: gRPC connection pool (`pkg/server/transport/grpc/client.go`)
- P0: Consul O(n) heartbeat scan (`pkg/server/cluster/provider_consul.go`)
- P0: Network retry in sendPayload (`pkg/server/router/servicecontext.go`)
- P1: etcd config TTL (`pkg/server/cluster/provider_etcd.go`, `factory.go`)
- P1: CrossNodeNoticeBroker TransportSelector wiring (`pkg/server/cluster/event.go`)
- P1: TransportConfig retry fields (`pkg/server/config/transportconfig.go`)
- P1: Socket protocol Health check (`pkg/server/transport/socket/socket_transport.go`)
- P2: ReadConfig test RetryDelay serialization (`servicecontext_test.go`)

## ❌ Remaining P2 Items

| ID | Issue |
|----|-------|
| P2-1 | SQLite/BadgerDB test failures (11) — connection leaks, WAL mode conflicts |
| P2-2 | QUIC/MQ transport adapters — `pkg/server/trans/quic/` exists but no Transport adapter |
| P2-3 | ProviderSwitcher no ongoing reconciliation during Begin→Complete migration |

---

## Codex Release Plans — All TODO (not worked on)

### Cluster / MQ / Transport Rollout
4 scenarios: request/reply, publish/subscribe, provider switch, node up/down

### Release Readiness (CORE-P0-001 through 005)
Go toolchain matrix, Docker env, WebSocket/Notice verification, Manage/Persistence compat, MQ config docs

### Dependent Services Risk
futures, omni-flow-ai-grok, ai-ops-platform compatibility verification

### WebSocket / Notice Acceptance
5 scenarios: public/private subscribe, hash push, cross-node notice, node drain

---

## Test Status (2026-06-10)

```
Server packages: 151 passed in 30 packages ✅
Persistence (pre-existing): 11 failed (SQLite + BadgerDB)
Full build: Success
```
