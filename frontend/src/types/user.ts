// 与 backend-contract/gateway/api/user.api 1:1 对应（字段名 = json tag）
// 注意：ID 为后端 snowflake int64，超出 JS 安全整数（2^53），
// 前端统一以 string 承载，避免 JSON.parse 截断低位导致查不到用户。

export interface User {
  id: string;
  username: string;
  nickname: string;
  avatar: string;
  bio: string;
  city_name: string;
}

export interface UserDetail {
  id: string;
  username: string;
  nickname: string;
  avatar: string;
  bio: string;
  city_name: string;
  following_count: number;
  follower_count: number;
  feed_count: number;
  is_following: boolean;
}

export interface RegisterReq {
  username: string;
  password: string;
  nickname: string;
}

export interface RegisterResp {
  user: User;
  token: string;
}

export interface LoginReq {
  username: string;
  password: string;
}

export interface LoginResp {
  user: User;
  token: string;
}

export interface UpdateUserReq {
  nickname?: string;
  avatar?: string;
  bio?: string;
  city_code?: string;
  city_name?: string;
}

export interface UpdateUserResp {
  user: User;
}

export interface UploadTokenReq {
  file_type: string;
  file_ext: string;
}

export interface UploadCredentials {
  tmp_secret_id: string;
  tmp_secret_key: string;
  session_token: string;
  expired_time: number;
}

export interface UploadTokenResp {
  upload_url: string;
  credentials: UploadCredentials;
  file_key: string;
  file_url: string;
}

// POST /upload/sign-url：为私有桶对象生成临时可访问地址
export interface SignUrlReq {
  file_key: string;
  duration?: number;
}

export interface SignUrlResp {
  signed_url: string;
  expired_at: number;
}
