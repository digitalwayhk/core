# Admin 选择行自定义命令表单修复记录

## 现象与复现

在 Core Admin 的 Manage 列表中，自定义命令同时设置 `IsSelectRow=true`、`EditShow=true` 时：

1. 选择一行后按钮会正确启用；
2. 编辑弹窗会显示选择行中的可编辑字段；
3. 点击确定后，请求体却是 `{}`，后端无法取得所选行的稳定身份字段。

Bitzoom Ego Lite 复现页：`/main/positions/jobconfigmanage`。选择
`marketCode=BTCUSDT, jobType=FUNDING_SETTLE` 后，浏览器抓取到：

```text
POST /api/manage/positions/jobconfigmanage/updatejobconfig
{}
```

后端正确返回 400，没有修改配置。

## 根因

`WayPage` 原本只有 `add/edit` 两种表单路由，没有把普通自定义命令的 `EditShow`
当作表单处理。本地 Docker 曾用文本替换临时开启 `EditShow`，但仍固定调用
`fromshow(command, null)`，直接丢弃了已选择的行。

`WayPage.fromshow()` 会先执行 `form.clear()`，再调用 `form.setValues(row)`。
`WayForm` 原来的 `setValues` 只更新 React state，没有同步调用 Ant Form 的
`setFieldsValue`。同时 React state 更新是异步的；紧接着提交时，
`getFormValue()` 仍可能读取 clear 后的旧空状态。而 `marketCode/jobType` 不是可编辑字段，
不能指望挂载的 Ant Form 自行重建完整行，因此序列化结果仍可能为 `{}`。

## 修复

在 Core Admin 正式实现 `EditShow` 路由：

- `add` 仍从空模型打开；
- `EditShow + IsSelectRow` 把当前完整选中行传入 `fromshow`；
- 两个工具栏入口复用同一 `commandFormRow` 决策，避免行为分叉。

新增 `applyProgrammaticValues`，将规范化后的同一个完整模型同时写入：

- WayForm 的 React `values` 状态；
- Ant Form 的字段状态。

同时用 `programmaticValuesRef` 同步保留选中行的完整快照。提交时以该快照为底稿，
再覆盖 React 状态、Ant Form 字段和当次提交字段；这样不可编辑的行身份不会因
弹窗挂载时序或 React 异步状态而丢失。`clear()` 会同时清空该快照，不会泄漏上一行。

没有改变 Manage API URL、请求/响应 JSON 契约、Gateway 或交易前端。修复仅作用于后台
Admin 程序化装载表单值，手机端和 futures web 不引用该组件。

## 回归与还原

- 单元测试锁定：程序化装载后 React state 与 Ant Form 都收到包含
  `marketCode/jobType` 的完整选择行模型。
- 单元测试锁定：`EditShow + IsSelectRow` 传入选中行，`add` 传入空模型。
- 单元测试锁定：表单只提交可编辑字段时，仍保留完整行的
  `marketCode/jobType`，并使用用户编辑后的新值覆盖原值。
- Ego Lite 必须重新验证：编辑弹窗提交体包含完整选择行，而不是 `{}`；后端仍只从
  `command.Model` 读取目标身份。
- 回滚时还原 `WayForm/index.tsx` 的 `form.setValues`，并删除
  `setProgrammaticValues.ts` 及其测试即可；API 无需回滚。
