// 与 backend-contract/gateway/api/comment.api 1:1 对应
// ID 字段为 int64，前端以 string 承载，避免 JSON 解析精度丢失。

export interface CommentAuthor {
  id: string;
  nickname: string;
  avatar: string;
}

export interface CommentReplyUser {
  id: string;
  nickname: string;
}

export interface CommentReply {
  id: string;
  content: string;
  author: CommentAuthor;
  reply_user: CommentReplyUser;
  like_count: number;
  is_liked: boolean;
  created_at: number;
}

export interface CommentEntry {
  id: string;
  content: string;
  author: CommentAuthor;
  like_count: number;
  is_liked: boolean;
  reply_count: number;
  created_at: number;
  sub_replies: CommentReply[];
}

export interface CreateCommentReq {
  content: string;
  root_id?: string;
  parent_id?: string;
  reply_user_id?: string;
}

export interface CommentDetail {
  id: string;
  feed_id: string;
  content: string;
  root_id: string;
  parent_id: string;
  author: CommentAuthor;
  reply_user: CommentReplyUser;
  like_count: number;
  created_at: number;
}

export interface CreateCommentResp {
  comment: CommentDetail;
}

export interface ListCommentsResp {
  hot_comments: CommentEntry[];
  list: CommentEntry[];
  next_cursor: string;
  has_more: boolean;
}

export interface ListRepliesResp {
  list: CommentReply[];
  next_cursor: string;
  has_more: boolean;
}

export interface DeleteCommentResp {
  success: boolean;
}

export interface LikeCommentResp {
  success: boolean;
  like_count: number;
}

export interface UnlikeCommentResp {
  success: boolean;
  like_count: number;
}
