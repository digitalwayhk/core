import { request } from 'umi';

export interface requestPorps {
  c: string;
  m?: string;
  item?: any;
}
const reqKeys = new Map();

/** 获取初始化数据 */
export async function init(params: { c: string; s: string }) {
  const url = `/api/${params.c}/view`;
  console.log('\ud83c\udf10 [request.ts] init 被调用:', { params, url });
  try {
    console.log('\ud83d\udce4 [request.ts] 发送 HTTP 请求:', url);
    const result = await request(url, {
      method: 'POST',
    });
    console.log('\u2705 [request.ts] 请求成功:', result);
    return result;
  } catch (e) {
    console.error('\u274c [request.ts] 请求失败:', e);
    return {
      success: false,
      message: e,
    };
  }
}
export async function search(params: { c: string; s: string; item: any }) {
  try {
    if (!reqKeys.has(params.c)) {
      console.log(params.c);
      reqKeys.set(params.c, params.item);
      return await request(`/api/${params.c}/search`, {
        method: 'POST',
        data: params.item,
      });
    } else {
      console.log(params.c + '查询中，不能重复请求！');
      return {
        success: false,
        message: '查询中，不能重复请求！',
      };
    }
  } catch (e) {
    return {
      success: false,
      message: e,
    };
  } finally {
    reqKeys.delete(params.c);
  }
}
export async function execute(params: { c: string; m: string; s: string; item: any }) {
  const key = params.c + '-' + params.m;
  try {
    if (!reqKeys.has(key)) {
      reqKeys.set(key, params.item);
      return await request(`/api/${params.c}/${params.m}`, {
        method: 'POST',
        data: params.item,
      });
    } else {
      console.log(key + '执行中，不能重复请求！');
      return {
        success: false,
        message: '执行中，不能重复请求！',
      };
    }
  } catch (e) {
    return {
      success: false,
      message: e,
    };
  } finally {
    reqKeys.delete(key);
  }
}

export async function getMenu(service: string) {
  try {
    return await request(`/api/servermanage/getmenu`, {
      method: 'GET',
    });
  } catch (e) {
    return {
      success: false,
      message: e,
    };
  }
}
export async function getRouters(path: string) {
  try {
    return await request(path, {
      method: 'POST',
    });
  } catch (e) {
    return {
      success: false,
      message: e,
    };
  }
}
export async function getService() {
  try {
    return await request(`/api/servermanage/queryservice`, {
      method: 'POST',
    });
  } catch (e) {
    return {
      success: false,
      message: e,
    };
  }
}
