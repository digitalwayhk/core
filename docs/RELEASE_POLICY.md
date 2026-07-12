# Core 发布策略

## 版本规则

- 使用 SemVer 标签 `vMAJOR.MINOR.PATCH`。当前 `v0.x` 阶段仍保护已登记公共表面，不能以未到 1.0 为由静默破坏。
- PATCH：兼容修复；MINOR：加性 API/能力；MAJOR：经批准且附迁移证据的破坏性变更。
- 安全修复允许有意收紧行为，但必须在 changelog 的 Security/Changed 中说明迁移影响。

## 发布前置条件

1. 工作区干净，HEAD 为计划发布提交。
2. `CHANGELOG.md` 的 Unreleased 包含本次变更、迁移和安全影响。
3. `api-compat`、`public-api`、`config-contract`、`security` 全部通过。
4. 公共 API 破坏必须有批准记录、迁移说明和消费方 smoke 证据。
5. 废弃项满足登记窗口，不能提前删除。
6. 发布人显式创建签名或 annotated tag；脚本不得自动 tag、push 或发布。

## 发布流程

```bash
./scripts/test.sh release-contract
CORE_RELEASE_VERSION=v0.0.248 ./scripts/release-check.sh --release
git tag -a v0.0.248 -m "core v0.0.248"
git push origin v0.0.248
```

最后两条命令必须由发布负责人人工执行。发布脚本只读检查。

## 回滚

- 代码回滚到上一稳定 tag，并重新运行 `release-contract`。
- 下游将 `go.mod` 锁回上一 tag/精确 commit，执行各自 smoke 后再部署。
- 不移动或重写已发布 tag；错误版本发布新 PATCH 修复并在 changelog 记录。

## 责任边界

- Core owner：兼容基线、changelog、tag 候选和回滚说明。
- 子系统 owner：行为测试、迁移说明和运维影响。
- 消费方 owner：精确版本锁定和本仓库之外的 smoke 结果。
