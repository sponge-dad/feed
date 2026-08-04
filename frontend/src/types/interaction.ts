// 与 backend-contract/gateway/api/interaction.api 1:1 对应

export interface LikeFeedResp {
  success: boolean;
  like_count: number;
}

export interface UnlikeFeedResp {
  success: boolean;
  like_count: number;
}

export interface CollectFeedResp {
  success: boolean;
  collect_count: number;
}

export interface UncollectFeedResp {
  success: boolean;
  collect_count: number;
}
