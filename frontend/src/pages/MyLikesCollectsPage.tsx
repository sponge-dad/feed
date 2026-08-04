import { useCallback } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { getMyCollects, getMyLikes } from '@/api/interaction';
import type { FeedCard } from '@/types/feed';
import { useCursorList, type CursorResp } from '@/hooks/useCursorList';
import FeedCardGrid from '@/components/FeedCardGrid';

/** 我的赞 / 我的收藏（路由 /me/likes、/me/collects 复用本页） */
export default function MyLikesCollectsPage() {
  const { tab = 'likes' } = useParams<{ tab: string }>();
  const navigate = useNavigate();
  const isLikes = tab !== 'collects';

  const fetcher = useCallback(
    (cursor: string): Promise<CursorResp<FeedCard>> => {
      const params = { cursor: cursor || undefined, page_size: 10 };
      return isLikes ? getMyLikes(params) : getMyCollects(params);
    },
    [isLikes],
  );

  const { items, loading, hasMore, sentinelRef } = useCursorList(fetcher);

  return (
    <>
      <div className="tabs">
        <button className={`tab ${isLikes ? 'active' : ''}`} onClick={() => navigate('/me/likes')}>
          我的赞
        </button>
        <button className={`tab ${!isLikes ? 'active' : ''}`} onClick={() => navigate('/me/collects')}>
          我的收藏
        </button>
      </div>
      <FeedCardGrid items={items} loading={loading} hasMore={hasMore} sentinelRef={sentinelRef} />
    </>
  );
}
