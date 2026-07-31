import { http, request } from './request';
import type {
  FollowResp,
  IsFollowingResp,
  RelationListParams,
  RelationUserList,
  UnfollowResp,
} from '@/types/relation';

export const follow = (followeeId: string) =>
  http.post<FollowResp>('/relations/follow', { followee_id: followeeId });

// 契约（relation.api）定义取关为 DELETE /relations/follow，参数在 JSON body。
// 但部署链路的网关/反向代理会丢弃 DELETE 的 body，导致后端解析不到 followee_id。
// 后端 UnfollowReq 已加 form:"followee_id" tag，使 go-zero 能从 URL query 解析；
// 故此处改用 query 传参，绕开 DELETE body 被丢弃的问题（body 仍被 json tag 兼容）。
export const unfollow = (followeeId: string) =>
  request<UnfollowResp>({ method: 'DELETE', url: '/relations/follow', params: { followee_id: followeeId } });

export const getFollowingList = (params: RelationListParams) =>
  http.get<RelationUserList>('/relations/following', params);

export const getFollowerList = (params: RelationListParams) =>
  http.get<RelationUserList>('/relations/followers', params);

export const isFollowing = (targetId: string) =>
  http.get<IsFollowingResp>('/relations/is-following', { target_id: targetId });
