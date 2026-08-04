import { http } from './request';
import type { CursorParams, FeedCardList } from '@/types/feed';
import type {
  CollectFeedResp,
  LikeFeedResp,
  UncollectFeedResp,
  UnlikeFeedResp,
} from '@/types/interaction';

export const likeFeed = (feedId: string) => http.post<LikeFeedResp>(`/feeds/${feedId}/like`);

export const unlikeFeed = (feedId: string) => http.delete<UnlikeFeedResp>(`/feeds/${feedId}/like`);

export const collectFeed = (feedId: string) =>
  http.post<CollectFeedResp>(`/feeds/${feedId}/collect`);

export const uncollectFeed = (feedId: string) =>
  http.delete<UncollectFeedResp>(`/feeds/${feedId}/collect`);

export const getMyLikes = (params: CursorParams) =>
  http.get<FeedCardList>('/users/me/likes', params);

export const getMyCollects = (params: CursorParams) =>
  http.get<FeedCardList>('/users/me/collects', params);
