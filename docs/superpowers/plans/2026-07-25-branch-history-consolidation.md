# Core 分支历史收敛实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 审计 Web 运行时历史、精准补入仍缺失的能力，并将本地仓库收敛为以 `main` 为唯一权威开发线的可恢复状态。

**Architecture:** 审计以当前 `main` 为事实源，通过祖先关系、patch-id、`range-diff`、当前代码和测试五类证据建立提交去向清单。旧分支禁止整体 merge；只有被证明仍有价值且缺失的行为才从最新 `main` 单独实现。清理前为每个旧 tip 创建 annotated archive tag，确保删除分支后仍可恢复。

**Tech Stack:** Git worktree、Git range-diff/cherry、Go 1.26、现有 Go/契约测试脚本、中文 Markdown 审计文档。

---

### Task 1: 固化审计基线

**Files:**
- Create: `docs/codex/BRANCH_CONSOLIDATION_AUDIT.md`
- Reference: `docs/superpowers/specs/2026-07-25-branch-history-consolidation-design.md`

- [ ] **Step 1: 确认相关 worktree 都没有未提交内容**

Run:

```bash
rtk git -C /Users/vincent/orca/workspaces/core/core-api-web status --short --untracked-files=all
rtk git -C /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core-codex status --short --untracked-files=all
rtk git -C /Users/vincent/orca/workspaces/core/review-pr-4 status --short --untracked-files=all
```

Expected: 三条命令均无文件输出；若任一 worktree 非干净，停止清理并记录文件。

- [ ] **Step 2: 记录不可变提交基线**

Run:

```bash
rtk git show-ref --heads
rtk git merge-base main feat/web-runtime-auth
rtk git merge-base optimize/code-cleanup feat/web-runtime-auth
rtk git rev-list --left-right --count main...feat/web-runtime-auth
rtk git range-diff b76610a..586f512 72cc6cb..548aa53
```

Expected:

- `main` 与 `feat/web-runtime-auth` 的共同基点为 `41da968`。
- `optimize/code-cleanup` 的 tip `5eda9ad` 是 `feat/web-runtime-auth` 的祖先。
- 最后三组提交映射为 `dbc4d3b/c79bee0`、`7492e70/fcc3b31`、`586f512/548aa53`。

- [ ] **Step 3: 创建提交去向清单**

创建 `docs/codex/BRANCH_CONSOLIDATION_AUDIT.md`，固定以下表头：

```markdown
# Core 分支收敛审计

## 基线

| 引用 | 提交 | 说明 |
| --- | --- | --- |

## 提交组去向

| 提交范围 | 功能组 | 分类 | main 证据 | 验证命令 |
| --- | --- | --- | --- | --- |

分类只允许：已合入、已被替代、明确废弃、需要补入。

## 清理门禁

- [ ] 所有提交组均已分类
- [ ] 需要补入项为零或已进入 main
- [ ] 完整测试通过
- [ ] release-contract 通过
- [ ] archive tag 已验证
```

- [ ] **Step 4: 提交审计基线**

```bash
rtk git add docs/codex/BRANCH_CONSOLIDATION_AUDIT.md
rtk git commit -m "docs: establish branch consolidation audit"
```

Expected: 只提交审计文档。

### Task 2: 按功能组归类 Web 分支

**Files:**
- Modify: `docs/codex/BRANCH_CONSOLIDATION_AUDIT.md`

- [ ] **Step 1: 生成共同基点之后的有序提交清单**

Run:

```bash
rtk git log --reverse --format="%h%x09%s" 41da968..feat/web-runtime-auth
```

Expected: 输出 69 个提交，最后三个为 `dbc4d3b`、`7492e70`、`586f512`。

- [ ] **Step 2: 审计 Web runtime auth 与 HTMLServer 安全链**

Run:

```bash
rtk git diff --stat 41da968..feat/web-runtime-auth -- pkg/server/run pkg/server/trans/rest pkg/server/types pkg/server/safe
rtk git log --reverse --format="%h %s" 41da968..feat/web-runtime-auth -- pkg/server/run pkg/server/trans/rest pkg/server/types pkg/server/safe
rtk rg -n "Logto|AttachServices|ServerManageAuth|webbootstrap|ManageAuthAuthority" pkg internal docs/codex
```

Classification rules:

- 依赖 Logto、AttachServices、Observe/Notify 的提交归为“明确废弃”。
- 当前 `main` 已由 Access Token、Casdoor、`ServerManageAuth` 或现行 HTMLServer 测试覆盖的提交归为“已被替代”。
- 当前 `main` 缺少且不依赖已删除能力的安全行为归为“需要补入”。

- [ ] **Step 3: 审计 Manage 菜单、查询和字段持久化**

Run:

```bash
rtk git log --reverse --format="%h %s" 41da968..feat/web-runtime-auth -- pkg/server/api/manage service/manage
rtk git diff main..feat/web-runtime-auth -- pkg/server/api/manage service/manage
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/api/manage ./service/manage -count=1
```

Expected: 通过当前测试；每组旧提交在清单中记录当前实现文件或缺失点。

- [ ] **Step 4: 审计 OpenAPI、Swagger 和同源路由**

Run:

```bash
rtk git log --reverse --format="%h %s" 41da968..feat/web-runtime-auth -- pkg/server/api/public/openapi.go pkg/server/run/openapi.go pkg/server/run/htmlserver.go
rtk git diff main..feat/web-runtime-auth -- pkg/server/api/public/openapi.go pkg/server/run/openapi.go pkg/server/run/htmlserver.go
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./pkg/server/api/public ./pkg/server/run -count=1
```

Expected: 匿名 `/api/openapi`、受 `ServerManageAuth` 保护的 `/api/internal/openapi` 和现行同源规则不得被旧实现覆盖。

- [ ] **Step 5: 审计 Web Admin 源码、子模块和嵌入产物**

Run:

```bash
rtk git log --reverse --format="%h %s" 41da968..feat/web-runtime-auth -- web/admin pkg/server/run/dist scripts/build-web-admin.sh
rtk git diff --submodule=log main..feat/web-runtime-auth -- web/admin
rtk git diff --stat main..feat/web-runtime-auth -- pkg/server/run/dist scripts/build-web-admin.sh
```

Expected: 生成产物只有在可追溯到子模块源码提交和构建脚本时才可归为“需要补入”；孤立旧产物归为“已被替代”或“明确废弃”。

- [ ] **Step 6: 审计示例、启动门禁和 UAT**

Run:

```bash
rtk git log --reverse --format="%h %s" 41da968..feat/web-runtime-auth -- examples pkg/server/integration
rtk git diff --stat main..feat/web-runtime-auth -- examples pkg/server/integration
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/... ./pkg/server/integration -run "^$" -count=1
```

Expected: 旧测试若要求 Logto、AttachServices、旧 QUIC 或已删除启动语义，归为“明确废弃”；当前业务行为缺少覆盖时归为“需要补入”。

- [ ] **Step 7: 证明清单覆盖全部 69 个提交**

Run:

```bash
rtk git log --reverse --format="%h" 41da968..feat/web-runtime-auth
rtk rg -o "[0-9a-f]{7}" docs/codex/BRANCH_CONSOLIDATION_AUDIT.md
```

Expected: 每个提交至少被一个连续提交范围覆盖，清单不存在未解释范围。

- [ ] **Step 8: 提交分类结果**

```bash
rtk git add docs/codex/BRANCH_CONSOLIDATION_AUDIT.md
rtk git commit -m "docs: classify legacy web branch history"
```

### Task 3: 处理“需要补入”项

**Files:**
- Modify: `docs/codex/BRANCH_CONSOLIDATION_AUDIT.md`

- [ ] **Step 1: 检查是否存在“需要补入”**

Run:

```bash
rtk rg -n "需要补入" docs/codex/BRANCH_CONSOLIDATION_AUDIT.md
```

Expected:

- 若只有分类说明、没有数据行：直接进入 Task 4。
- 若存在数据行：将本计划标记为被补漏工作阻塞，停止归档和删除流程。

- [ ] **Step 2: 对补漏项执行独立设计门禁**

若存在“需要补入”数据行：

1. 为每个功能组单独形成设计和实施计划。
2. 以当前 `main` 的失败测试证明行为确实缺失。
3. 补漏计划必须明确文件、测试、race 和兼容性门禁。
4. 补漏完成后把清单分类更新为“已合入”，再恢复本计划的 Task 4。

Expected: 本计划不包含未知实现占位符，也不会在缺少证据时删除旧历史。

### Task 4: 运行删除前门禁

**Files:**
- Modify: `docs/codex/BRANCH_CONSOLIDATION_AUDIT.md`

- [ ] **Step 1: 检查格式与工作树**

```bash
rtk git ls-files "*.go" | rtk xargs gofmt -l
rtk git diff --check
rtk git status --short
```

Expected: `gofmt -l` 无输出、无 whitespace error、工作树干净。

- [ ] **Step 2: 运行完整全仓测试**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./... -count=1
```

Expected: exit 0。

- [ ] **Step 3: 运行配置、日志和发布契约**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh config-contract
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/check-logging.sh
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
```

Expected: 三个命令均 exit 0，且 release-contract 明确说明未创建 tag、未 push、未发布。

- [ ] **Step 4: 勾选审计清单门禁并提交**

```bash
rtk git add docs/codex/BRANCH_CONSOLIDATION_AUDIT.md
rtk git commit -m "docs: close branch consolidation audit"
```

### Task 5: 创建可恢复归档

**Files:**
- Modify: Git refs only

- [ ] **Step 1: 确认 archive tag 尚不存在**

```bash
rtk git tag --list "archive/*-20260725"
```

Expected: 无同名 tag；若已存在，先核对其目标，不覆盖。

- [ ] **Step 2: 为三个旧 tip 创建 annotated tag**

```bash
rtk git tag -a archive/feat-web-runtime-auth-20260725 586f512 -m "归档 feat/web-runtime-auth 清理前 tip"
rtk git tag -a archive/optimize-code-cleanup-20260725 5eda9ad -m "归档 optimize/code-cleanup 清理前 tip"
rtk git tag -a archive/codex-optimize-code-cleanup-20260725 f97807f -m "归档 codex/optimize-code-cleanup 清理前 tip"
```

- [ ] **Step 3: 验证归档可恢复**

```bash
rtk git rev-parse archive/feat-web-runtime-auth-20260725^{}
rtk git rev-parse archive/optimize-code-cleanup-20260725^{}
rtk git rev-parse archive/codex-optimize-code-cleanup-20260725^{}
```

Expected: 分别输出 `586f512...`、`5eda9ad...`、`f97807f...`。

### Task 6: 删除旧 worktree 与分支

**Files:**
- Modify: Git worktree registry and local branch refs only

- [ ] **Step 1: 从主仓库目录移除 `core-api-web`**

```bash
rtk git worktree remove /Users/vincent/orca/workspaces/core/core-api-web
rtk git branch -D feat/web-runtime-auth
```

Expected: worktree 和分支均不存在，archive tag 仍可解析。

- [ ] **Step 2: 移除 `core-codex`**

```bash
rtk git worktree remove /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core-codex
rtk git branch -D codex/optimize-code-cleanup
```

Expected: worktree 和分支均不存在，archive tag 仍可解析。

- [ ] **Step 3: 释放 `optimize/code-cleanup`**

在 `/Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core` 中执行：

```bash
rtk git checkout --detach main
rtk git branch -D optimize/code-cleanup
```

Expected: 主仓库 checkout 暂时 detached 在当前 `main` 提交；`main` 仍由 `review-pr-4` worktree 持有。

- [ ] **Step 4: 清理并验证 worktree 注册表**

```bash
rtk git worktree prune
rtk git worktree list --porcelain
rtk git branch --list
rtk git status --short
```

Expected:

- 不再存在 `core-api-web` 和 `core-codex`。
- 不再存在三个旧分支。
- `main` 指向完成审计的提交且工作树干净。
- `perf/single-node-hotpath` 及其 worktree 保持不变。

### Task 7: 最终交付检查

**Files:**
- Reference: `docs/codex/BRANCH_CONSOLIDATION_AUDIT.md`

- [ ] **Step 1: 证明旧 tip 仍可恢复**

```bash
rtk git show -s --oneline archive/feat-web-runtime-auth-20260725
rtk git show -s --oneline archive/optimize-code-cleanup-20260725
rtk git show -s --oneline archive/codex-optimize-code-cleanup-20260725
```

- [ ] **Step 2: 证明 `main` 是唯一权威开发线**

```bash
rtk git branch --list
rtk git log -5 --oneline main
rtk git status --short
```

Expected: 除明确排除的性能分支外，不存在三个旧开发分支；`main` 包含审计关闭提交。
