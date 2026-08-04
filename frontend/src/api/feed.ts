import { http } from './request';
import type {
  CreateFeedReq,
  CreateFeedResp,
  CursorParams,
  DeleteFeedResp,
  FeedCardList,
  FeedDetail,
  TimelineParams,
} from '@/types/feed';

export const createFeed = (data: CreateFeedReq) => http.post<CreateFeedResp>('/feeds', data);

export const deleteFeed = (feedId: string) => http.delete<DeleteFeedResp>(`/feeds/${feedId}`);

export const getFeedDetail = (feedId: string) => http.get<FeedDetail>(`/feeds/${feedId}`);

// 首页信息流：type = recommend | follow | city，cursor 分页
export const getTimeline = (params: TimelineParams) =>
  http.get<FeedCardList>('/feeds/timeline', params);

// 个人主页帖子列表
export const getUserFeeds = (userId: string, params: CursorParams) =>
  http.get<FeedCardList>(`/users/${userId}/feeds`, params);
