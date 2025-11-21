# WayPlus Redux Model 配置说明

## 问题原因

WayPage 组件依赖 Redux 进行状态管理和数据请求，但项目中**缺少 Redux Model 的注册**，导致：

1. ❌ `props.init` 为 `undefined`
2. ❌ 没有发起网络请求
3. ❌ 页面显示"model 未创建或init方法未实现"错误

## 解决方案

已创建 `/src/models/manage.ts` 文件，注册 `manage` namespace 的 Redux Model。

## 数据流程

```
┌─────────────┐
│  main.tsx   │  传入 namespace='manage', service, controller
└──────┬──────┘
       │
       ▼
┌─────────────────┐
│   WayPage.tsx   │  使用 connect() 连接 Redux
└──────┬──────────┘
       │ mapDispatchToProps
       │ 创建 init/search/execute 方法
       ▼
┌─────────────────────────┐
│  Redux Dispatch         │
│  type: 'manage/init'    │  ← 这里需要 manage model
│  payload: { c, s }      │
└──────┬──────────────────┘
       │
       ▼
┌─────────────────────┐
│  models/manage.ts   │  ✅ 新创建的文件
│  namespace: 'manage'│
│  使用 WayModel()     │
└──────┬──────────────┘
       │
       ▼
┌──────────────────────┐
│  waymodel.ts         │
│  effects: { init }   │  Generator 函数
└──────┬───────────────┘
       │ yield call()
       ▼
┌──────────────────────┐
│  request.ts          │
│  init(params)        │  发起真实的 HTTP 请求
└──────┬───────────────┘
       │
       ▼
┌──────────────────────────────┐
│  HTTP Request                │
│  POST /api/{c}/view          │  ← 这是你期望看到的网络请求
│  例如: /api/manage/user/     │
│         usercontroller/view  │
└──────────────────────────────┘
```

## 关键配置点

### 1. Model 命名空间

```typescript
// main.tsx 中传入
<WayPage
  controller={c}
  service={s}
  namespace={'manage'}  // ← 这个值对应 model 的 namespace
/>
```

### 2. Redux Action Type

```typescript
// mapDispatchToProps 中生成
const typens = ownProps.namespace; // 'manage'
const actionType = typens + '/init'; // 'manage/init'
```

### 3. Model 注册

```typescript
// src/models/manage.ts
export default {
  namespace: 'manage',  // ← 必须与传入的 namespace 一致
  ...WayModel({...})
}
```

### 4. API 路径构造

```typescript
// 在 init() 中构造
const c = ownProps.namespace + '/' + ownProps.service + '/' + ownProps.controller;
// 例如: 'manage/user/usercontroller'

// 在 request.ts 中使用
await request(\`/api/\${params.c}/view\`, { method: 'POST' });
// 实际请求: POST /api/manage/user/usercontroller/view
```

## 验证步骤

1. **刷新页面**，查看控制台是否有以下日志：
   ```
   🔗 mapDispatchToProps 配置: { namespace: 'manage', service: '...', controller: '...' }
   📤 Dispatching init action: { type: 'manage/init', payload: {...} }
   🔧 manage model initing: {...}
   ```

2. **检查网络面板**，应该能看到：
   ```
   POST /api/manage/{service}/{controller}/view
   ```

3. **如果仍然失败**，检查：
   - UmiJS 是否自动识别了 models 文件夹
   - 查看 `.umi/core/plugin-model/models.ts` 中是否包含 manage
   - 尝试重启开发服务器：`yarn start`

## 添加其他 Model

如果需要其他 namespace（如 'system', 'config' 等），只需在 `src/models/` 下创建对应文件：

```typescript
// src/models/system.ts
import { WayModel } from '@/components/WayPlus/waymodel';

export default {
  namespace: 'system',
  ...WayModel({
    // 可选的钩子函数
  }),
};
```

## 调试技巧

### 查看 Redux 状态
使用 Redux DevTools 插件，可以看到：
- Action: `manage/init`
- State 变化

### 查看网络请求
打开浏览器 Network 面板，筛选 XHR 请求，应该能看到：
- URL: `/api/manage/.../view`
- Method: POST
- Status: 200 (如果后端正常)

### 控制台日志
现在添加了详细的日志输出，关键日志包括：
- 🔗 Redux 配置
- 📤 Action 派发
- 🔧 Model 钩子执行
- ✅ 请求成功/失败
