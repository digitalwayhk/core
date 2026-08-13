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

`WayPage.fromshow()` 会先执行 `form.clear()`，再调用 `form.setValues(row)`。
`WayForm` 原来的 `setValues` 只更新 React state，没有同步调用 Ant Form 的
`setFieldsValue`。紧接着提交时，`getFormValue()` 读取到的是 clear 后的空表单与空状态，
因此序列化结果为 `{}`。

## 修复

新增 `applyProgrammaticValues`，将规范化后的同一个完整模型同时写入：

- WayForm 的 React `values` 状态；
- Ant Form 的字段状态。

没有改变 Manage API URL、请求/响应 JSON 契约、Gateway 或交易前端。修复仅作用于后台
Admin 程序化装载表单值，手机端和 futures web 不引用该组件。

## 回归与还原

- 单元测试锁定：程序化装载后 React state 与 Ant Form 都收到包含
  `marketCode/jobType` 的完整选择行模型。
- Ego Lite 必须重新验证：编辑弹窗提交体包含完整选择行，而不是 `{}`；后端仍只从
  `command.Model` 读取目标身份。
- 回滚时还原 `WayForm/index.tsx` 的 `form.setValues`，并删除
  `setProgrammaticValues.ts` 及其测试即可；API 无需回滚。
