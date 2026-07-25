# Core 分支历史收敛设计

## 背景

当前仓库同时存在名称相近但历史不同的 `optimize/code-cleanup` 与
`codex/optimize-code-cleanup`。`feat/web-runtime-auth` 从前者演进，但只有末尾三项改动
被重新摘取到后者，随后 `codex/optimize-code-cleanup` 才合入 `main`。因此，提交祖先关系、
补丁等价关系和当前功能状态并不一致，不能仅凭 `git log main..branch` 判断功能是否遗漏，
也不能把旧分支整体合并回 `main`。

本设计将 `main` 收敛为唯一权威开发线；旧分支只作为待审计历史，不再继续开发。

## 目标

1. 对 `feat/web-runtime-auth` 从共同基点 `41da968` 之后的提交建立可追溯去向。
2. 将仍有价值且未进入 `main` 的能力重新整理并补入 `main`。
3. 明确记录已合入、已被替代、明确废弃和需要补入的内容，避免重复恢复旧能力。
4. 在完整门禁通过后归档旧分支 tip，删除旧 worktree 和混淆分支。
5. 不重写 `main` 历史，不 force-push，不把审计过程中的临时提交直接暴露为长期开发线。

## 范围

### 纳入整理

- `feat/web-runtime-auth`
- `optimize/code-cleanup`
- `codex/optimize-code-cleanup`
- 对应的 `core-api-web`、`core-codex` 和旧 optimize checkout
- 已重新摘取到 `main` 的三组提交映射：
  - `dbc4d3b` → `c79bee0`
  - `7492e70` → `fcc3b31`
  - `586f512` → `548aa53`

### 不纳入整理

- `perf/single-node-hotpath` 及其 worktree
- 远端分支删除或远端 tag 发布
- `main` 的 rebase、filter-repo 或其他历史改写
- 与本次分支收敛无关的代码重构

## 核心原则

### 提交历史不等于功能状态

每个旧提交必须同时检查：

- 提交是否为 `main` 的祖先；
- 是否存在 patch-id 或 `range-diff` 对应提交；
- 当前 `main` 是否通过其他实现提供了同一能力；
- 该能力是否已被后续架构决策明确删除。

只有四项证据共同明确后，才能决定提交去向。

### 禁止整体合并旧分支

不得直接把 `feat/web-runtime-auth` merge 到 `main`。旧分支仍包含 Logto、
`AttachServices`、旧 OpenAPI、旧服务启动链和历史嵌入产物等已经删除或被替代的实现，
整体合并会复活旧契约并制造大量伪冲突。

### 补漏必须基于当前 `main`

真正遗漏的能力必须在从当前 `main` 创建的短期审计分支上处理。可以参考或摘取旧提交，
但冲突解决必须服从当前认证、OpenAPI、配置、传输和服务生命周期契约。

## 审计分类

每个提交或紧密提交组只能归入以下一类：

| 分类 | 判定条件 | 处理方式 |
| --- | --- | --- |
| 已合入 | 是 `main` 祖先，或存在等价补丁 | 记录目标提交，不再处理 |
| 已被替代 | 当前 `main` 已用不同实现满足同一目标 | 记录替代文件、测试和提交 |
| 明确废弃 | 与已批准删除的能力或现行契约冲突 | 记录废弃依据，禁止恢复 |
| 需要补入 | 当前 `main` 缺少仍然需要的行为 | 在短期分支重新整理并测试 |

提交按功能组审计，避免对大量生成产物逐文件机械判断：

1. Web runtime auth 与 HTMLServer 安全链。
2. Manage 菜单、查询和字段持久化。
3. OpenAPI、Swagger 与同源路由。
4. Web Admin 子模块与嵌入产物。
5. 示例 04—07、启动门禁和 UAT。
6. 构建脚本、文档和公共 API 基线。
7. 已删除的 Logto、AttachServices、Observe/Notify 和旧传输能力。

## 执行流程

### 阶段一：冻结与取证

- 确认所有相关 worktree 干净。
- 记录各分支 tip、merge-base、ahead/behind、`git cherry` 和 `range-diff`。
- 在任何删除前为三个旧分支 tip 创建本地 annotated archive tag。
- 生成一份提交去向清单，清单是后续删除的必要证据。

### 阶段二：只读归类

- 从 `41da968` 开始按功能组审查 `feat/web-runtime-auth`。
- 对生成产物提交同时核对对应源码或子模块提交，不把二进制/压缩产物差异单独视为功能。
- 对已被删除的能力引用现行设计、兼容性测试和删除契约作为废弃依据。
- 发现无法证明去向的提交时，默认归入“需要补入或进一步验证”，不得猜测为已完成。

### 阶段三：精准补漏

- 从最新 `main` 创建短期分支 `audit/web-runtime-consolidation`。
- 每个功能组独立形成最小提交，避免恢复整段旧历史。
- 优先复用旧测试；若旧测试绑定已删除架构，则改写为当前契约下的行为测试。
- 每组补漏通过定向测试后再进入下一组。

### 阶段四：合并验证

- 将短期审计分支合入本地 `main`。
- 运行格式、定向测试、race、公共 API、配置契约、日志和完整全仓测试。
- 提交去向清单中不得存在未解释项。
- 验证 archive tag 能解析到删除前的旧分支 tip。

### 阶段五：清理

按以下顺序处理，避免分支仍被 worktree 占用：

1. 删除 `core-api-web` worktree，再删除 `feat/web-runtime-auth`。
2. 删除 `core-codex` worktree，再删除 `codex/optimize-code-cleanup`。
3. `main` 当前若仍被 `review-pr-4` worktree 占用，则先将主仓库 checkout 安全地
   detach 到已验证的 `main` 提交，再删除 `optimize/code-cleanup`；不得为抢占分支而
   强制删除宿主 worktree。
4. 宿主释放 `review-pr-4` 后，再把主仓库 checkout 切换到 `main`；在此之前，
   `review-pr-4` 继续作为本地 `main` 的权威 checkout。
5. 执行 `git worktree prune`，再次确认只保留有明确用途的 worktree。

若 worktree 属于宿主运行环境且无法安全移除，则停止在删除前，保留分支和 archive tag，
并报告具体所有者与阻塞条件，不使用文件系统命令绕过 Git。

## 验证门禁

补漏阶段至少运行：

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./... -count=1
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh config-contract
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract
GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/check-logging.sh
```

涉及认证、REST、OpenAPI、Manage、MQ 或并发生命周期时，补充对应包的 `-race` 测试。
涉及 Web Admin 源码或嵌入产物时，必须验证子模块源码提交、构建脚本和嵌入产物三者一致。

## 删除条件

只有同时满足以下条件才允许删除旧分支和 worktree：

1. worktree 无未提交和未跟踪文件。
2. 每个旧提交或提交组都有明确分类和证据。
3. 所有“需要补入”项已经进入 `main`。
4. 完整测试和发布契约通过。
5. archive tag 已创建并验证。
6. `main` 工作树干净，且包含最终审计合并提交。

任何一项不满足都保持旧引用，不执行强制删除。

## 交付结果

完成后仓库应满足：

- `main` 是唯一权威开发分支；宿主 worktree 尚未释放时允许主仓库暂时 detached。
- 三个混淆旧分支均已删除，但其 tip 可通过本地 archive tag 恢复。
- `core-api-web` 和 `core-codex` 不再占用 worktree。
- `perf/single-node-hotpath` 保持不变。
- 中文提交去向清单能够解释旧分支每个功能组的最终归属。
- 没有恢复 Logto、AttachServices、旧 OpenAPI、旧 Socket/QUIC 或其他已删除契约。
