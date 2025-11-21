import { useEffect } from 'react';
import { useParams, useLocation } from '@umijs/max';
import WayPage from '@/components/WayPlus/WayPage/index';

export default () => {
  const params = useParams<{ s: string; c: string }>();
  const location = useLocation();

  console.log('===== Main.tsx 组件渲染 =====');
  console.log('🔍 URL:', window.location.href);
  console.log('🔍 pathname:', location.pathname);
  console.log('🔍 useParams 结果:', params);
  console.log('🔍 s (service):', params.s);
  console.log('🔍 c (controller):', params.c);
  console.log('================================');

  useEffect(() => {
    console.log('===== Main.tsx useEffect 触发 =====');
    console.log('组件已挂载');
    console.log('当前参数:', params);

    return () => {
      console.log('===== Main.tsx 组件卸载 =====');
    };
  }, [params]);

  // 检查参数是否存在
  if (!params.s || !params.c) {
    console.error('❌ 错误：缺少路由参数');
    console.error('URL:', window.location.href);
    console.error('params:', params);

    return (
      <div style={{ padding: 24, backgroundColor: '#fff2f0', border: '1px solid #ffccc7', borderRadius: 4 }}>
        <h2 style={{ color: '#ff4d4f' }}>⚠️ 路由参数错误</h2>
        <p><strong>当前 URL:</strong> {window.location.href}</p>
        <p><strong>期望格式:</strong> /main/:s/:c</p>
        <h3>解析结果：</h3>
        <pre style={{ backgroundColor: '#f5f5f5', padding: 12, borderRadius: 4 }}>
          {JSON.stringify({
            pathname: location.pathname,
            params: params,
            s: params.s || '❌ 未定义',
            c: params.c || '❌ 未定义',
          }, null, 2)}
        </pre>
      </div>
    );
  }

  const { s, c } = params;

  console.log('✅ 准备渲染 WayPage 组件');
  console.log('传入参数:', {
    controller: c,
    service: s,
    namespace: 'manage'
  });

  try {
    return (
      <div style={{ height: '100%' }}>
        <div style={{
          padding: '8px 16px',
          backgroundColor: '#e6f7ff',
          borderBottom: '1px solid #91d5ff',
          fontSize: 12,
          color: '#0050b3'
        }}>
          🔍 调试信息: Service={s}, Controller={c}, Namespace=manage
        </div>
        <WayPage
          controller={c}
          service={s}
          namespace={'manage'}
        />
      </div>
    );
  } catch (error: any) {
    console.error('===== Main.tsx 渲染错误 =====');
    console.error('错误信息:', error.message);
    console.error('错误堆栈:', error.stack);
    console.error('================================');

    return (
      <div style={{ padding: 24, color: 'red' }}>
        <h2>❌ 组件渲染错误</h2>
        <p><strong>错误信息:</strong> {error.message}</p>
        <pre style={{ backgroundColor: '#fff2f0', padding: 12, borderRadius: 4 }}>
          {error.stack}
        </pre>
      </div>
    );
  }
};
