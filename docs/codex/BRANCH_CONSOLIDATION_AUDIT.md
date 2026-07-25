# Core 分支收敛审计

## 基线

审计日期：2026-07-25

| 引用 | 提交 | 说明 |
| --- | --- | --- |
| `main` | `60e4216` | 当前唯一目标开发线，已包含分支收敛设计与实施计划 |
| `feat/web-runtime-auth` | `586f512` | `core-api-web` worktree 的待审计 tip |
| `optimize/code-cleanup` | `5eda9ad` | `feat/web-runtime-auth` 的历史分支基点 |
| `codex/optimize-code-cleanup` | `f97807f` | 已通过 `a13be18` 合入 `main` 的整理分支 |
| `main` / Web 共同基点 | `41da968` | `kin-openapi` 升级完成点 |

三个相关 worktree 在审计开始时均为干净状态。`main...feat/web-runtime-auth`
为 `40/69`；69 个旧分支提交不等同于 69 个缺失功能，必须按当前契约归类。

已确认末尾迁移映射：

| Web 提交 | main 提交 | 差异 |
| --- | --- | --- |
| `dbc4d3b` | `c79bee0` | 同一功能，摘取时调整 `web/admin` 子模块基点 |
| `7492e70` | `fcc3b31` | 同一嵌入产物发布目标，应用在不同基线 |
| `586f512` | `548aa53` | patch-id 完全等价 |

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
