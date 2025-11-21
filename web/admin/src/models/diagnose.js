/**
 * WayPlus Redux Model 诊断脚本
 *
 * 在浏览器控制台中运行此脚本来诊断 Redux Model 是否正确加载
 */

console.log('🔍 开始诊断 WayPlus Redux Model...\n');

// 1. 检查 Redux store 是否存在
if (typeof window !== 'undefined' && window.g_app) {
  console.log('✅ Redux store 已找到');

  // 2. 获取当前的 store state
  const store = window.g_app._store;
  if (store) {
    const state = store.getState();
    console.log('📊 当前 Redux State:', state);

    // 3. 检查 manage model 是否注册
    if (state.manage) {
      console.log('✅ manage model 已注册');
      console.log('📦 manage state:', state.manage);
    } else {
      console.error('❌ manage model 未找到！');
      console.log('📋 已注册的 models:', Object.keys(state));
      console.log('\n💡 可能的原因:');
      console.log('   1. 开发服务器需要重启 (Ctrl+C 然后 yarn start)');
      console.log('   2. src/models/manage.ts 文件格式不正确');
      console.log('   3. UmiJS 配置问题');
    }

    // 4. 测试 dispatch
    console.log('\n🧪 测试 dispatch manage/init...');
    try {
      store.dispatch({
        type: 'manage/init',
        payload: {
          c: 'manage/test/testcontroller',
          s: 'test',
        }
      });
      console.log('✅ dispatch 成功，查看上面的日志看是否有请求发出');
    } catch (e) {
      console.error('❌ dispatch 失败:', e);
    }
  } else {
    console.error('❌ Redux store 未初始化');
  }
} else {
  console.error('❌ window.g_app 不存在');
  console.log('💡 这通常意味着:');
  console.log('   1. 页面还未完全加载');
  console.log('   2. UmiJS 应用未正确初始化');
  console.log('   3. 不在 UmiJS 应用环境中');
}

console.log('\n' + '='.repeat(60));
console.log('📌 完整的调试流程:');
console.log('1. 确保开发服务器已重启');
console.log('2. 打开浏览器 DevTools (F12)');
console.log('3. 访问页面: /main/{service}/{controller}');
console.log('4. 查看控制台日志，应该看到:');
console.log('   - 📦 [manage.ts] Model 正在加载...');
console.log('   - 🔗 mapDispatchToProps 配置...');
console.log('   - 📤 Dispatching init action...');
console.log('   - 🔧 [manage model] initing 钩子被调用...');
console.log('   - 🌐 [request.ts] init 被调用...');
console.log('   - 📤 [request.ts] 发送 HTTP 请求...');
console.log('5. 打开 Network 面板，应该看到 /api/.../view 请求');
console.log('='.repeat(60));
