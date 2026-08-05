// 与 backend-contract/gateway/api/feed.api 1:1 对应
// ID 字段（id / user_id）为 int64，前端以 string 承载，避免 JSON 解析精度丢失。

export interface FeedAuthor {
  id: string;
  nickname: string;
  avatar: string;
}

export interface FeedAuthorDetail {
  id: string;
  nickname: string;
  avatar: string;
  is_following: boolean;
}

export interface FeedStatsInfo {
  like_count: number;
  comment_count: number;
  collect_count: number;
}

export interface FeedInteractionInfo {
  is_liked: boolean;
  is_collected: boolean;
}

export interface FeedCard {
  id: string;
  feed_type: number;
  title: string;
  cover_url: string;
  author: FeedAuthor;
  stats: FeedStatsInfo;
  interaction: FeedInteractionInfo;
  created_at: number;
  // 信息流来源标记（FeedSource）：0 未知 / 1 关注推模式(inbox) / 2 关注大V(outbox) /
  // 3 inbox 兜底重建 / 4 同城池 / 5 推荐池。用于排障与体验优化。
  source: number;
}

export interface FeedCardList {
  list: FeedCard[];
  next_cursor: string;
  has_more: boolean;
}

export interface CreateFeedReq {
  feed_type: number;
  title?: string;
  description?: string;
  media_urls: string[];
  cover_url?: string;
}

export interface FeedInfo {
  id: string;
  user_id: string;
  feed_type: number;
  title: string;
  description: string;
  media_urls: string[];
  cover_url: string;
  city_name: string;
  ip_location: string;
  created_at: number;
}

export interface CreateFeedResp {
  feed: FeedInfo;
}

export interface DeleteFeedResp {
  success: boolean;
}

export interface FeedDetail {
  id: string;
  feed_type: number;
  title: string;
  description: string;
  media_urls: string[];
  cover_url: string;
  city_name: string;
  ip_location: string;
  created_at: number;
  author: FeedAuthorDetail;
  stats: FeedStatsInfo;
  interaction: FeedInteractionInfo;
}

export type TimelineType = 'recommend' | 'follow' | 'city';

export interface TimelineParams {
  type?: TimelineType;
  cursor?: string;
  page_size?: number;
}

export interface CursorParams {
  cursor?: string;
  page_size?: number;
}
