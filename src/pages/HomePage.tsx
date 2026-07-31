import { useCallback, useState } from 'react';
import { Link } from 'react-router-dom';
import { getTimeline } from '@/api/feed';
import type { FeedCard, TimelineType } from '@/types/feed';
import { useCursorList, type CursorResp } from '@/hooks/useCursorList';
import FeedCardGrid from '@/components/FeedCardGrid';

const TABS: { key: TimelineType; label: string }[] = [
  { key: 'recommend', label: '推荐' },
  { key: 'follow', label: '关注' },
  { key: 'city', label: '同城' },
];

export default function HomePage() {
  const [type, setType] = useState<TimelineType>('recommend');

  const fetcher = useCallback(
    (cursor: string): Promise<CursorResp<FeedCard>> =>
      getTimeline({ type, cursor: cursor || undefined, page_size: 10 }),
    [type],
  );

  const { items, loading, hasMore, sentinelRef } = useCursorList(fetcher);

  return (
    <>
      <div className="tabs">
        {TABS.map((t) => (
          <button
            key={t.key}
            className={`tab ${type === t.key ? 'active' : ''}`}
            onClick={() => setType(t.key)}
          >
            {t.label}
          </button>
        ))}
      </div>
      <FeedCardGrid items={items} loading={loading} hasMore={hasMore} sentinelRef={sentinelRef} />
      <Link to="/publish" className="fab" aria-label="发布帖子">+</Link>
    </>
  );
}
