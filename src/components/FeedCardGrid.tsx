import { Link } from 'react-router-dom';
import type { FeedCard } from '@/types/feed';
import { formatRelative } from '@/utils/time';
import Avatar from './Avatar';

interface Props {
  items: FeedCard[];
  loading: boolean;
  hasMore: boolean;
  sentinelRef: React.RefObject<HTMLDivElement | null>;
}

/** FeedCard 瀑布列表 + 无限滚动哨兵（信息流/个人主页/我的赞收藏共用） */
export default function FeedCardGrid({ items, loading, hasMore, sentinelRef }: Props) {
  return (
    <>
      <div className="feed-grid">
        {items.map((f) => (
          <Link className="feed-card" key={f.id} to={`/feeds/${f.id}`}>
            {f.cover_url ? (
              <img className="cover" src={f.cover_url} alt={f.title} loading="lazy" />
            ) : (
              <div className="cover" />
            )}
            <div className="body">
              <div className="title">{f.title || '无标题'}</div>
              <div className="meta">
                <Avatar src={f.author.avatar} size={20} />
                <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {f.author.nickname}
                </span>
                <span>{f.interaction.is_liked ? '❤️' : '🤍'} {f.stats.like_count}</span>
              </div>
              <div className="meta">
                <span>💬 {f.stats.comment_count}</span>
                <span>⭐ {f.stats.collect_count}</span>
                <span style={{ marginLeft: 'auto' }}>{formatRelative(f.created_at)}</span>
              </div>
            </div>
          </Link>
        ))}
      </div>
      <div ref={sentinelRef as React.RefObject<HTMLDivElement>} />
      <div className="list-end">
        {loading ? '加载中…' : hasMore ? '' : items.length === 0 ? '暂无内容' : '没有更多了'}
      </div>
    </>
  );
}
