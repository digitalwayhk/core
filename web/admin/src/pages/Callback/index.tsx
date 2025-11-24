import React, { useEffect, useState } from 'react';
import { history, useModel } from '@umijs/max';
import { Spin, Result, Button, Card, Descriptions, Alert } from 'antd';
import { LoadingOutlined, CheckCircleOutlined, CloseCircleOutlined } from '@ant-design/icons';
import { fetchCasdoorConfig, handleSigninCallback } from '@/config/casdoor';

/**
 * 解析 JWT Token 获取用户信息
 */
const parseJwtToken = (token: string) => {
  try {
    const parts = token.split('.');
    if (parts.length !== 3) {
      throw new Error('Invalid JWT token format');
    }

    // 解码 payload (第二部分)
    const payload = parts[1];
    const decoded = atob(payload.replace(/-/g, '+').replace(/_/g, '/'));
    const claims = JSON.parse(decoded);

    console.log('===== JWT Token 解析结果 =====');
    console.log('Claims:', claims);

    return claims;
  } catch (error) {
    console.error('JWT Token 解析失败:', error);
    throw new Error('Token 解析失败');
  }
};

/**
 * 将 JWT Claims 转换为用户信息格式
 */
const convertClaimsToUserInfo = (claims: any): API.CurrentUser => {
  return {
    userid: claims.id || claims.sub,
    name: claims.name || claims.displayName,
    email: claims.email,
    avatar: claims.avatar,
    signature: claims.bio,
    title: claims.title,
    group: claims.owner,
    tags: [],
    notifyCount: 0,
    unreadCount: 0,
    country: claims.country || claims.region,
    access: 'user', // 如果 isAdmin 为 true，可以设置为 'admin'
    geographic: {
      province: { label: '', key: '' },
      city: { label: '', key: '' },
    },
    address: claims.location || '',
    phone: claims.phone || '',
  };
};

const Callback: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [debugInfo, setDebugInfo] = useState<any>({});
  const { setInitialState } = useModel('@@initialState');

  useEffect(() => {
    const processCallback = async () => {
      const logs: any[] = [];

      try {
        // 1. 从 URL 获取 code 和验证 state
        logs.push({ step: '1. 获取授权码', status: 'processing' });
        setDebugInfo({ logs: [...logs] });

        const { code, state } = handleSigninCallback();

        logs.push({
          step: '1. 获取授权码',
          status: 'success',
          data: { code: code.substring(0, 20) + '...', state }
        });
        setDebugInfo({ logs: [...logs] });

        console.log('收到授权码:', code);
        console.log('State:', state);

        await new Promise(resolve => setTimeout(resolve, 1000));

        // 2. 构建后端回调 URL
        logs.push({ step: '2. 构建后端URL', status: 'processing' });
        setDebugInfo({ logs: [...logs] });
        const config=await fetchCasdoorConfig()
        const backendBaseUrl = window.location.origin; // 自动获取当前运行的地址
        const typeUrl=config.ismanage?'&type=manage':''
        const callbackUrl = `${config.BackgroundCallbackURL}?code=${code}&state=${state}${typeUrl}`;
        const backendCallbackUrl = `${backendBaseUrl}${callbackUrl}`;

        console.log('后端地址:', backendBaseUrl);
        console.log('回调URL:', backendCallbackUrl);

        logs.push({
          step: '2. 构建后端URL',
          status: 'success',
          data: { backendBaseUrl, callbackUrl: backendCallbackUrl }
        });
        setDebugInfo({ logs: [...logs] });

        await new Promise(resolve => setTimeout(resolve, 1000));

        // 3. 调用后端换取 token
        logs.push({ step: '3. 调用后端换取token', status: 'processing' });
        setDebugInfo({ logs: [...logs] });

        console.log('===== 开始请求后端 =====');
        console.log('URL:', backendCallbackUrl);

        const response = await fetch(backendCallbackUrl, {
          method: 'GET',
          credentials: 'include',
          headers: { 'Accept': 'application/json' },
        });

        console.log('===== 收到响应 =====');
        console.log('Status:', response.status);

        const responseText = await response.text();
        console.log('Response Text:', responseText);

        if (!response.ok) {
          throw new Error(`后端返回错误: ${response.status}`);
        }

        const data = JSON.parse(responseText);
        console.log('Parsed Data:', data);

        logs.push({
          step: '3. 调用后端换取token',
          status: 'success',
          data: {
            httpStatus: response.status,
            success: data.success,
            hasToken: !!data.data,
          }
        });
        setDebugInfo({ logs: [...logs], response: data, rawResponse: responseText });

        await new Promise(resolve => setTimeout(resolve, 1000));

        // 4. 提取 Token
        logs.push({ step: '4. 提取Token', status: 'processing' });
        setDebugInfo({ logs: [...logs], response: data, rawResponse: responseText });

        if (!data.success || !data.data) {
          throw new Error('后端未返回有效的 token');
        }

        const token = data.data;
        console.log('===== Token 提取成功 =====');
        console.log('Token Length:', token.length);
        console.log('Token Preview:', token.substring(0, 50) + '...');

        // 保存 token 到 localStorage
        localStorage.setItem('casdoor_token', token);

        logs.push({
          step: '4. 提取Token',
          status: 'success',
          data: {
            tokenLength: token.length,
            tokenPreview: token.substring(0, 50) + '...',
          }
        });
        setDebugInfo({
          logs: [...logs],
          response: data,
          rawResponse: responseText,
          token: token.substring(0, 50) + '...',
        });

        await new Promise(resolve => setTimeout(resolve, 1000));

        // 5. 解析 JWT Token 获取用户信息
        logs.push({ step: '5. 解析JWT Token', status: 'processing' });
        setDebugInfo({
          logs: [...logs],
          response: data,
          rawResponse: responseText,
          token: token.substring(0, 50) + '...',
        });

        const claims = parseJwtToken(token);
        const userInfo = convertClaimsToUserInfo(claims);

        console.log('===== 用户信息解析完成 =====');
        console.log('Claims:', claims);
        console.log('User Info:', userInfo);

        // 保存用户信息
        localStorage.setItem('casdoor_user', JSON.stringify(userInfo));

        logs.push({
          step: '5. 解析JWT Token',
          status: 'success',
          data: {
            userid: userInfo.userid,
            name: userInfo.name,
            email: userInfo.email,
            claimsKeys: Object.keys(claims),
          }
        });
        setDebugInfo({
          logs: [...logs],
          response: data,
          rawResponse: responseText,
          token: token.substring(0, 50) + '...',
          claims: claims,
          userInfo: userInfo,
        });

        await new Promise(resolve => setTimeout(resolve, 1000));

        // 6. 更新全局状态
        logs.push({ step: '6. 更新全局状态', status: 'processing' });
        setDebugInfo({
          logs: [...logs],
          response: data,
          rawResponse: responseText,
          token: token.substring(0, 50) + '...',
          claims: claims,
          userInfo: userInfo,
        });

        await setInitialState((s) => ({
          ...s,
          currentUser: userInfo,
        }));

        logs.push({
          step: '6. 更新全局状态',
          status: 'success',
        });
        setDebugInfo({
          logs: [...logs],
          response: data,
          rawResponse: responseText,
          token: token.substring(0, 50) + '...',
          claims: claims,
          userInfo: userInfo,
        });

        await new Promise(resolve => setTimeout(resolve, 1000));

        // 7. 跳转回原页面
        const redirectUrl = sessionStorage.getItem('redirect_url') || '/';
        sessionStorage.removeItem('redirect_url');

        logs.push({
          step: '7. 准备跳转',
          status: 'success',
          data: { redirectUrl }
        });
        setDebugInfo({
          logs: [...logs],
          response: data,
          rawResponse: responseText,
          token: token.substring(0, 50) + '...',
          claims: claims,
          userInfo: userInfo,
          redirectUrl,
        });

        console.log('✅ 登录成功，2秒后跳转到:', redirectUrl);

        setTimeout(() => {
          history.push(redirectUrl);
        }, 2000);

      } catch (err: any) {
        console.error('===== 整体处理失败 =====');
        console.error('Error:', err);

        logs.push({
          step: '❌ 错误',
          status: 'error',
          data: {
            errorName: err.name,
            errorMessage: err.message,
            errorStack: err.stack,
          }
        });
        setDebugInfo({ logs: [...logs], error: err });
        setError(err.message || '登录处理失败，请重试');
        setLoading(false);
      }
    };

    processCallback();
  }, [setInitialState]);

  // ...existing code... (保持 UI 部分不变)

  if (loading && !error) {
    return (
      <div
        style={{
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          minHeight: '100vh',
          padding: '24px',
          backgroundColor: '#f0f2f5',
        }}
      >
        <Card
          title="登录回调处理中"
          style={{ width: '100%', maxWidth: 900 }}
          extra={<Spin indicator={<LoadingOutlined style={{ fontSize: 24 }} spin />} />}
        >
          <Alert
            message="调试模式 - JWT Token 解析"
            description="直接从 Token 中解析用户信息，无需额外 API 调用"
            type="info"
            showIcon
            style={{ marginBottom: 24 }}
          />

          <Descriptions title="URL 信息" bordered column={1} size="small">
            <Descriptions.Item label="完整URL">
              {window.location.href}
            </Descriptions.Item>
            <Descriptions.Item label="授权码(code)">
              {new URLSearchParams(window.location.search).get('code')?.substring(0, 30)}...
            </Descriptions.Item>
            <Descriptions.Item label="State">
              {new URLSearchParams(window.location.search).get('state')}
            </Descriptions.Item>
          </Descriptions>

          <div style={{ marginTop: 24 }}>
            <h3>处理步骤：</h3>
            {debugInfo.logs?.map((log: any, index: number) => (
              <div
                key={index}
                style={{
                  padding: '12px',
                  marginBottom: '8px',
                  backgroundColor: log.status === 'error' ? '#fff2f0' : '#fff',
                  border: `1px solid ${log.status === 'error' ? '#ffccc7' : '#d9d9d9'}`,
                  borderRadius: '4px',
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                  {log.status === 'success' && (
                    <CheckCircleOutlined style={{ color: '#52c41a', fontSize: 20 }} />
                  )}
                  {log.status === 'error' && (
                    <CloseCircleOutlined style={{ color: '#ff4d4f', fontSize: 20 }} />
                  )}
                  {log.status === 'processing' && (
                    <LoadingOutlined style={{ color: '#1890ff', fontSize: 20 }} />
                  )}
                  <strong>{log.step}</strong>
                </div>
                {log.data && (
                  <pre style={{
                    marginTop: 8,
                    padding: 8,
                    backgroundColor: '#f5f5f5',
                    borderRadius: 4,
                    fontSize: 11,
                    overflow: 'auto',
                    maxHeight: 400,
                  }}>
                    {JSON.stringify(log.data, null, 2)}
                  </pre>
                )}
              </div>
            ))}
          </div>

          {debugInfo.token && (
            <Alert
              message="✅ Token 已获取"
              description={`Token: ${debugInfo.token}`}
              type="success"
              showIcon
              style={{ marginTop: 24 }}
            />
          )}

          {debugInfo.claims && (
            <div style={{ marginTop: 24 }}>
              <h3>🔐 JWT Claims（Token 解析结果）：</h3>
              <pre style={{
                padding: 12,
                backgroundColor: '#e6f7ff',
                borderRadius: 4,
                fontSize: 11,
                overflow: 'auto',
                maxHeight: 300,
                border: '1px solid #91d5ff',
              }}>
                {JSON.stringify(debugInfo.claims, null, 2)}
              </pre>
            </div>
          )}

          {debugInfo.userInfo && (
            <div style={{ marginTop: 24 }}>
              <h3>👤 用户信息（转换后）：</h3>
              <Descriptions bordered column={2} size="small">
                <Descriptions.Item label="用户ID">
                  {debugInfo.userInfo.userid}
                </Descriptions.Item>
                <Descriptions.Item label="用户名">
                  {debugInfo.userInfo.name}
                </Descriptions.Item>
                <Descriptions.Item label="邮箱">
                  {debugInfo.userInfo.email}
                </Descriptions.Item>
                <Descriptions.Item label="头像">
                  {debugInfo.userInfo.avatar ? '已设置' : '未设置'}
                </Descriptions.Item>
              </Descriptions>
            </div>
          )}

          {debugInfo.redirectUrl && (
            <Alert
              message="🚀 即将跳转"
              description={`2秒后将跳转到: ${debugInfo.redirectUrl}`}
              type="info"
              showIcon
              style={{ marginTop: 24 }}
            />
          )}
        </Card>
      </div>
    );
  }

  if (error) {
    return (
      <div
        style={{
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'center',
          minHeight: '100vh',
          padding: '24px',
          backgroundColor: '#f0f2f5',
        }}
      >
        <Card style={{ width: '100%', maxWidth: 900 }}>
          <Result
            status="error"
            title="登录失败"
            subTitle={error}
            extra={[
              <Button
                type="primary"
                key="retry"
                onClick={() => {
                  sessionStorage.removeItem('redirect_url');
                  history.push('/user/login');
                }}
              >
                返回登录
              </Button>,
            ]}
          />

          {debugInfo.logs && (
            <div style={{ marginTop: 24, textAlign: 'left' }}>
              <h3>🔍 详细错误信息：</h3>
              <pre style={{
                padding: 12,
                backgroundColor: '#fff2f0',
                borderRadius: 4,
                fontSize: 11,
                overflow: 'auto',
                border: '1px solid #ffccc7',
                maxHeight: 500,
              }}>
                {JSON.stringify(debugInfo, null, 2)}
              </pre>
            </div>
          )}
        </Card>
      </div>
    );
  }

  return null;
};

export default Callback;
