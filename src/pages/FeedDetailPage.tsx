import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { deleteFeed, getFeedDetail } from '@/api/feed';
import { collectFeed, likeFeed, uncollectFeed, unlikeFeed } from '@/api/interaction';
import { follow, unfollow } from '@/api/relation';
import {
  createComment,
  deleteComment,
  likeComment,
  listComments,
  listReplies,
  unlikeComment,
} from '@/api/comment';
import type { FeedDetail } from '@/types/feed';
import type { CommentEntry, CommentReply } from '@/types/comment';
import { useCursorList, type CursorResp } from '@/hooks/useCursorList';
import { useAuthStore } from '@/store/auth';
import { formatRelative, formatTime } from '@/utils/time';
import { toast } from '@/utils/toast';
import Avatar from '@/components/Avatar';

interface ReplyTarget {
  rootId: string;
  parentId: string;
  replyUserId: string;
  nickname: string;
}

export default function FeedDetailPage() {
  const { feedId = '' } = useParams<{ feedId: string }>();
  const id = feedId;
  const navigate = useNavigate();
  const me = useAuthStore((s) => s.user);

  const [feed, setFeed] = useState<FeedDetail | null>(null);
  const [commentText, setCommentText] = useState('');
  const [replyTarget, setReplyTarget] = useState<ReplyTarget | null>(null);
  const [expandedReplies, setExpandedReplies] = useState<Record<string, CommentReply[]>>({});

  useEffect(() => {
    getFeedDetail(id).then(setFeed).catch(() => {});
  }, [id]);

  const commentFetcher = useCallback(
    (cursor: string): Promise<CursorResp<CommentEntry>> =>
      listComments(id, { cursor: cursor || undefined, page_size: 20 }).then((r) => ({
        list: cursor ? r.list : [...(r.hot_comments || []), ...(r.list || [])],
        next_cursor: r.next_cursor,
        has_more: r.has_more,
      })),
    [id],
  );
  const comments = useCursorList(commentFetcher);

  if (!feed) return <div className="list-end">加载中…</div>;

  // ---- 帖子互动 ----
  const toggleLike = async () => {
    const r = feed.interaction.is_liked ? await unlikeFeed(id) : await likeFeed(id);
    setFeed({
      ...feed,
      stats: { ...feed.stats, like_count: r.like_count },
      interaction: { ...feed.interaction, is_liked: !feed.interaction.is_liked },
    });
  };

  const toggleCollect = async () => {
    const r = feed.interaction.is_collected ? await uncollectFeed(id) : await collectFeed(id);
    setFeed({
      ...feed,
      stats: { ...feed.stats, collect_count: r.collect_count },
      interaction: { ...feed.interaction, is_collected: !feed.interaction.is_collected },
    });
  };

  const toggleFollow = async () => {
    if (feed.author.is_following) await unfollow(feed.author.id);
    else await follow(feed.author.id);
    setFeed({ ...feed, author: { ...feed.author, is_following: !feed.author.is_following } });
  };

  const onDeleteFeed = async () => {
    if (!confirm('确定删除该帖子？')) return;
    await deleteFeed(id);
    toast('删除成功', 'success');
    navigate('/');
  };

  // ---- 评论 ----
  const submitComment = async () => {
    const content = commentText.trim();
    if (!content) return;
    await createComment(id, {
      content,
      root_id: replyTarget?.rootId || undefined,
      parent_id: replyTarget?.parentId || undefined,
      reply_user_id: replyTarget?.replyUserId || undefined,
    });
    toast('评论成功', 'success');
    setCommentText('');
    setReplyTarget(null);
    comments.reload();
    setFeed((f) =>
      f ? { ...f, stats: { ...f.stats, comment_count: f.stats.comment_count + 1 } } : f,
    );
  };

  const toggleCommentLike = async (c: CommentEntry) => {
    const r = c.is_liked ? await unlikeComment(c.id) : await likeComment(c.id);
    comments.setItems((prev) =>
      prev.map((x) => (x.id === c.id ? { ...x, is_liked: !c.is_liked, like_count: r.like_count } : x)),
    );
  };

  const onDeleteComment = async (commentId: string) => {
    if (!confirm('确定删除该评论？')) return;
    await deleteComment(commentId);
    comments.setItems((prev) => prev.filter((x) => x.id !== commentId));
  };

  const loadReplies = async (rootId: string) => {
    const r = await listReplies(rootId, { page_size: 20 });
    setExpandedReplies((prev) => ({ ...prev, [rootId]: r.list || [] }));
  };

  return (
    <>
      <div className="card">
        {/* 作者栏 */}
        <div className="user-row" style={{ borderBottom: 'none', padding: 0 }}>
          <Link to={`/users/${feed.author.id}`}>
            <Avatar src={feed.author.avatar} size={40} />
          </Link>
          <div className="info">
            <Link to={`/users/${feed.author.id}`}><strong>{feed.author.nickname}</strong></Link>
            <div className="bio">
              {formatTime(feed.created_at)}
              {feed.ip_location ? ` · ${feed.ip_location}` : ''}
              {feed.city_name ? ` · ${feed.city_name}` : ''}
            </div>
          </div>
          {me && me.id !== feed.author.id && (
            <button
              className={`btn small ${feed.author.is_following ? 'active-state' : ''}`}
              onClick={toggleFollow}
            >
              {feed.author.is_following ? '已关注' : '关注'}
            </button>
          )}
          {me && me.id === feed.author.id && (
            <button className="btn ghost small" onClick={onDeleteFeed}>删除</button>
          )}
        </div>

        <h2 style={{ margin: '12px 0 8px', fontSize: 18 }}>{feed.title}</h2>
        <p style={{ fontSize: 14, lineHeight: 1.7, whiteSpace: 'pre-wrap' }}>{feed.description}</p>

        {/* 媒体：feed_type 语义待后端确认，暂按 URL/类型猜测视频 */}
        <div className="media-grid">
          {feed.media_urls?.map((url) =>
            /\.(mp4|mov|webm)(\?|$)/i.test(url) ? (
              <video key={url} src={url} controls preload="metadata" />
            ) : (
              <img key={url} src={url} alt="" loading="lazy" />
            ),
          )}
        </div>

        <div className="detail-actions">
          <button
            className={`btn ghost small ${feed.interaction.is_liked ? 'active-state' : ''}`}
            onClick={toggleLike}
          >
            {feed.interaction.is_liked ? '❤️' : '🤍'} 赞 {feed.stats.like_count}
          </button>
          <button
            className={`btn ghost small ${feed.interaction.is_collected ? 'active-state' : ''}`}
            onClick={toggleCollect}
          >
            ⭐ 收藏 {feed.stats.collect_count}
          </button>
          <span style={{ alignSelf: 'center', fontSize: 13, color: '#888' }}>
            💬 评论 {feed.stats.comment_count}
          </span>
        </div>
      </div>

      {/* 评论输入 */}
      <div className="card">
        {replyTarget && (
          <div style={{ fontSize: 13, color: '#888', marginBottom: 8 }}>
            回复 @{replyTarget.nickname}{' '}
            <span style={{ cursor: 'pointer', color: '#e5484d' }} onClick={() => setReplyTarget(null)}>
              取消
            </span>
          </div>
        )}
        <div style={{ display: 'flex', gap: 8 }}>
          <input
            style={{ flex: 1, padding: '10px 12px', border: '1px solid #ddd', borderRadius: 8 }}
            placeholder={replyTarget ? `回复 @${replyTarget.nickname}` : '说点什么…'}
            value={commentText}
            onChange={(e) => setCommentText(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && submitComment()}
          />
          <button className="btn" onClick={submitComment}>发送</button>
        </div>

        {/* 评论列表（首屏含热评） */}
        {comments.items.map((c) => (
          <div className="comment-item" key={c.id}>
            <Link to={`/users/${c.author.id}`}>
              <Avatar src={c.author.avatar} size={32} />
            </Link>
            <div className="content">
              <div className="nick">{c.author.nickname}</div>
              <div>{c.content}</div>
              <div className="comment-actions">
                <span className="time">{formatRelative(c.created_at)}</span>
                <span className={c.is_liked ? 'liked' : ''} onClick={() => toggleCommentLike(c)}>
                  {c.is_liked ? '❤️' : '🤍'} {c.like_count}
                </span>
                <span
                  onClick={() =>
                    setReplyTarget({
                      rootId: c.id,
                      parentId: c.id,
                      replyUserId: c.author.id,
                      nickname: c.author.nickname,
                    })
                  }
                >
                  回复
                </span>
                {me?.id === c.author.id && (
                  <span onClick={() => onDeleteComment(c.id)}>删除</span>
                )}
              </div>

              {/* 子回复预览 + 展开全部 */}
              {(expandedReplies[c.id] || c.sub_replies || []).length > 0 && (
                <div className="sub-replies">
                  {(expandedReplies[c.id] || c.sub_replies).map((r) => (
                    <div key={r.id} style={{ fontSize: 13, padding: '4px 0' }}>
                      <span className="nick">{r.author.nickname}</span>
                      {r.reply_user?.id ? (
                        <span className="nick"> 回复 {r.reply_user.nickname}</span>
                      ) : null}
                      ：{r.content}
                      <span
                        className="nick"
                        style={{ marginLeft: 8, cursor: 'pointer' }}
                        onClick={() =>
                          setReplyTarget({
                            rootId: c.id,
                            parentId: r.id,
                            replyUserId: r.author.id,
                            nickname: r.author.nickname,
                          })
                        }
                      >
                        回复
                      </span>
                    </div>
                  ))}
                  {!expandedReplies[c.id] && c.reply_count > (c.sub_replies?.length || 0) && (
                    <div
                      style={{ fontSize: 12, color: '#e5484d', cursor: 'pointer', marginTop: 4 }}
                      onClick={() => loadReplies(c.id)}
                    >
                      展开全部 {c.reply_count} 条回复
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        ))}
        <div ref={comments.sentinelRef as React.RefObject<HTMLDivElement>} />
        <div className="list-end">
          {comments.loading ? '加载中…' : comments.hasMore ? '' : '没有更多评论'}
        </div>
      </div>
    </>
  );
}
