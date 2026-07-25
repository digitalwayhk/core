# Bug Fix Round Status

Tracking the progressive bug fix rounds from Copilot reviews.

## Round 1 (commit b59bc03)
All P0/P1/P2 fixed: Cluster/Transport/MQ startup wiring, LocalProvider MachineID isolation, WebSocket cross-node, ProviderSwitcher warm-up.

## Round 2 (commit 364fa5a)
All P0/P1/P2 fixed: ClusterSwitcher sync, ProviderSwitcher List(ctx,"") fix, Mode=on panic, TransportSelector per-config, WebSocket unregister dedup.

## Round 3 (commit b59bc03)
P1 quic/mq: BuildSelector returns explicit error for Internal=quic/mq. Fallback entries warn and skip.

## Round 4 (various commits)
All items verified: CRUD happy-path tests, EventBridge framework integration, TransportMQ panic test all passing.

## P0/P1 Optimization Round (commit cb62c5c)

| ID | Issue | Status |
|----|-------|--------|
| P0 | Add gRPC connection pool (sync.Map) | ✅ Fixed |
| P0 | Fix ConsulProvider O(n) heartbeat/deregister scan | ✅ Fixed |
| P0 | Add network retry with exponential backoff to sendPayload | ✅ Fixed |
| P1 | Fix etcd hardcoded TTL → use config value | ✅ Fixed |
| P1 | Wire TransportSelector into CrossNodeNoticeBroker | ✅ Fixed |
| P1 | Add TransportConfig.MaxRetries/RetryDelay fields | ✅ Fixed |
| P1 | Add socket protocol ping roundtrip to Health check | ✅ Fixed |
| P2 | Fix ReadConfig test helper for RetryDelay serialization | ✅ Fixed |

## Remaining

| ID | Issue | Status |
|----|-------|--------|
| P2 | SQLite/BadgerDB test failures (11, pre-existing) | ❌ Open |
| P2 | QUIC/MQ transport adapters not implemented | ❌ Open |
| P2 | ProviderSwitcher no ongoing reconciliation during migration | ❌ Open |
