// Casdoor 配置
let casdoorConfig: any = null;

export async function fetchCasdoorConfig() {
  if (casdoorConfig) return casdoorConfig;
  const resp = await fetch('/api/casdoor?type=manage');
  if (!resp.ok) throw new Error('获取 Casdoor 配置失败');
  const result = await resp.json();
  if (!result.success || !result.data) throw new Error('Casdoor 配置无效');
  casdoorConfig = result.data;
  casdoorConfig.ismanage = true;
  return casdoorConfig;
}
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
export const getCasdoorSignInUrl = async () => {
  const config = await fetchCasdoorConfig();
  const redirectUri = `${window.location.origin}/callback`;
  const state = generateState();
  // 保存 state 用于后续验证
  sessionStorage.setItem('casdoor_state', state);

  const params = new URLSearchParams({
    client_id: config.ClientID,
    response_type: 'code',
    redirect_uri: redirectUri,
    scope: 'read',
    state: state,
    organization: config.Organization,
    application: config.Application,
  });

  return `${config.Endpoint}/login/oauth/authorize?${params.toString()}`;
};

/**
 * 跳转到 Casdoor 登录页面
 */
export async function signinRedirect() {
  const signInUrl = await getCasdoorSignInUrl();
  window.location.href = signInUrl;
}

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
export async function getCasdoorSignOutUrl() {
  const config = await fetchCasdoorConfig();
  const redirectUri = `${window.location.origin}/user/login`;
  return `${config.Endpoint}/logout?redirect_uri=${encodeURIComponent(redirectUri)}`;
};

/**
 * 登出
 */
export async function signout() {
  localStorage.removeItem('casdoor_token');
  localStorage.removeItem('casdoor_user');
  sessionStorage.clear();
  const signOutUrl = await getCasdoorSignOutUrl();
  window.location.href = signOutUrl;
}
