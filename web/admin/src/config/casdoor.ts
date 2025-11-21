// Casdoor 配置
export const casdoorConfig = {
  serverUrl: 'http://localhost:8000',
  clientId: 'd55769f0148775246e05',
  organizationName: 'futures',
  appName: 'application_p93igp',
  redirectPath: '/callback',
};

/**
 * 生成随机 state
 */
const generateState = () => {
  return Math.random().toString(36).substring(2, 15) +
         Math.random().toString(36).substring(2, 15);
};

/**
 * 获取 Casdoor 登录 URL
 */
export const getCasdoorSignInUrl = () => {
  const redirectUri = `${window.location.origin}${casdoorConfig.redirectPath}`;
  const state = generateState();

  // 保存 state 用于后续验证
  sessionStorage.setItem('casdoor_state', state);

  const params = new URLSearchParams({
    client_id: casdoorConfig.clientId,
    response_type: 'code',
    redirect_uri: redirectUri,
    scope: 'read',
    state: state,
  });

  return `${casdoorConfig.serverUrl}/login/oauth/authorize?${params.toString()}`;
};

/**
 * 跳转到 Casdoor 登录页面
 */
export const signinRedirect = () => {
  const signInUrl = getCasdoorSignInUrl();
  window.location.href = signInUrl;
};

/**
 * 处理 Casdoor 回调
 * @returns 返回 code 和 state
 */
export const handleSigninCallback = () => {
  const urlParams = new URLSearchParams(window.location.search);
  const code = urlParams.get('code');
  const state = urlParams.get('state');
  const savedState = sessionStorage.getItem('casdoor_state');

  // 验证
  if (!code) {
    throw new Error('未获取到授权码');
  }

  if (!state || state !== savedState) {
    throw new Error('State 验证失败，可能存在安全风险');
  }

  // 清除保存的 state
  sessionStorage.removeItem('casdoor_state');

  return { code, state };
};

/**
 * 获取 Casdoor 登出 URL
 */
export const getCasdoorSignOutUrl = () => {
  const redirectUri = `${window.location.origin}/user/login`;
  return `${casdoorConfig.serverUrl}/logout?redirect_uri=${encodeURIComponent(redirectUri)}`;
};

/**
 * 登出
 */
export const signout = () => {
  // 清除本地数据
  localStorage.removeItem('casdoor_token');
  localStorage.removeItem('casdoor_user');
  sessionStorage.clear();

  // 跳转到 Casdoor 登出页面
  const signOutUrl = getCasdoorSignOutUrl();
  window.location.href = signOutUrl;
};
