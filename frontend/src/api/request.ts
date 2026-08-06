import axios, { AxiosError, type AxiosRequestConfig } from 'axios';
import JSONBig from 'json-bigint';
import type { ApiResponse } from '@/types/common';
import { useAuthStore } from '@/store/auth';
import { toast } from '@/utils/toast';
import { mockAdapter } from '@/mock';

// 后端 snowflake ID 为 int64（约 1.8e18），超出 JS 安全整数 2^53（约 9e15）。
// 默认 JSON.parse 会截断低位，导致拿到的 id 与后端不一致、查不到用户。
// 用 json-bigint 解析响应：storeAsString=true 时，仅对超出安全范围的
// 大整数（即 ID）保留为完整字符串，其余安全范围内的数字（计数/时间戳/枚举）
// 仍为 number，且字符串 id 可直接用于 === 比较与拼 URL，避免 BigNumber 对象
// 比较恒为 false 的问题。
const JSONbig = JSONBig({ useNativeBigInt: false, storeAsString: true });

// Base URL 固定 /api/v1，开发环境由 vite server.proxy 转发到网关，不依赖 CORS
const instance = axios.create({
  baseURL: '/api/v1',
  timeout: 15000,
  transformResponse: [
    (data: unknown) => {
      if (typeof data !== 'string' || data === '') return data;
      try {
        return JSONbig.parse(data);
      } catch {
        return data;
      }
    },
  ],
});

// 前端自测：开启 mock 后由 axios adapter 直接返回契约数据，无需后端
if (import.meta.env.VITE_USE_MOCK === 'true') {
  instance.defaults.adapter = mockAdapter;
}

// 请求拦截：注入 Authorization: Bearer <token>（登录/注册等公开接口带上也无害）
instance.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  // 注入可读的 request_id：网关中间件会原样采用并在响应体中回写该 id，
  // 便于在前端操作后在浏览器 Console 拿到它，再去后端查询本次请求的 Trace。
  if (!config.headers['X-Request-ID']) {
    config.headers['X-Request-ID'] = `fe-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
  }
  return config;
});

function handleUnauthorized(): void {
  useAuthStore.getState().logout();
  if (!location.pathname.startsWith('/login')) {
    // 记录来源页，登录后跳回
    const redirect = encodeURIComponent(location.pathname + location.search);
    location.href = `/login?redirect=${redirect}`;
  }
}

// 响应拦截：解析统一响应体 { code, message, data, request_id }
instance.interceptors.response.use(
  (response) => {
    const body = response.data as ApiResponse;
    if (body && typeof body.code === 'number') {
      // 把后端回写的 request_id 打到 Console，前端操作后即可据此查询 Trace
      if (body.request_id) console.log('[request_id]', body.request_id);
      if (body.code === 0) {
        return body.data as never; // 成功：直接返回 data
      }
      // 业务错误：message 直接提示
      toast(body.message || '请求失败');
      return Promise.reject(new ApiError(body.code, body.message, body.request_id));
    }
    // 非统一响应体（不应出现），原样返回
    return response.data;
  },
  (error: AxiosError) => {
    // HTTP 401：响应体为空（JWT 失效），单独拦截 -> 清 token、跳登录
    if (error.response?.status === 401) {
      toast('登录已失效，请重新登录');
      handleUnauthorized();
      return Promise.reject(new ApiError(-401, '未登录或登录已过期'));
    }
    const body = error.response?.data as ApiResponse | undefined;
    const msg = body?.message || error.message || '网络异常，请稍后重试';
    toast(msg);
    return Promise.reject(new ApiError(body?.code ?? -1, msg, body?.request_id));
  },
);

export class ApiError extends Error {
  code: number;
  requestId?: string;
  constructor(code: number, message: string, requestId?: string) {
    super(message);
    this.code = code;
    this.requestId = requestId;
  }
}

// 泛型请求方法：返回值即统一响应体中的 data
export function request<T>(config: AxiosRequestConfig): Promise<T> {
  return instance.request<unknown, T>(config);
}

export const http = {
  get: <T>(url: string, params?: object) => request<T>({ method: 'GET', url, params }),
  post: <T>(url: string, data?: object) => request<T>({ method: 'POST', url, data }),
  patch: <T>(url: string, data?: object) => request<T>({ method: 'PATCH', url, data }),
  delete: <T>(url: string, data?: object) => request<T>({ method: 'DELETE', url, data }),
};
