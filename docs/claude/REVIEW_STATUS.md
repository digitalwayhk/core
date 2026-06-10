# Bug Fix Round Status

Tracking the progressive bug fix rounds from Copilot reviews.

## Round 1 (b59bc03 — Fix P0/P1/P2 from COPILOT_REVIEW_BUGS.md)

| ID | Issue | Status |
|----|-------|--------|
| P0 | Cluster/Transport/MQ not wired to service startup | ✅ Fixed |
| P1 | LocalProvider MachineID slot conflict not per-ServiceName | ✅ Fixed |
| P1 | WebSocket cross-node unregister/drain unreliable | ✅ Fixed |
| P2 | ProviderSwitcher can't migrate local provider nodes | ✅ Fixed |

## Round 2 (364fa5a — Fix P0/P1/P2 from COPILOT_REVIEW_BUGS_ROUND2.md)

| ID | Issue | Status |
|----|-------|--------|
| P0 | ClusterSwitcher doesn't update ServiceContext provider | ✅ Fixed |
| P1 | ProviderSwitcher warm-up still uses List(ctx, "") | ✅ Fixed |
| P1 | Mode=on cluster/MQ init errors swallowed by NewServiceContext | ✅ Fixed |
| P1 | TransportSelector not built per Transport.Internal/Fallback config | ✅ Fixed |
| P2 | WebSocket unregister should avoid negative hash count | ✅ Fixed |

## Round 3 (from COPILOT_REVIEW_BUGS_ROUND3.md)

| ID | Issue | Status |
|----|-------|--------|
| P1 | TransportConfig allows quic/mq but BuildSelector silently skips | ❌ Open |

## Round 4 — Test Coverage (from COPILOT_TEST_COVERAGE_BUGS_ROUND4.md)

| ID | Issue | Status |
|----|-------|--------|
| P1 | service/manage missing happy-path tests (Search/Edit/Submit) | ❌ Open |
| P1 | MQ/event-stream framework-level integration missing | ❌ Open |
| P1 | WayPlus frontend tests not runnable (missing deps) | ❌ Open |
| P2 | Missing Transport.Internal=mq panic test in ServiceContext | ❌ Open |
