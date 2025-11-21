import React, { useEffect, useState } from 'react';
import { Spin, Result, Button } from 'antd';
import { LoadingOutlined } from '@ant-design/icons';
import { signinRedirect } from '@/config/casdoor';

const Login: React.FC = () => {
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    try {
      // 页面加载时直接跳转到 Casdoor 登录
      signinRedirect();
    } catch (err: any) {
      console.error('跳转到 Casdoor 失败:', err);
      setError(err.message || '跳转到登录页面失败');
    }
  }, []);

  if (error) {
    return (
      <div
        style={{
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          height: '100vh',
        }}
      >
        <Result
          status="error"
          title="登录失败"
          subTitle={error}
          extra={[
            <Button
              type="primary"
              key="retry"
              onClick={() => {
                window.location.reload();
              }}
            >
              重试
            </Button>,
          ]}
        />
      </div>
    );
  }

  // 显示加载中状态
  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'center',
        alignItems: 'center',
        height: '100vh',
        gap: '16px',
        backgroundColor: '#f0f2f5',
      }}
    >
      <Spin indicator={<LoadingOutlined style={{ fontSize: 48 }} spin />} />
      <div style={{ fontSize: '16px', color: '#666' }}>
        正在跳转到统一登录平台...
      </div>
      <div style={{ fontSize: '14px', color: '#999' }}>
        如果长时间未跳转，请刷新页面
      </div>
    </div>
  );
};

export default Login;
