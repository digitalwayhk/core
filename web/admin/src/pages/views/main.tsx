import { useEffect } from 'react';
import { useParams, useLocation, useModel } from '@umijs/max';
import WayPage from '@/components/WayPlus/WayPage/index';
import { init, search, execute } from '@/components/WayPlus/request';
import { ErrorBoundary } from '@ant-design/pro-components';


export default () => {
  const params = useParams<{ s: string; c: string }>();
  const location = useLocation();
  const model = useModel('useRouteParams'); // 使用全局 model

  useEffect(() => {
    // 同步路由参数到全局 model
    model.setRouteParams?.({ s: params.s, c: params.c });
    return () => {
      console.log('🧹 Main 组件卸载:', { pathname: location.pathname, params: params });
    };
  }, [params]);
  // 检查参数是否存在
  if (!params.s || !params.c) {
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
  return (
    <ErrorBoundary key={`manage-${params.s}-${params.c}`}>
      <WayPage
        key={`manage-${params.s}-${params.c}`} // 强制基于路由参数重挂载
        controller={c}
        service={s}
        namespace={'manage'}
        init={() => {
          const payload = {
            c: 'manage/' + params.s + '/' + params.c,
            s: s,
          };
          return init(payload);
        }}
        search={(item: any) => {
          const url = 'manage/' + params.s + '/' + params.c;
          return search({ c: url, s: s, item: item });
        }}
        execute={(method: string, item: any) => {
          const url = 'manage/' + params.s + '/' + params.c;
          return execute({ c: url, m: method, s: s, item: item });
        }}
      />
    </ErrorBoundary>
  );
};
