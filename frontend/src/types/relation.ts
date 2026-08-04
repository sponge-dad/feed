// 与 backend-contract/gateway/api/relation.api 1:1 对应
// 关注/粉丝列表是 offset 分页（page/page_size/total/has_more），以 *.api 为准
// ID 字段为 int64，前端以 string 承载，避免 JSON 解析精度丢失。

export interface FollowReq {
  followee_id: string;
}

export interface FollowResp {
  success: boolean;
  follower_count: number;
}

export interface UnfollowReq {
  followee_id: string;
}

export interface UnfollowResp {
  success: boolean;
  follower_count: number;
}

export interface RelationUser {
  id: string;
  nickname: string;
  avatar: string;
  bio: string;
  is_following: boolean;
}

export interface RelationListParams {
  user_id?: string;
  page?: number;
  page_size?: number;
}

export interface RelationUserList {
  list: RelationUser[];
  page: number;
  page_size: number;
  total: number;
  has_more: boolean;
}

export interface IsFollowingResp {
  is_following: boolean;
}
