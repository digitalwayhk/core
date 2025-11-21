import type { RequestOptions } from '@@/plugin-request/request';
import type { RequestConfig } from '@umijs/max';
import { message, notification } from 'antd';

enum ErrorShowType {
  SILENT = 0,
  WARN_MESSAGE = 1,
  ERROR_MESSAGE = 2,
  NOTIFICATION = 3,
  REDIRECT = 9,
}

interface ResponseStructure {
  success: boolean;
  data: any;
  errorCode?: number;
  errorMessage?: string;
  showType?: ErrorShowType;
}

export const errorConfig: RequestConfig = {
  // 请求拦截器
  requestInterceptors: [
    (config: RequestOptions) => {
      // 从 localStorage 获取 Casdoor token
      const token = localStorage.getItem('casdoor_token');

      if (token) {
        config.headers = {
          ...config.headers,
          Authorization: `Bearer ${token}`,
        };
      }

      return config;
    },
  ],

  // 响应拦截器
  responseInterceptors: [
    (response) => {
      const { data } = response as unknown as ResponseStructure;
      if (data?.success === false) {
        message.error(data.errorMessage);
      }
      return response;
    },
  ],

  errorConfig: {
    errorHandler: (error: any, opts: any) => {
      if (opts?.skipErrorHandler) throw error;

      // 401 未授权 - 清除 token 并跳转登录
      if (error.response?.status === 401) {
        localStorage.removeItem('casdoor_token');
        localStorage.removeItem('casdoor_user');
        window.location.href = '/user/login';
        return;
      }

      // 其他错误处理
      if (error.name === 'BizError') {
        const errorInfo: ResponseStructure = error.info;
        if (errorInfo) {
          const { errorMessage, errorCode } = errorInfo;
          switch (errorInfo.showType) {
            case ErrorShowType.SILENT:
              break;
            case ErrorShowType.WARN_MESSAGE:
              message.warning(errorMessage);
              break;
            case ErrorShowType.ERROR_MESSAGE:
              message.error(errorMessage);
              break;
            case ErrorShowType.NOTIFICATION:
              notification.open({
                description: errorMessage,
                message: errorCode,
              });
              break;
            default:
              message.error(errorMessage);
          }
        }
      } else if (error.response) {
        message.error(`Response status: ${error.response.status}`);
      } else if (error.request) {
        message.error('网络错误，请重试');
      } else {
        message.error('请求错误，请重试');
      }
    },
  },
};
