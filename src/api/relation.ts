import { http } from './request';
import type {
  FollowResp,
  IsFollowingResp,
  RelationListParams,
  RelationUserList,
  UnfollowResp,
} from '@/types/relation';

export const follow = (followeeId: string) =>
  http.post<FollowResp>('/relations/follow', { followee_id: followeeId });

// DELETE 带 body（UnfollowReq 为 json 字段），axios delete 需放 data
export const unfollow = (followeeId: string) =>
  http.delete<UnfollowResp>('/relations/follow', { followee_id: followeeId });

export const getFollowingList = (params: RelationListParams) =>
  http.get<RelationUserList>('/relations/following', params);

export const getFollowerList = (params: RelationListParams) =>
  http.get<RelationUserList>('/relations/followers', params);

export const isFollowing = (targetId: string) =>
  http.get<IsFollowingResp>('/relations/is-following', { target_id: targetId });
