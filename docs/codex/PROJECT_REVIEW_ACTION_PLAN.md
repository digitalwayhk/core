# Core Project Review Action Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the project review recommendations into executable, verifiable work: prefer mature go-zero capabilities, remove dead or duplicate framework code, standardize production logging and exception ownership, secure request and configuration boundaries, isolate dependency changes, provide repeatable integration dependencies, establish compatibility and CI gates, and align tests and documentation.

**Architecture:** Treat this repository as a thin application-composition layer over go-zero and other already-selected mature libraries. Keep only domain-specific contracts such as Digitalway router/model conventions, MachineID isolation, provider switching, and cross-node notices; use thin adapters around mature infrastructure instead of reimplementing clients, caches, discovery loops, retries, concurrency primitives, or logging frameworks. Keep request state isolated, make configuration claims match runtime behavior, own subsystem lifecycle explicitly, use go-zero structured logging with trace context, record errors once at the owning boundary, and keep fast unit tests independent from Docker.

**Tech Stack:** Go 1.26 module, go-zero v1.10.2, `go test`, `go vet`, race detector, Docker Compose, etcd, Consul, Redis Streams, NATS JetStream, Kafka-compatible broker placeholder, MySQL, MongoDB, ClickHouse, and the existing `tests/integration` build tag.

---

## Current Baseline

| Area | Current state | Evidence / command |
| --- | --- | --- |
| Server vet | Passing | `go vet ./pkg/server/...` |
| Full-project vet | Failing | `go vet ./...` reports unkeyed `bson.E` values in `pkg/persistence/database/nosql/mongo.go` |
| Server tests | Passing when local port binding is allowed | `go test ./pkg/server/... -count=1` |
| Fast utility/manage tests | Passing | `go test ./service/manage ./pkg/utils ./pkg/persistence/types -count=1` |
| Persistence tests | Failing, environment-coupled, and slow to terminate | `go test ./pkg/persistence/... -count=1` fails default-delay/sync correctness tests, implicitly connects to local MySQL, and can leave retries/workers running after failures |
| Race checks | Not clean | A focused `-race` run exposes an asynchronous WebSocket subscription callback synchronization contract that is not documented or safely asserted |
| Integration tests | Correctly gated by build tag | `go test ./tests/integration` reports build constraints exclude files unless `-tags=integration` is supplied |
| External dependency tests | Partially implemented | etcd, Consul, Redis Streams, NATS JetStream exist; Kafka/RabbitMQ/RocketMQ are config-validated but not provider-implemented |
| go-zero reuse | Partial and inconsistent | Existing code uses `logx`, `httpx`, `conf`, and `rest`; go-zero v1.10.2 also provides unused candidates including `discov`, `stores/redis`, `stores/cache`, `mr`, `fx`, `threading`, `syncx`, and `zrpc` |
| Dead/incomplete code | Confirmed | `CacheAdapter.getCacheDB()` returns `(nil, nil)`, Mongo contains `panic("implement me")`, two SQLite registries duplicate ownership, and runtime packages contain debug `fmt.Print*` calls |
| Logging | Inconsistent and potentially unsafe | Runtime code mixes `fmt`, standard `log`, and `logx`; levels and languages vary; some logs include full payload/response/SQL; TraceID exists but is rarely attached to log context |
| Security defaults | Require hardening | Config migration writes permissive file modes, CORS defaults broadly, auth configuration uses package globals, and forwarded client IP headers are trusted without a proxy policy |
| Configuration contract | Incomplete | Several accepted MQ and cluster fields have no confirmed runtime consumer or behavioral test |
| CI/release governance | Missing | No repository workflow, required quality gates, exported API compatibility check, changelog, or release policy is present |
| Dependency upgrade state | Not clean | `go.mod` and `go.sum` are locally modified and should be handled as a separate dependency-upgrade task |

## Engineering Decision Rules

Apply these rules before adding or replacing framework code:

1. **go-zero first:** check the pinned go-zero version and its local source before creating infrastructure helpers.
2. **Thin adapters only:** preserve Digitalway public interfaces when they add domain value, but delegate connection management, retries, cache behavior, discovery, logging, and lifecycle handling to mature libraries.
3. **Do not confuse abstractions:** go-zero `core/queue` is an in-process producer/consumer queue, not a Kafka/NATS/Redis Streams broker implementation. Broker providers require a proven client or the separately versioned go-zero queue ecosystem.
4. **Protect domain semantics:** do not replace cluster or MQ abstractions until contract tests prove MachineID isolation, heartbeat/watch behavior, failover, acknowledgment, health checks, and provider switching remain intact.
5. **Delete with evidence:** classify a candidate as `remove`, `replace`, `keep-domain`, or `unsupported`; do not delete exported or runtime-reachable code based only on a text search.
6. **One migration per commit:** separate cache, discovery, concurrency, transport, and dead-code changes so each commit remains reviewable and revertible.
7. **Log events, not prose:** use stable ASCII `snake_case` event names and structured fields through go-zero `logx`; do not add a second logging facade.
8. **Log an error once:** lower layers wrap and return errors; the boundary that stops, retries, degrades, or responds records the event. A function must not log and return the same error unless it owns a fallback or terminal decision.
9. **No sensitive telemetry:** never log tokens, credentials, TOTP secrets/codes, full request/response bodies, complete payloads, DSNs, or raw SQL with values.
10. **Secure by default:** secrets use least-permissive storage, network trust is explicit, and missing security configuration fails closed rather than widening access.
11. **Configuration must be truthful:** every accepted field has a runtime consumer and behavior test; unsupported values fail validation or are removed through a documented deprecation.
12. **Request-local state only:** request, identity, trace, and mutable operation data must not be stored on shared service singletons or exposed through mutable global registries.
13. **Compatibility is a release contract:** public errors, routes, exported Go APIs, configuration, and consumer behavior require compatibility evidence or a documented migration path.

## Plan Decomposition

This file is the master index, dependency order, and completion ledger. Before implementing Tasks 11-17, create the corresponding focused plan under `docs/codex/plans/` and keep code-level steps, test cases, rollout notes, and accepted tradeoffs there:

| Task | Required implementation plan |
| --- | --- |
| 11 | `docs/codex/plans/11-security-auth-isolation.md` |
| 12 | `docs/codex/plans/12-request-lifecycle-concurrency.md` |
| 13 | `docs/codex/plans/13-persistence-correctness.md` |
| 14 | `docs/codex/plans/14-config-runtime-contract.md` |
| 15 | `docs/codex/plans/15-api-release-governance.md` |
| 16 | `docs/codex/plans/16-ci-quality-gates.md` |
| 17 | `docs/codex/plans/17-performance-slo-baseline.md` |

Tasks 6-9 must also create a focused sub-plan before an accepted migration changes code. The master plan records outcomes and evidence; it must not grow into a second implementation specification.

## Completion Tracking

Update this table after each task. A task is complete only when the command in `Completion evidence` passes and the commit hash is recorded.

| Task | Status | Commit | Completion evidence |
| --- | --- | --- | --- |
| 1. Dependency upgrade isolation | Completed | `f72447f` | `go mod verify`, `go mod tidy -diff`, server vet, and scoped compatibility tests pass; dependency files are committed separately |
| 2. Docker Compose integration stack | Not started |  | Default profile is healthy for etcd/consul/redis/nats; `--profile kafka` is healthy when explicitly requested |
| 3. Test command script | Completed | `0d29df1` | `bash -n`, `quick`, and `server` pass; an unknown mode exits 2 with usage and the script does not require `rtk` |
| 4. External integration tests via Docker | Not started |  | `./scripts/test.sh integration-external` passes etcd/consul/redis/nats suites |
| 5. Kafka provider gap decision | Not started |  | Either provider tests are implemented or docs explicitly mark Kafka as config-only |
| 6. go-zero capability and reuse audit | Not started |  | `docs/codex/GO_ZERO_REUSE_AUDIT.md` records evidence and a keep/replace/remove decision for every reviewed subsystem |
| 7. Dead and incomplete code cleanup | Not started |  | Enabled runtime paths contain no known placeholder implementations; every removal/replacement has focused tests |
| 8. Global logging and exception audit | Not started |  | Runtime logs use `logx` structured events, carry trace context at request/cross-service boundaries, pass sensitive-data scans, and contain no unapproved console/fatal output |
| 9. Architecture hardening backlog | Not started |  | Issues are either fixed or converted to tracked docs with file paths and test commands |
| 10. README/docs and scenario usage guide | Not started |  | README, skill reference, and scenario guide agree on routes, models, maturity, logging, and reuse policy |
| 11. Security baseline and authentication isolation | In progress (11.1-11.4 complete) | `a8f1c0d`, `804a2de`, `937d381`, `daa2c57`, `5e4bcd8` | Config files are `0600`, CORS is explicit, Logto policy is isolated, and auth/internal causes are hidden; trusted proxies and security headers remain |
| 12. Request isolation, global state, and lifecycle | Not started |  | Request/race/lifecycle tests pass with idempotent shutdown and no known leaked workers |
| 13. Persistence correctness and external-test separation | Not started |  | Persistence unit tests pass without external services; Docker-backed database suites pass when enabled |
| 14. Configuration-to-runtime capability contract | Not started |  | Every accepted MQ/cluster/transport field has a runtime consumer and behavior test or is rejected |
| 15. Public API compatibility and release governance | Not started |  | Typed error, route/API snapshot, deprecation, changelog, and consumer compatibility checks pass |
| 16. CI quality gates and consumer compatibility matrix | Not started |  | Required CI tiers pass from a clean checkout and publish actionable failure artifacts |
| 17. Performance, capacity, and operational SLO baseline | Not started |  | Benchmarks, budgets, RED/USE metrics, traces, and SLO checks have recorded baselines and owners |

## Task 1: Dependency Upgrade Isolation

**Files:**
- Review: `go.mod`
- Review: `go.sum`
- Modify if needed: no code files in this task

- [x] **Step 1: Inspect current dependency drift**

Run:

```bash
git diff --stat -- go.mod go.sum
git diff -- go.mod | sed -n '1,220p'
```

Expected: only dependency version and direct/indirect classification changes appear.

- [x] **Step 2: Decide commit boundary**

If the dependency upgrade is intended, commit only `go.mod` and `go.sum`:

```bash
git add go.mod go.sum
git commit -m "chore: update core dependency versions"
```

If the dependency upgrade is not intended, ask before reverting because these files are already user/local changes.

- [x] **Step 3: Verify dependency-upgrade compatibility**

Run:

```bash
go vet ./pkg/server/...
go test ./pkg/server/... ./pkg/utils ./service/manage ./pkg/persistence/types -count=1
```

Expected: both commands exit 0.

## Task 2: Docker Compose Integration Stack

**Files:**
- Create: `docker-compose.integration.yml`
- Create: `.env.integration.example`
- Modify: `.gitignore` if local Docker volumes or env files need ignoring

- [ ] **Step 1: Add Compose services**

Create `docker-compose.integration.yml` with this content. These are reviewed version pins, not `latest`; verify their official image manifests and record immutable digests when implementing the task. All unauthenticated integration ports bind to host loopback only.

```yaml
name: digitalway-core-integration

services:
  etcd:
    image: gcr.io/etcd-development/etcd:v3.6.11
    command:
      - /usr/local/bin/etcd
      - --name=core-etcd
      - --data-dir=/etcd-data
      - --listen-client-urls=http://0.0.0.0:2379
      - --advertise-client-urls=http://etcd:2379
      - --listen-peer-urls=http://0.0.0.0:2380
      - --initial-advertise-peer-urls=http://etcd:2380
      - --initial-cluster=core-etcd=http://etcd:2380
      - --initial-cluster-state=new
      - --initial-cluster-token=core-integration
    ports:
      - "127.0.0.1:2379:2379"
    healthcheck:
      test: ["CMD", "etcdctl", "--endpoints=http://127.0.0.1:2379", "endpoint", "health"]
      interval: 5s
      timeout: 3s
      retries: 20

  consul:
    image: hashicorp/consul:1.21.3
    command: ["agent", "-dev", "-client=0.0.0.0", "-log-level=warn"]
    ports:
      - "127.0.0.1:8500:8500"
    healthcheck:
      test: ["CMD", "consul", "members"]
      interval: 5s
      timeout: 3s
      retries: 20

  redis:
    image: redis:7.2-alpine
    command: ["redis-server", "--appendonly", "no"]
    ports:
      - "127.0.0.1:6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 20

  nats:
    image: nats:2.12.8-alpine
    command: ["-js", "-sd", "/data"]
    ports:
      - "127.0.0.1:4222:4222"
      - "127.0.0.1:8222:8222"
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8222/healthz"]
      interval: 5s
      timeout: 3s
      retries: 20

  kafka:
    profiles: ["kafka"]
    image: apache/kafka:4.3.1
    environment:
      KAFKA_NODE_ID: "1"
      KAFKA_PROCESS_ROLES: "broker,controller"
      KAFKA_CONTROLLER_QUORUM_VOTERS: "1@kafka:9093"
      KAFKA_LISTENERS: "PLAINTEXT://:9092,CONTROLLER://:9093"
      KAFKA_ADVERTISED_LISTENERS: "PLAINTEXT://127.0.0.1:9092"
      KAFKA_CONTROLLER_LISTENER_NAMES: "CONTROLLER"
      KAFKA_INTER_BROKER_LISTENER_NAME: "PLAINTEXT"
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: "1"
      KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: "1"
      KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: "1"
      KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS: "0"
    ports:
      - "127.0.0.1:9092:9092"
    healthcheck:
      test: ["CMD", "/opt/kafka/bin/kafka-broker-api-versions.sh", "--bootstrap-server", "127.0.0.1:9092"]
      interval: 10s
      timeout: 5s
      retries: 30
```

- [ ] **Step 2: Add environment example**

Create `.env.integration.example`:

```bash
CORE_TEST_CLUSTER_LOCAL=1
CORE_TEST_ETCD=1
ETCD_ENDPOINTS=127.0.0.1:2379
CORE_TEST_CONSUL=1
CONSUL_HTTP_ADDR=127.0.0.1:8500
CORE_TEST_REDIS_STREAM=1
CORE_TEST_REDIS_ADDR=127.0.0.1:6379
CORE_TEST_NATS=1
CORE_TEST_NATS_URL=nats://127.0.0.1:4222
# Enable only after Task 5 adds a Kafka provider and contract test:
# CORE_TEST_KAFKA=1
CORE_TEST_KAFKA_BROKERS=127.0.0.1:9092
```

- [ ] **Step 3: Verify stack starts**

Run:

```bash
docker compose -f docker-compose.integration.yml up -d
docker compose -f docker-compose.integration.yml --profile kafka up -d kafka
docker compose -f docker-compose.integration.yml --profile kafka ps
```

Expected: etcd, consul, redis, and nats are healthy in the default profile; Kafka is healthy only when the explicit profile is requested.

## Task 3: Test Command Script

**Files:**
- Create: `scripts/test.sh`
- Modify: `.gitignore` only if local artifacts are introduced

- [x] **Step 1: Create script directory and script**

Create `scripts/test.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

case "${1:-quick}" in
  quick)
    go vet ./pkg/server/...
    go test ./pkg/utils ./service/manage ./pkg/persistence/types -count=1
    ;;
  server)
    go vet ./pkg/server/...
    go test ./pkg/server/... -count=1
    ;;
  integration-local)
    CORE_TEST_CLUSTER_LOCAL=1 go test -tags=integration ./tests/integration -run TestClusterLocal -count=1
    ;;
  integration-external)
    CORE_TEST_ETCD=1 \
    ETCD_ENDPOINTS="${ETCD_ENDPOINTS:-127.0.0.1:2379}" \
    CORE_TEST_CONSUL=1 \
    CONSUL_HTTP_ADDR="${CONSUL_HTTP_ADDR:-127.0.0.1:8500}" \
    CORE_TEST_REDIS_STREAM=1 \
    CORE_TEST_REDIS_ADDR="${CORE_TEST_REDIS_ADDR:-127.0.0.1:6379}" \
    CORE_TEST_NATS=1 \
    CORE_TEST_NATS_URL="${CORE_TEST_NATS_URL:-nats://127.0.0.1:4222}" \
    go test -tags=integration ./tests/integration -run 'TestClusterEtcd|TestClusterConsul|TestMQ' -count=1
    ;;
  all)
    "$0" quick
    "$0" server
    "$0" integration-local
    "$0" integration-external
    ;;
  *)
    echo "usage: scripts/test.sh {quick|server|integration-local|integration-external|all}" >&2
    exit 2
    ;;
esac
```

- [x] **Step 2: Make it executable**

Run:

```bash
chmod +x scripts/test.sh
```

- [x] **Step 3: Verify quick modes**

Run:

```bash
scripts/test.sh quick
scripts/test.sh server
```

Expected: both commands exit 0.

## Task 4: External Integration Tests via Docker

**Files:**
- Modify: `docs/codex/AUTOMATED_VERIFICATION_PLAN.md`
- Read: `tests/integration/etcd_provider_test.go`
- Read: `tests/integration/consul_provider_test.go`
- Read: `tests/integration/mq_provider_test.go`

- [ ] **Step 1: Start dependencies**

Run:

```bash
docker compose -f docker-compose.integration.yml up -d
```

Expected: compose creates or reuses all services.

- [ ] **Step 2: Run existing external integration tests**

Run:

```bash
./scripts/test.sh integration-external
```

Expected: tests named `TestClusterEtcd*`, `TestClusterConsul*`, `TestMQRedisStream`, `TestMQNATSJetStream`, `TestMQEventStreamRedis`, and `TestMQEventStreamNATS` pass.

- [ ] **Step 3: Document exact local command**

Append this section to `docs/codex/AUTOMATED_VERIFICATION_PLAN.md`:

```markdown
## Docker-backed External Dependencies

Start local dependencies:

```bash
docker compose -f docker-compose.integration.yml up -d
```

Run external integration tests:

```bash
scripts/test.sh integration-external
```

Stop local dependencies:

```bash
docker compose -f docker-compose.integration.yml down
```
```

## Task 5: Kafka Provider Gap Decision

**Files:**
- Review: `pkg/server/config/mqconfig.go`
- Review: `pkg/server/mq/factory.go`
- Create if implementing: `pkg/server/mq/provider_kafka.go`
- Create if implementing: `tests/integration/kafka_provider_test.go`
- Modify if deferring: `docs/codex/AUTOMATED_VERIFICATION_PLAN.md`

- [ ] **Step 1: Confirm current Kafka behavior**

Run:

```bash
rg -n 'case "kafka"|provider=kafka|CORE_TEST_KAFKA|NewKafka' pkg/server tests
```

Expected today: config validates `kafka`, factory returns an unimplemented-provider error, and there is no Kafka integration test.

- [ ] **Step 2A: If implementing Kafka provider, write a failing integration test**

Create `tests/integration/kafka_provider_test.go`:

```go
//go:build integration

package integration_test

import (
	"os"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/mq"
)

func TestMQKafka(t *testing.T) {
	if os.Getenv("CORE_TEST_KAFKA") == "" {
		t.Skip("CORE_TEST_KAFKA not set")
	}
	brokers := os.Getenv("CORE_TEST_KAFKA_BROKERS")
	if brokers == "" {
		brokers = "127.0.0.1:9092"
	}
	p := mq.NewKafkaProvider(strings.Split(brokers, ","), "core-integration")
	runMQContract(t, p)
}
```

Run:

```bash
CORE_TEST_KAFKA=1 CORE_TEST_KAFKA_BROKERS=127.0.0.1:9092 go test -tags=integration ./tests/integration -run TestMQKafka -count=1
```

Expected before implementation: compile fails because `mq.NewKafkaProvider` is undefined.

- [ ] **Step 2B: If deferring Kafka provider, document it as config-only**

Add this note to `docs/codex/AUTOMATED_VERIFICATION_PLAN.md`:

```markdown
### Kafka Status

Kafka is available in `MQConfig` validation and in the Docker integration stack, but `pkg/server/mq` does not yet implement a Kafka `MQProvider`. Do not enable `CORE_TEST_KAFKA` until `provider_kafka.go` and `tests/integration/kafka_provider_test.go` are added.
```

## Task 6: go-zero Capability and Reuse Audit

**Files:**
- Create: `docs/codex/GO_ZERO_REUSE_AUDIT.md`
- Review: `go.mod`
- Review: `pkg/server/config/serverconfig.go`
- Review: `pkg/server/trans/rest/server.go`
- Review: `pkg/server/mq`
- Review: `pkg/server/cluster`
- Review: `pkg/persistence/adapter/cache.go`
- Review: `pkg/persistence/database/nosql/redis.go`
- Review: `pkg/utils/concurrency.go`
- Read only: `${GOMODCACHE}/github.com/zeromicro/go-zero@v1.10.2`

- [ ] **Step 1: Record the exact go-zero surface used by this repository**

Run:

```bash
rg -l 'github.com/zeromicro/go-zero' pkg service examples tests --glob '*.go'
rg -n 'conf\.(MustLoad|Load)|rest\.(NewServer|MustNewServer)|zrpc\.|stores/redis|stores/cache|core/discov|core/mr|core/fx|core/threading' pkg service examples tests --glob '*.go'
go list -deps ./pkg/server/...
```

Expected: the audit distinguishes packages actually used from packages merely available in go-zero v1.10.2. Current evidence shows real use of `logx`, `httpx`, `conf`, and `rest`; there is no current use of `stores/cache`, `stores/redis`, `discov`, `mr`, `fx`, `threading`, or `zrpc`.

- [ ] **Step 2: Create the reuse decision matrix**

Create `docs/codex/GO_ZERO_REUSE_AUDIT.md` with this initial matrix, then add source links and test evidence for every changed decision:

```markdown
# go-zero Reuse Audit

Pinned dependency: `github.com/zeromicro/go-zero v1.10.2`.

| Area | Current implementation | Mature candidate | Initial decision | Required proof |
| --- | --- | --- | --- | --- |
| Configuration | `serverconfig.go` already uses go-zero `conf` | `core/conf`, `core/configcenter` | Keep and standardize; remove duplicate parsing only after config compatibility tests | Existing JSON migration/default tests remain green |
| Logging and recovery | Mixed `logx`, `fmt.Print*`, and local panic recovery | `core/logx`, `core/rescue`, `core/threading` | Standardize runtime logging on `logx`; evaluate recovery helpers per call site | Panic/error behavior and log fields are tested |
| HTTP runtime | go-zero `rest.Server` is already wrapped by Digitalway REST code; Fiber remains elsewhere | `rest`, `zrpc` | Keep current public wrapper; audit Fiber usage before any server migration | Route/auth/WebSocket/OpenAPI compatibility suite |
| Generic Redis KV | Local `nosql.Redis` creates and pings a new client per operation | `core/stores/redis` | Replace client lifecycle with a shared go-zero Redis adapter | ICache contract plus Docker Redis test |
| Cache-aside | `CacheAdapter.getCacheDB()` returns `(nil, nil)` | `core/stores/cache` for cache-aside; `core/stores/redis` for plain KV | Replace or remove based on real callers; do not keep the nonfunctional adapter | Caller inventory and cache hit/miss/TTL tests |
| SQL persistence | Digitalway model contracts use GORM | go-zero `sqlx/sqlc` | Keep GORM; do not run two ORM/data-access stacks without a measured need | Existing ModelList/manage contracts remain green |
| etcd discovery | Custom provider adds MachineID, heartbeat, watch, and service semantics | `core/discov` publisher/subscriber | Build a compatibility spike; reuse go-zero etcd lifecycle only if domain contract remains intact | Cluster provider contract and Docker etcd tests |
| Consul discovery | Custom provider | No equivalent in pinned go-zero core | Keep behind the common provider interface | Consul contract test |
| MQ abstraction | `MQProvider`, switching, EventBridge, Redis Streams, NATS JetStream | Mature broker clients; separate go-zero queue ecosystem if approved | Keep the domain abstraction; simplify provider internals only | Publish/subscribe/ack/health/switch/rollback tests |
| In-process queue | Local framework code where applicable | go-zero `core/queue` | Use only for process-local producer/consumer pipelines, never as broker replacement | Shutdown/backpressure tests |
| Concurrency helpers | `ConcurrencyTasks` preserves ordered results and aggregates errors | `core/mr`, `core/fx`, `core/threading`, `core/syncx` | Compare contracts; replace only where ordered results, limits, cancellation, and panic behavior match | Existing utility tests plus cancellation/race tests |
| Retry/timeout/lifecycle | Ad hoc loops in providers and persistence | `core/fx`, `core/service`, `core/proc`, breaker helpers | Prefer go-zero primitives in new code; migrate one subsystem at a time | Deterministic retry/shutdown tests |
```

- [ ] **Step 3: Turn each `replace` decision into an isolated migration plan**

Create one implementation branch per accepted replacement. The first recommended slice is Redis KV/cache because the current adapter is incomplete and the local Redis wrapper reconnects for every operation. Do not combine it with cluster, MQ, or HTTP migrations.

- [ ] **Step 4: Verify that reuse does not weaken Digitalway contracts**

Run after every migration:

```bash
go test ./pkg/persistence/... ./pkg/server/... ./service/manage/... -count=1
go test -race ./pkg/utils ./pkg/server/cluster ./pkg/server/mq -count=1
```

Expected: all existing behavioral contracts pass; replacement is rejected or revised if it changes public routes, model behavior, MachineID isolation, broker acknowledgment, or provider switching.

## Task 7: Dead and Incomplete Code Cleanup

**Files:**
- Create: `docs/codex/DEAD_CODE_AUDIT.md`
- Review: `pkg/persistence/adapter/cache.go`
- Review: `pkg/persistence/adapter/nosql.go`
- Review: `pkg/persistence/database/nosql/mongo.go`
- Review: `pkg/persistence/entity/modellist.go`
- Review: `pkg/persistence/adapter/default.go`
- Review: `pkg/server/safe/twosteps/google.go`
- Review: `pkg/server/trans/quic`
- Review: runtime `fmt.Print*` calls under `pkg/server` and `pkg/utils`

- [ ] **Step 1: Create a classified cleanup register**

Create `docs/codex/DEAD_CODE_AUDIT.md` with these confirmed candidates:

```markdown
# Dead and Incomplete Code Audit

| Candidate | Risk | Initial classification | Exit condition |
| --- | --- | --- | --- |
| `pkg/persistence/adapter/cache.go` | `getCacheDB()` returns `(nil, nil)`, so public methods can dereference nil | replace or remove | Real callers identified; contract implemented on go-zero Redis/cache or exported adapter removed |
| `pkg/persistence/adapter/nosql.go` | Large commented implementation obscures the supported persistence path | remove | No live references require the commented code; history remains available in Git |
| `pkg/persistence/database/nosql/mongo.go` | Enabled method contains `panic("implement me")` and another prints a placeholder result | implement or mark unsupported | Enabled methods return correct results or explicit errors; no placeholder panic remains |
| SQLite registries in `entity/modellist.go` and `adapter/default.go` | Two global maps can create separate instances for the same database | consolidate | One concurrency-safe owner and tests proving instance reuse |
| `pkg/server/safe/twosteps/google.go` debug output | May print TOTP secret and verification codes | remove immediately | No secret/code output in library runtime paths; behavior tests pass |
| Runtime `fmt.Print*` calls | Bypass structured logging and may expose data | move to Task 8 logging audit | Runtime packages use structured `logx`; only explicit CLI/example output remains |
| QUIC stub/legacy transport code | May be unreachable, incomplete, or selected only by build tags | verify before removal | Build-tag matrix and factory references prove keep/remove decision |
```

- [ ] **Step 2: Verify reachability before deleting exported code**

Run:

```bash
rg -n 'CacheAdapter|NewRedis\(|Mongo|globalSqliteInstances|twosteps|TransportQUIC|quic' . --glob '*.go'
rg -n 'implement me|fmt\.(Print|Printf|Println)' pkg service --glob '*.go'
go list ./...
```

Expected: each candidate has its callers, build tags, and public exposure recorded. Tests, examples, generated files, and runtime files are classified separately.

- [ ] **Step 3: Verify the Task 11 secret-output fix in the cleanup register**

Task 11 owns removal of TOTP secrets/codes from `pkg/server/safe/twosteps/google.go` and its behavioral tests. Task 7 records the result and verifies no dead or placeholder path reintroduces the output:

```bash
go test ./pkg/server/safe/... -count=1
rg -n 'fmt\.(Print|Printf|Println).*secret|fmt\.(Print|Printf|Println).*code' pkg/server/safe --glob '*.go'
```

Expected: tests pass and the second command returns no runtime secret/code print.

- [ ] **Step 4: Replace or remove the broken cache path with tests first**

Add an `ICache` contract test covering get, set, delete, TTL, scan/search behavior, unavailable Redis, and client reuse. Implement it using go-zero `core/stores/redis`; use `core/stores/cache` only for cache-aside behavior that needs miss suppression. Remove `CacheAdapter` if repository-wide caller inventory is empty.

Run:

```bash
go test ./pkg/persistence/adapter ./pkg/persistence/database/nosql -count=1
CORE_TEST_REDIS_STREAM=1 CORE_TEST_REDIS_ADDR=127.0.0.1:6379 go test -tags=integration ./tests/integration -count=1
```

Expected: unit tests pass without Docker; the opt-in Redis contract passes with Compose running.

- [ ] **Step 5: Consolidate SQLite ownership independently**

Move instance creation to one concurrency-safe registry used by both ModelList and adapters. Add a parallel test asserting the same logical database name returns one instance and run with `-race`.

```bash
go test -race ./pkg/persistence/entity ./pkg/persistence/adapter -count=1
```

Expected: PASS with no race report and only one `globalSqliteInstances` owner remaining.

- [ ] **Step 6: Resolve incomplete Mongo and legacy NoSQL code**

For every method reachable through a supported configuration, return an explicit typed error until a complete implementation and integration test exist; never leave runtime placeholder panics. Delete commented implementations once live references are ruled out.

```bash
go test ./pkg/persistence/database/nosql ./pkg/persistence/adapter -count=1
rg -n 'panic\("implement me"\)|mongo implement|TODO implement me' pkg/persistence --glob '*.go'
```

Expected: tests pass and the scan returns no placeholder implementation on enabled runtime paths.

## Task 8: Global Logging and Exception Audit

**Files:**
- Create: `docs/codex/LOGGING_AUDIT_AND_STANDARD.md`
- Create: `scripts/check-logging.sh`
- Modify: `pkg/server/router/request.go`
- Modify: `pkg/server/router/servicecontext.go`
- Modify: `pkg/server/trans/rest/server.go`
- Modify: `pkg/server/safe/logto/authmiddleware.go`
- Modify: `pkg/server/safe/twosteps/google.go`
- Review and modify by batch: `pkg/server/cluster`, `pkg/server/mq`, `pkg/server/event`, `pkg/server/transport`, `pkg/server/trans`, `pkg/persistence`, `pkg/utils`, `service/manage`
- Test: focused tests beside each changed package

- [ ] **Step 1: Produce a complete runtime log inventory**

Run these scans and copy every live runtime finding into `docs/codex/LOGGING_AUDIT_AND_STANDARD.md`. Classify examples, tests, comments, and generated files separately; only `pkg` and `service` are production-library scope.

```bash
rg -n 'fmt\.(Print|Printf|Println)|log\.(Print|Printf|Println|Fatal|Fatalf|Panic|Panicf)' pkg service --glob '*.go'
rg -n 'logx\.(Debug|Debugf|Info|Infof|Error|Errorf|Severe|Severef|Slow|Slowf|Infow|Errorw|Debugw|Sloww)' pkg service --glob '*.go'
rg -n 'logx\..*(payload|request|response|body|token|password|passwd|secret|authorization|cookie|sql)|fmt\..*(token|password|passwd|secret|authorization|cookie)' pkg service --glob '*.go' -i
rg -n 'logx\.(Error|Errorf).*(retry|fallback|degrad|skip|降级|跳过|重试)' pkg service --glob '*.go' -i
rg -n 'request.?id|trace.?id|span.?id|x-request-id|TraceID|RequestID' pkg service --glob '*.go' -i
```

Expected: the register records file, line, current level, event purpose, sensitive-data risk, duplicate-error risk, target action, and verification command. Confirmed starting findings include standard-console output, `log.Fatalf` inside a library constructor, full payload/response/SQL logging, recoverable fallbacks logged as errors, decorative banners/icons, and TraceID propagation without consistent log binding.

- [ ] **Step 2: Establish one logging contract based on go-zero `logx`**

Create `docs/codex/LOGGING_AUDIT_AND_STANDARD.md` with this normative section:

```markdown
# Logging Audit and Standard

## Runtime Contract

- Use go-zero `logx`; do not introduce another logging facade.
- Event names are stable ASCII `snake_case`, for example `service_started`, `transport_fallback`, and `mq_publish_failed`.
- Use structured fields through `logx.Infow`, `Errorw`, `Debugw`, `Sloww`, `Field`, and `ContextWithFields`.
- Runtime event text is concise English. User-facing validation errors may remain localized because they are API content, not log event names.
- Required context when available: `service`, `trace_id`, `route`, `method`, `operation`, `provider`, `node_id`, `attempt`, `duration_ms`, and `error`.
- Never log complete payloads, bodies, responses, tokens, credentials, cookies, TOTP values, DSNs, or raw SQL containing values.

## Levels

| API | Use |
| --- | --- |
| `Errorw` | Final operation failure, broken invariant, data loss risk, panic recovery, or dependency failure with no successful fallback |
| `Infow` | Service lifecycle, provider switch, successful recovery, or handled degradation/fallback that operators should know about |
| `Debugw` | Per-attempt retry, route registration detail, worker lifecycle, cache detail, and other high-volume diagnostics |
| `Sloww` | A measured operation exceeded its configured latency threshold |
| `Severe` | Process startup boundary only; never terminate the process from a reusable library package |

## Error Ownership

1. A lower layer adds operation context with `%w` and returns the error.
2. The layer that retries logs attempts at debug level.
3. If fallback succeeds, log one info event describing the degradation.
4. If all recovery fails, the boundary logs one error event and returns or responds.
5. Do not log and return the same unchanged error from every layer.

## Remove or Demote

- Remove separators, icons, success slogans, object dumps, and duplicated stack traces.
- Demote per-worker, per-route, per-record, and retry-attempt messages to debug unless they indicate final loss.
- Replace repeated status logs with metrics when the question is aggregate rate, latency, queue depth, memory, or connection count.
```

- [ ] **Step 3: Add a static logging guard**

Create `scripts/check-logging.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

failed=0

check_forbidden() {
  local description="$1"
  local pattern="$2"
  if rg -n "$pattern" pkg service --glob '*.go' --glob '!**/*_test.go'; then
    echo "forbidden runtime logging: $description" >&2
    failed=1
  fi
}

check_forbidden "console or process-terminating logger" 'fmt\.(Print|Printf|Println)|log\.(Print|Printf|Println|Fatal|Fatalf|Panic|Panicf)'
check_forbidden "decorative log output" 'logx\..*[🚀✅⚠️❌🛑📊🆕🔗║╚]'
check_forbidden "sensitive value in log expression" '(logx\.|fmt\.|log\.)(.*)(token|password|passwd|secret|authorization|cookie|totp)'

exit "$failed"
```

Add `./scripts/check-logging.sh` to the `quick` test tier. During migration, record narrow temporary exceptions in the audit document with an owner and removal task; do not weaken the patterns globally.

- [ ] **Step 4: Complete P0 logging work under Task 11 ownership**

Task 11 owns authentication state, constructor signatures, client responses, CORS/proxy policy, and secret removal. Task 8 owns the logging contract and verifies these changes in the same P0 security branch:

1. Remove every print of TOTP secret, code, QR payload, and verification result from `pkg/server/safe/twosteps/google.go`.
2. Replace standard `log.Printf` in Logto middleware with structured `logx` events that never include the token or claims body.
3. Replace `log.Fatalf` in `AuthHandler` with a constructor returning an error, and propagate that error through REST server registration to the service startup boundary.
4. Return generic authentication responses to clients while logging only the error class, issuer host, route, and TraceID.

Use the constructor contract selected and compatibility-checked by the Task 11 sub-plan; the expected direction is:

```go
func NewAuthHandler(
    next http.HandlerFunc,
    issuer string,
    expectedAudience string,
) (http.Handler, error)
```

Run:

```bash
go test ./pkg/server/safe/... ./pkg/server/trans/rest -count=1
rg -n 'fmt\.(Print|Printf|Println)|log\.(Print|Printf|Println|Fatal|Fatalf)' pkg/server/safe --glob '*.go'
```

Expected: tests pass; no secret-bearing or process-terminating runtime logging remains in `pkg/server/safe`.

- [ ] **Step 5: Bind TraceID and stable fields at request and cross-service boundaries**

Use the existing `Request.traceID`, `PayLoad.TraceID`, OpenTelemetry context, and go-zero context fields. Do not create a custom logger type.

```go
ctx := logx.ContextWithFields(r.Context(),
    logx.Field("trace_id", req.GetTraceId()),
    logx.Field("service", req.ServiceName()),
    logx.Field("route", req.GetPath()),
)
logger := logx.WithContext(ctx)
logger.Errorw("request_failed",
    logx.Field("operation", "router_do"),
    logx.Field("error", err),
)
```

Add one request-completion event at the HTTP boundary only for failed or slow requests; successful request volume and latency belong in existing router metrics/stats rather than one info line per request. Propagate the same TraceID through HTTP, gRPC, EventBridge, MQ envelopes, and cross-node calls.

Tests must prove an incoming `X-Trace-Id` appears in a captured structured error event and an outbound transport keeps the same value.

- [ ] **Step 6: Normalize levels and exception ownership in four reviewable batches**

Apply these mappings:

| Batch | Packages | Required changes |
| --- | --- | --- |
| A | `router`, `run`, `trans/rest`, `safe` | Remove banners and per-auth-success noise; log startup summary once; log request terminal failure once; route registration detail becomes debug |
| B | `cluster`, `mq`, `event`, `transport` | Retry attempt becomes debug; successful fallback/switch becomes info; exhausted recovery and failed rollback become error; add provider/node/attempt fields |
| C | `persistence`, `utils` | Stop dumping raw SQL, DSNs, objects, and records; remove log-and-return duplication; lifecycle recovery is info, final corruption/data-loss risk is error |
| D | WebSocket and notification packages | Worker start/stop becomes debug; queue drop, panic, unhealthy skip, and shutdown timeout remain error with route/shard/drop-count fields |

Commit and verify each batch independently. Do not mix persistence logging changes with transport or authentication changes.

- [ ] **Step 7: Add missing high-value events and remove low-value events**

Required events:

| Boundary | Required events |
| --- | --- |
| Service startup/shutdown | `service_starting`, `service_started`, `service_start_failed`, `service_stopped` with service, mode, port, and duration |
| Request boundary | `request_failed` and `request_slow` with trace, route template, method, status class, duration, and error class |
| Cluster | `cluster_provider_ready`, `cluster_degraded`, `cluster_switch_started/completed/rolled_back`, final heartbeat/watch failure |
| Transport | `transport_retry`, `transport_fallback`, `transport_send_failed`; never log full payload or response |
| MQ/EventBridge | provider connect/switch/close, subscribe failure, publish terminal failure, consumer panic; never log every successful message |
| Persistence | connection/recovery/migration outcome and terminal sync failure; never log every CRUD success or SQL value string |
| WebSocket | queue drop, consumer panic, shard initialization failure, and shutdown timeout; worker lifecycle stays debug |

Remove logs that cannot answer an operator question: separators, decorative status art, routine success per record, full object dumps, duplicate stack traces, and messages without service/operation context.

- [ ] **Step 8: Verify actual log output and prevent regression**

Add focused tests using a temporary go-zero writer. Parse emitted JSON and assert stable event name, level, `trace_id`, service/route/provider fields, and absence of secret fixtures. Induce one final failure and one successful fallback; verify the failure is logged once and the fallback is info rather than error.

Run:

```bash
scripts/check-logging.sh
go vet ./pkg/server/... ./pkg/persistence/... ./service/...
go test ./pkg/server/... ./pkg/persistence/... ./pkg/utils ./service/manage/... -count=1
go test -race ./pkg/server/router ./pkg/server/cluster ./pkg/server/mq ./pkg/server/types -count=1
```

Expected: all commands exit 0; a manually induced failure can be found by TraceID without exposing request bodies, credentials, tokens, SQL values, or TOTP data.

## Task 9: Architecture Hardening Backlog

**Files:**
- Modify: `docs/codex/CORE_RELEASE_READINESS_PLAN.md`
- Review: `pkg/server/router/servicecontext.go`
- Review: `pkg/server/cluster/event.go`
- Review: `pkg/server/config/serverconfig.go`
- Review: `pkg/persistence/entity/modellist.go`
- Review: `pkg/persistence/adapter/default.go`

- [ ] **Step 1: Add hardening checklist**

Append to `docs/codex/CORE_RELEASE_READINESS_PLAN.md`:

```markdown
## Architecture Hardening Backlog

- [ ] Guard `pkg/server/router/servicecontext.go` global `scontext` map with a mutex or replace it with a registry type.
- [ ] Decide whether `types.SetCrossNodeForwarder` should be process-global or keyed by service name.
- [ ] In `pkg/server/cluster/event.go`, use `net.JoinHostPort` and treat non-2xx HTTP responses as forwarding errors.
- [ ] In `pkg/server/config/serverconfig.go`, log config migration write failures with config path and field context.
- [ ] Split `pkg/persistence/database/nosql/sharedbadger.go` by sync queue, batch write, self-healing, and query/cache responsibilities.
```

- [ ] **Step 2: Convert each checked item to a separate implementation branch**

For each item, create one small branch and include a focused test. Do not combine sharedbadger splitting with runtime cluster changes.

## Task 10: README and API Docs Alignment

**Files:**
- Modify: `README.md`
- Modify: `docs/codex/AUTOMATED_VERIFICATION_PLAN.md`
- Create: `docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- Review: `.codex/skills/use-digitalway-core/references/core-backend-api.md`
- Review: `examples/01-hello-router`
- Review: `examples/03-manage-crud`
- Review: `examples/12-mq-event-stream`

- [ ] **Step 1: Replace stale README snippets**

Update README examples so they match these current rules:

```text
普通 public/private 路径: /api/{service}/{structLower}
private 身份读取: req.GetUser()
manage CRUD 路径: /api/manage/{service}/{manageStructLower}/{operationLower}
ModelList 初始化: every model embedding entity.Model or entity.BaseModel must implement NewModel()
```

- [ ] **Step 2: Link examples to verification commands**

Add this README section:

````markdown
## Local Verification

Fast checks:

```bash
./scripts/test.sh quick
```

Server checks:

```bash
./scripts/test.sh server
```

Docker-backed integration checks:

```bash
docker compose -f docker-compose.integration.yml up -d
./scripts/test.sh integration-external
```
````

- [ ] **Step 3: Document the framework reuse boundary**

Add a short architecture section to `README.md` and the core skill reference stating:

```markdown
## Framework Reuse Policy

Digitalway Core composes go-zero and other mature libraries. New infrastructure code must first check the pinned go-zero capabilities. Digitalway-owned abstractions should remain thin and are justified only when they preserve public API compatibility or domain behavior such as router/model conventions, MachineID isolation, cross-node notices, and provider switching.

go-zero `core/queue` is process-local and does not replace Redis Streams, NATS JetStream, or Kafka providers. Broker integrations must use a maintained broker client behind the existing provider contract.
```

- [ ] **Step 4: Publish the logging contract for framework consumers**

Update `README.md` and `.codex/skills/use-digitalway-core/references/core-backend-api.md` to link `docs/codex/LOGGING_AUDIT_AND_STANDARD.md` and state:

```markdown
## Logging Rules

- Use go-zero structured `logx` events with stable `snake_case` names.
- Attach TraceID and service/route/provider context when available.
- Log errors once at the boundary that handles, degrades, or terminates the operation.
- Never log tokens, credentials, TOTP values, full payloads/bodies/responses, DSNs, or SQL values.
- Use debug for retries and per-item detail, info for lifecycle or successful fallback, error for terminal failure, and slow logs for measured latency threshold breaches.
```

- [ ] **Step 5: Publish the scenario-based framework usage guide**

Create `docs/codex/FRAMEWORK_USAGE_GUIDE.md` as the decision entrypoint for framework consumers. Cover public/private APIs, Manage CRUD and hooks, model selection and pagination, WebSocket notices, cross-node notices, EventBridge/MQ, cluster providers, transport selection, cache/Redis, configuration, testing, and extension boundaries.

For every capability, include:

```text
Scenario -> recommended framework API -> closest example -> required configuration -> test command -> maturity
```

Use only these maturity labels:

- `Stable`: current production constructor and tests confirm the path.
- `Conditional`: supported only with explicit configuration or external dependencies.
- `Experimental`: an API exists but startup, lifecycle, compatibility, or production evidence is incomplete.
- `Unsupported`: configuration or legacy code may mention it, but runtime use must fail clearly.

Add a short anti-pattern section covering shared request state, bypassing ModelList/service wrappers, infrastructure reimplementation, silent config acceptance, secrets in logs, and external-service assumptions in unit tests. Link this guide from README and the `use-digitalway-core` skill reference.

**Acceptance:** each documented scenario points to a real example/test and matches `CONFIG_RUNTIME_CAPABILITY_MATRIX.md`; no capability is labeled `Stable` solely because a config field or interface exists.

## Task 11: Security Baseline and Authentication Isolation

**Priority:** P0

**Files:**
- Create: `docs/codex/plans/11-security-auth-isolation.md`
- Modify: `pkg/server/config/serverconfig.go`
- Modify: `pkg/server/trans/rest/server.go`
- Modify: `pkg/server/safe/logto/authmiddleware.go`
- Modify: client IP and request-boundary helpers under `pkg/server`

- [ ] **Step 1: Record the trust-boundary threat model**

Document secrets at rest, JWT issuer/audience ownership, browser origins, trusted reverse proxies, body-size limits, public error exposure, and abuse controls. Include current evidence: permissive config modes, broad CORS fallback, package-global auth settings, and unconditional forwarded-IP trust.

- [ ] **Step 2: Add failing security regression tests**

Cover config files written as `0600`, two concurrent auth handlers with different issuer/audience values, rejected unapproved origins, spoofed forwarding headers from untrusted peers, request-size limits, generic client errors, and security headers. Tests must prove manage and user auth cannot overwrite each other's policy.

- [ ] **Step 3: Make defaults explicit and fail closed**

Move auth policy into immutable per-handler configuration; require an explicit CORS allowlist outside development; trust forwarding headers only from configured proxy CIDRs; support environment or secret-provider overrides without serializing resolved secrets; add bounded bodies, appropriate HTTP security headers, and auth/API rate limits.

- [ ] **Step 4: Verify secret and response hygiene**

Run focused tests plus a repository scan for permissive secret-file modes, raw token/claim logging, internal error text returned to clients, and wildcard production CORS.

**Acceptance:** security tests pass under `-race`; no mutable package-global auth policy remains; migrated config secrets are least-permission; public responses do not disclose internal causes; proxy and origin trust are configuration-driven.

## Task 12: Request Isolation, Global State, and Lifecycle

**Priority:** P0 for request isolation and races; P1 for broader lifecycle consolidation

**Files:**
- Create: `docs/codex/plans/12-request-lifecycle-concurrency.md`
- Modify: `service/manage/manageservice.go`
- Modify: `pkg/server/api/manage/menumanage.go`
- Modify: `pkg/server/router/servicecontext.go`
- Modify: `pkg/server/run/server.go`
- Modify: `pkg/server/types/routerinfo.go`
- Modify: provider, Fiber, WebSocket, MQ, transport, and database lifecycle owners as required

- [ ] **Step 1: Inventory mutable process and request state**

Classify each global/map/goroutine as immutable registry, synchronized registry, request-local value, or lifecycle-owned worker. Include `ManageService.Req`, service/global type maps, subscriber maps, etcd lease state, empty Fiber shutdown, WebSocket limiter cleanup, and subsystem close paths.

- [ ] **Step 2: Prove request isolation and registry safety**

Add concurrent tests showing request IDs and identities never cross service calls. Replace shared request storage with explicit parameters or request-scoped context. Return immutable snapshots from registries and protect every mutable map consistently.

- [ ] **Step 3: Establish a single lifecycle owner**

Define ordered, idempotent `Start`/`Close` behavior for cluster membership, heartbeat, CrossNodeNoticeBroker, MQ, transport, database connections, Fiber/HTTP servers, cleanup workers, and background callbacks. Use cancellation, deadlines, and wait groups; propagate terminal startup/shutdown errors.

- [ ] **Step 4: Close the provider-switch reconciliation gap**

During `Begin -> Complete`, continuously mirror or reconcile nodes registered after migration begins. Test concurrent registration, watch events, rollback, completion, duplicate delivery, and provider failure so no membership is silently lost.

- [ ] **Step 5: Run race and leak gates**

Partition race tests by package and add bounded goroutine-leak checks around repeated start/stop cycles. Document the asynchronous WebSocket callback contract and make tests synchronize through channels or wait groups rather than unsafely captured state.

**Acceptance:** no request-scoped mutable state lives on shared service objects; all registries have one synchronization policy; repeated start/close is idempotent; provider migration reconciles concurrent membership; focused race and leak tests pass.

## Task 13: Persistence Correctness and External-Test Separation

**Priority:** P0

**Files:**
- Create: `docs/codex/plans/13-persistence-correctness.md`
- Modify: `pkg/persistence/database/oltp/mysql.go`
- Modify: `pkg/persistence/database/oltp/sqlite.go`
- Modify: persistence sync/config tests and external database tests
- Modify: `docker-compose.integration.yml`

- [ ] **Step 1: Split unit and external database contracts**

Identify tests that implicitly connect to `127.0.0.1:3306` or other services. Keep unit tests on SQLite/fakes and gate MySQL, MongoDB, and ClickHouse suites behind the integration build tag plus explicit environment variables. Add dedicated Compose profiles and health checks; bind unauthenticated host ports to `127.0.0.1` only.

- [ ] **Step 2: Add failing result-propagation tests**

Verify `Raw`, `Scan`, and `Exec` return the operation result's `.Error`, not a stale database handle error. Cover query failure, scan failure, context cancellation, and transaction rollback for MySQL-compatible and SQLite paths.

- [ ] **Step 3: Correct synchronization semantics**

Fix and test default batch delay, success/failure counts, pending state, CAS/conflict handling, retry boundaries, and fatal-break behavior. A log must never report sync success when zero requested records completed.

- [ ] **Step 4: Validate both tiers**

The default persistence command must pass without Docker or hidden local services. Give every tier an explicit timeout and verify failed tests cancel retries and workers promptly. Docker-backed suites must prove driver configuration, migrations, CRUD, cancellation, and cleanup against pinned MySQL, MongoDB, and ClickHouse images.

**Acceptance:** `go test ./pkg/persistence/... -count=1 -timeout=5m` is environment-independent and green; explicit Docker persistence suites are green; stale-handle error propagation and false success reporting have regression coverage; failure-path tests terminate without residual retries or workers.

## Task 14: Configuration-to-Runtime Capability Contract

**Priority:** P1

**Files:**
- Create: `docs/codex/plans/14-config-runtime-contract.md`
- Create: `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- Review/modify: `pkg/server/config`
- Review/modify: cluster, transport, MQ, event, and ServiceContext factories

- [ ] **Step 1: Build the field-level capability matrix**

For every server, cluster, transport, MQ, event, auth, and persistence field, record accepted values, default, validation, runtime consumer, behavior test, lifecycle owner, and support status. Start with MQ `Usage`, request/reply, retry, dead-letter, and dynamic-switch fields plus cluster heartbeat, suspect, reuse-cooldown, and shard settings.

- [ ] **Step 2: Test configuration through the real startup path**

Use production constructors rather than manually populated `ServiceContext` values. Prove configuration creates and starts the expected cluster provider, selector, MQ manager, event stream/bridge, and CrossNodeNoticeBroker in the required order, and that shutdown closes them.

- [ ] **Step 3: Remove silent capability claims**

Wire supported fields to mature library behavior behind thin adapters. For unsupported combinations, return an actionable validation/startup error or remove/deprecate the field with migration documentation. Do not accept `quic`, `mq`, retry, dead-letter, or usage modes that are silently skipped.

- [ ] **Step 4: Gate future configuration additions**

Require matrix and behavior-test updates in the review template and CI whenever config structs or tags change.

**Acceptance:** every accepted field has a tested runtime effect; unsupported values fail before serving traffic; the matrix matches defaults, factories, startup, and shutdown behavior.

## Task 15: Public API Compatibility and Release Governance

**Priority:** P1

**Files:**
- Create: `docs/codex/plans/15-api-release-governance.md`
- Modify: `pkg/server/trans/rest/error.go`
- Review: `docs/codex/CORE_RELEASE_READINESS_PLAN.md`
- Review: `docs/codex/DEPENDENT_SERVICES_RISK_PLAN.md`
- Review: `docs/codex/PERSISTENCE_MANAGE_COMPAT_PLAN.md`
- Create or update: release, changelog, contribution, and compatibility documents

- [ ] **Step 1: Define the public compatibility surface**

List exported Go APIs, routes, payloads, status codes, error codes, configuration keys/defaults, database compatibility, and observable lifecycle behavior used by downstream services.

- [ ] **Step 2: Replace string-matched HTTP error mapping**

Define typed public error code, HTTP status, safe message, and wrapped internal cause. Add table tests proving localization or internal text changes cannot alter status and internal details are not exposed.

- [ ] **Step 3: Add compatibility artifacts**

Generate deterministic OpenAPI/route snapshots and an exported Go API baseline. Evaluate and pin a maintained compatibility checker before adoption; require an explicit approval file for intentional breaks.

- [ ] **Step 4: Establish release governance**

Document semantic versioning, deprecation duration, migration notes, changelog format, tag/release ownership, rollback, and exact commit/tag pinning for consumer repositories. Add smoke checks for futures, omni-flow, and ai-ops compatibility where locally available.

**Acceptance:** public errors are typed and stable; route/exported API drift is reviewed; intentional breaks have migration evidence; release tags and downstream pins are reproducible.

## Task 16: CI Quality Gates and Consumer Compatibility Matrix

**Priority:** P1

**Files:**
- Create: `docs/codex/plans/16-ci-quality-gates.md`
- Create: `.github/workflows/ci.yml` and focused workflow files if separation is clearer
- Modify: `scripts/test.sh`
- Create: pinned tool/version configuration as approved

- [ ] **Step 1: Define required tiers and time budgets**

Create gates for formatting and full `go vet`, fast unit tests, package-partitioned race tests, Docker broker/discovery integration, Docker persistence integration, configuration-runtime contracts, and downstream smoke tests. Record required/optional status and expected duration.

- [ ] **Step 2: Require owning tasks to fix blockers before enabling gates**

Task 7 owns the unkeyed Mongo `bson.E` cleanup, Task 13 owns persistence unit failures, and Task 12 owns the asynchronous callback race contract. Task 16 wires their passing commands into CI; it must not duplicate product-code fixes. Do not suppress warnings or exclude packages without a documented owner and expiry.

- [ ] **Step 3: Implement reproducible CI**

Pin Go, service images, and optional tools; cache modules/build outputs; use explicit build tags and environment variables; upload logs and test artifacts on failure; cancel superseded runs; set per-job timeouts.

- [ ] **Step 4: Add security and compatibility gates deliberately**

Evaluate `govulncheck`, static/security analysis, exported API comparison, generated-file drift, and consumer smoke tests. Pin approved tools and define triage/waiver ownership instead of introducing unowned warnings.

**Acceptance:** a clean checkout runs the same commands locally and in CI; all required checks pass; external services default to skipped locally and explicit in CI; failures identify the package, service, and artifact needed to reproduce them.

## Task 17: Performance, Capacity, and Operational SLO Baseline

**Priority:** P2 after correctness and lifecycle work

**Files:**
- Create: `docs/codex/plans/17-performance-slo-baseline.md`
- Create: focused benchmark and observability tests
- Review: large persistence, ServiceContext, router, and WebSocket modules

- [ ] **Step 1: Measure before restructuring**

Benchmark representative router dispatch, persistence operations, provider watch/switch, event/MQ flow, and WebSocket fan-out. Capture CPU, allocations, goroutines, queue depth, and shutdown latency before splitting large files or changing concurrency.

- [ ] **Step 2: Define capacity and resource budgets**

Set owned limits for goroutines, queues, database pools, retries, cache sizes, message/request bodies, and local storage mappings. Review the SQLite `mmap_size` near 30 GB and replace machine-scale defaults with bounded, configurable values backed by measurements.

- [ ] **Step 3: Add operational signals**

Expose RED metrics for HTTP/provider operations, USE-style signals for pools/queues/workers, dependency health, provider-switch state, and shutdown failures. Preserve trace continuity across HTTP, event, MQ, and cross-node boundaries; avoid high-cardinality labels and sensitive fields.

- [ ] **Step 4: Establish SLOs and regression gates**

Define availability, latency, error-rate, event-delivery, and recovery objectives with owners and alert thresholds. Add stable benchmark comparisons only after controlling variance; use profiles and contract boundaries to guide any large-file split.

**Acceptance:** baselines and budgets are recorded with reproducible commands; critical paths emit actionable metrics/traces; SLOs have owners; performance refactors demonstrate measured benefit without correctness regression.

## Cross-Task Ownership

When findings overlap, the following task owns the implementation; other tasks only verify or consume its evidence:

| Concern | Implementation owner | Consumers |
| --- | --- | --- |
| Auth state, TOTP output, CORS/proxy trust, safe auth responses | Task 11 | Tasks 8, 15, 16 |
| Request/global concurrency, workers, shutdown, provider reconciliation | Task 12 | Tasks 9, 16, 17 |
| Persistence errors, sync semantics, unit/external separation | Task 13 | Tasks 7, 8, 16, 17 |
| Config validation and actual runtime behavior | Task 14 | Tasks 5, 10, 15, 16 |
| Runtime logging vocabulary and ownership | Task 8 | Tasks 10, 16, 17 |
| Public errors, API/config compatibility, releases | Task 15 | Tasks 10, 16 |
| CI orchestration and required-check policy | Task 16 | All tasks provide commands; Task 16 does not own their product fixes |

## Development Entry Gate

Development may begin when these conditions are recorded:

1. Commit this master plan by itself so implementation diffs cannot silently rewrite scope.
2. Resolve Task 1 by either committing the current Go 1.26/go-zero v1.10.2 dependency upgrade separately or explicitly restoring the approved dependency baseline.
3. Create the focused Task 11-13 plans with failing tests, compatibility impact, rollback, and exact completion commands before editing their runtime code.
4. Land the portable Task 3 test harness early; repository scripts and CI must use standard `go`, `rg`, and `docker compose`, never the local `rtk` wrapper or a macOS-only cache path.
5. Use the first runtime slices in this order: Task 11 security/auth isolation, Task 12 request isolation and shutdown-critical races, then Task 13 persistence correctness/test separation. Keep each slice independently reviewable and revertible.

## Execution Order

1. Freeze or separately commit Task 1 dependency drift before attributing later failures to code changes.
2. Create the focused plans for Tasks 11-13, then fix P0 security defaults, request isolation/races, lifecycle-critical gaps, and persistence correctness. Task 8's secret/process-control logging fixes run in the same P0 phase.
3. Complete Task 14's configuration-runtime matrix and real-startup tests before claiming cluster, transport, MQ, or event features are supported.
4. Complete Task 6's go-zero reuse matrix, Task 7's cleanup register, and Task 8's full logging inventory. Use these artifacts to decide what is kept, delegated, removed, or deprecated.
5. Complete Tasks 2 and 3 together, extending Compose with explicit broker/discovery and persistence profiles. External dependencies remain disabled unless a script/CI job sets the documented environment variables.
6. Complete Task 4 after health checks are stable, then decide Task 5 explicitly: use a maintained Kafka client behind `MQProvider` or reject/document Kafka as unsupported.
7. Enable Task 16 CI gates incrementally: full vet and unit tests first, race partitions second, Docker integration and configuration/consumer contracts third. A gate becomes required only after its current blocker is fixed.
8. Execute Task 7 in separate cache, SQLite, and Mongo/NoSQL branches; execute Task 8 logging normalization in its four package batches. Keep Task 12 lifecycle/provider reconciliation changes isolated from cleanup.
9. Complete Task 15 typed errors, compatibility snapshots, deprecation, and release governance before the next public framework release.
10. Complete Task 9 remaining hardening and Task 10 documentation alignment, then establish Task 17 performance/SLO baselines before structural performance refactors.

## Verification Matrix

| Command | Layer | Requires Docker | Requires external env |
| --- | --- | --- | --- |
| `go vet ./...` | full-project compile/vet baseline | No | No |
| `./scripts/test.sh quick` | formatting/vet + environment-independent fast unit tests | No | No |
| `./scripts/test.sh server` | server package unit/integration-style tests | No, but local port binding must be allowed | No |
| `./scripts/test.sh persistence-unit` | persistence correctness with SQLite/fakes | No | No |
| `./scripts/test.sh race` | package-partitioned request, registry, lifecycle, and callback race tests | No | No |
| `./scripts/test.sh config-contract` | configuration validation through real startup/shutdown | No for local providers | No |
| `./scripts/test.sh integration-local` | local provider integration tests | No | `CORE_TEST_CLUSTER_LOCAL=1` set by script |
| `./scripts/test.sh integration-external` | etcd/consul/redis/nats tests | Yes | set by script defaults |
| `./scripts/test.sh integration-persistence` | MySQL/MongoDB/ClickHouse driver and lifecycle tests | Yes | explicit `CORE_TEST_*` variables set by script |
| `./scripts/check-logging.sh` | runtime logging policy and sensitive-output guard | No | No |
| `./scripts/test.sh security` | auth isolation, CORS/proxy, file mode, body, header, and safe-error tests | No | No |
| `./scripts/test.sh compatibility` | route/OpenAPI/exported API and configured consumer smoke checks | Consumer-dependent | consumer paths or revisions explicitly configured |
| `CORE_TEST_KAFKA=1 ... TestMQKafka` | Kafka provider contract | Yes | Only after Kafka provider exists |

## Done Definition

This plan is complete when:

- `git status --short` has no unreviewed dependency drift.
- Full `go vet ./...` passes without package exclusions or unowned suppressions.
- `./scripts/test.sh quick` passes.
- `./scripts/test.sh server` passes.
- Persistence unit tests pass without hidden MySQL, MongoDB, ClickHouse, or other local services.
- Focused race and lifecycle leak tests pass, including concurrent request/auth isolation, registries, WebSocket callbacks, and repeated start/close.
- `docker compose -f docker-compose.integration.yml up -d` starts healthy dependencies.
- `./scripts/test.sh integration-external` passes for etcd, Consul, Redis Streams, and NATS JetStream.
- Explicit Docker persistence suites pass for MySQL, MongoDB, and ClickHouse and clean up their resources.
- Kafka is either implemented with a passing provider contract test or documented as config-only.
- Config files containing secrets use least-permission modes; CORS, forwarded-IP trust, body limits, auth policy, and public errors pass the security contract.
- Auth issuer/audience policy is immutable per handler and safe for concurrent manage/user services.
- Request-scoped data is absent from shared service objects; mutable registries return snapshots and use consistent synchronization.
- Every started provider, broker, transport, database, server, callback, and cleanup worker has an idempotent bounded shutdown path.
- Provider switching reconciles nodes registered during `Begin -> Complete` and preserves membership across rollback/failure.
- Every accepted configuration field has a runtime consumer and behavioral test; unsupported fields or values fail validation/startup or follow a documented deprecation.
- `docs/codex/GO_ZERO_REUSE_AUDIT.md` identifies every reviewed subsystem as keep, replace, remove, or keep-domain, with source and test evidence.
- General Redis/cache, discovery, concurrency, retry, and lifecycle helpers are delegated to mature go-zero capabilities where contracts match; exceptions have a documented domain reason.
- Enabled runtime paths contain no `panic("implement me")`, nil-returning placeholder adapters, or secret-bearing debug output.
- Duplicate SQLite instance ownership is consolidated and covered by a race test.
- `./scripts/check-logging.sh` passes and production-library code contains no unapproved `fmt.Print*`, standard `log.*`, `Fatal*`, decorative, or sensitive-value logging.
- Request and cross-service terminal failures emit one structured event with TraceID and stable context fields; successful fallbacks are info events and retry attempts are debug events.
- Full payloads, responses, bodies, credentials, TOTP values, DSNs, and SQL values are absent from captured logs.
- Public errors use typed stable codes/statuses/safe messages; route/OpenAPI/exported API drift and dependent-service smoke checks are reviewed before release.
- Required CI gates reproduce local commands from a clean checkout and retain actionable failure artifacts.
- Release, changelog, deprecation, migration, tag, rollback, and downstream pinning policies are documented and exercised by a release candidate.
- Performance baselines, resource budgets, RED/USE metrics, trace continuity, and owned SLOs exist before performance-driven structural changes are accepted.
- README and `docs/codex/AUTOMATED_VERIFICATION_PLAN.md` show the same commands.
- README and the `use-digitalway-core` reference state the same go-zero reuse and logging boundaries.
- `docs/codex/FRAMEWORK_USAGE_GUIDE.md` provides scenario decisions and maturity labels backed by real constructors, examples, configuration, and tests.
