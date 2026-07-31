import { http } from './request';
import type {
  LoginReq,
  LoginResp,
  RegisterReq,
  RegisterResp,
  SignUrlReq,
  SignUrlResp,
  UpdateUserReq,
  UpdateUserResp,
  UploadTokenReq,
  UploadTokenResp,
  UserDetail,
} from '@/types/user';

// 公开接口
export const register = (data: RegisterReq) => http.post<RegisterResp>('/users/register', data);
export const login = (data: LoginReq) => http.post<LoginResp>('/users/login', data);

// 需登录
export const getUser = (userId: string) => http.get<UserDetail>(`/users/${userId}`);
export const getMe = () => http.get<UserDetail>('/users/me');
export const updateMe = (data: UpdateUserReq) => http.patch<UpdateUserResp>('/users/me', data);

// 上传凭证（注意：路由为 POST /upload/token，以 gateway.api 为准）
export const getUploadToken = (data: UploadTokenReq) =>
  http.post<UploadTokenResp>('/upload/token', data);

// 私有对象临时访问地址（路由为 POST /upload/sign-url）
export const getSignUrl = (data: SignUrlReq) =>
  http.post<SignUrlResp>('/upload/sign-url', data);
