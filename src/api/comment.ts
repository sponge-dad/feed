import { http } from './request';
import type {
  CreateCommentReq,
  CreateCommentResp,
  DeleteCommentResp,
  LikeCommentResp,
  ListCommentsResp,
  ListRepliesResp,
  UnlikeCommentResp,
} from '@/types/comment';
import type { CursorParams } from '@/types/feed';

export const createComment = (feedId: string, data: CreateCommentReq) =>
  http.post<CreateCommentResp>(`/feeds/${feedId}/comments`, data);

export const listComments = (feedId: string, params: CursorParams) =>
  http.get<ListCommentsResp>(`/feeds/${feedId}/comments`, params);

export const listReplies = (rootId: string, params: CursorParams) =>
  http.get<ListRepliesResp>(`/comments/${rootId}/replies`, params);

export const deleteComment = (commentId: string) =>
  http.delete<DeleteCommentResp>(`/comments/${commentId}`);

export const likeComment = (commentId: string) =>
  http.post<LikeCommentResp>(`/comments/${commentId}/like`);

export const unlikeComment = (commentId: string) =>
  http.delete<UnlikeCommentResp>(`/comments/${commentId}/like`);
