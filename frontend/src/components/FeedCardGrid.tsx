import { Link } from 'react-router-dom';
import type { FeedCard } from '@/types/feed';
import Avatar from './Avatar';

interface Props {
  items: FeedCard[];
  loading: boolean;
  hasMore: boolean;
  sentinelRef: React.RefObject<HTMLDivElement | null>;
  variant?: 'waterfall' | 'grid';
}

// 交错比例，制造高低错落的瀑布流视觉；无图时作为占位高度
const RATIOS = ['3 / 4', '1 / 1', '3 / 4', '4 / 5', '1 / 1', '4 / 3', '3 / 4', '1 / 1'];

/** FeedCard 列表 + 无限滚动哨兵（信息流/个人主页/我的赞收藏共用） */
export default function FeedCardGrid({
  items,
  loading,
  hasMore,
  sentinelRef,
  variant = 'waterfall',
}: Props) {
  const containerClass = variant === 'grid' ? 'feed-grid' : 'waterfall';
  return (
    <>
      <div className={containerClass}>
        {items.map((f, i) => {
          const ratio = RATIOS[i % RATIOS.length];
          return (
            <Link className="waterfall-card" key={f.id} to={`/feeds/${f.id}`}>
              <div className="card-img-wrap" style={{ aspectRatio: ratio }}>
                {f.cover_url ? (
                  <img
                    className="card-img"
                    src={f.cover_url}
                    alt={f.title || '帖子封面'}
                    loading="lazy"
                    onError={(e) => {
                      (e.currentTarget as HTMLImageElement).style.visibility = 'hidden';
                    }}
                  />
                ) : (
                  <div className="card-img" />
                )}
                <div className="card-img-overlay">
                  <span className="overlay-like">❤️ {f.stats.like_count}</span>
                </div>
              </div>
              <div className="card-body">
                <div className="card-title">{f.title || '无标题'}</div>
                <div className="card-footer">
                  <Avatar src={f.author.avatar} size={20} alt={f.author.nickname} />
                  <span className="name">{f.author.nickname}</span>
                </div>
              </div>
            </Link>
          );
        })}
      </div>
      <div ref={sentinelRef as React.RefObject<HTMLDivElement>} />
      <div className="list-end">
        {loading
          ? '加载中…'
          : hasMore
            ? ''
            : items.length === 0
              ? '暂无内容'
              : '没有更多了'}
      </div>
    </>
  );
}
