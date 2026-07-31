import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { getUser } from '@/api/user';
import { getUserFeeds } from '@/api/feed';
import { follow, unfollow } from '@/api/relation';
import type { UserDetail } from '@/types/user';
import type { FeedCard } from '@/types/feed';
import { useCursorList, type CursorResp } from '@/hooks/useCursorList';
import { useAuthStore } from '@/store/auth';
import FeedCardGrid from '@/components/FeedCardGrid';
import Avatar from '@/components/Avatar';

export default function ProfilePage() {
  const { userId = '' } = useParams<{ userId: string }>();
  const id = userId;
  const me = useAuthStore((s) => s.user);
  const navigate = useNavigate();
  const [user, setUser] = useState<UserDetail | null>(null);

  useEffect(() => {
    getUser(id).then(setUser).catch(() => {});
  }, [id]);

  const fetcher = useCallback(
    (cursor: string): Promise<CursorResp<FeedCard>> =>
      getUserFeeds(id, { cursor: cursor || undefined, page_size: 10 }),
    [id],
  );
  const feeds = useCursorList(fetcher);

  const toggleFollow = async () => {
    if (!user) return;
    if (user.is_following) {
      const r = await unfollow(user.id);
      setUser({ ...user, is_following: false, follower_count: r.follower_count });
    } else {
      const r = await follow(user.id);
      setUser({ ...user, is_following: true, follower_count: r.follower_count });
    }
  };

  if (!user) return <div className="list-end">加载中…</div>;

  return (
    <>
      <div className="card">
        <div className="user-row flush">
          <Avatar src={user.avatar} size={64} />
          <div className="info">
            <h2 className="section-title">{user.nickname}</h2>
            <div className="bio">@{user.username}{user.city_name ? ` · ${user.city_name}` : ''}</div>
          </div>
          {me && me.id === user.id ? (
            <button className="btn small" onClick={() => navigate('/me/edit')}>
              编辑资料
            </button>
          ) : (
            me && (
              <button
                className={`btn small ${user.is_following ? 'active-state' : ''}`}
                onClick={toggleFollow}
              >
                {user.is_following ? '已关注' : '关注'}
              </button>
            )
          )}
        </div>
        {user.bio && <p className="text-secondary" style={{ marginTop: 'var(--space-3)' }}>{user.bio}</p>}
        <div className="stats-row">
          <Link className="stat" to={`/users/${user.id}/relations/following`}>
            <div className="num">{user.following_count}</div>
            <div className="label">关注</div>
          </Link>
          <Link className="stat" to={`/users/${user.id}/relations/followers`}>
            <div className="num">{user.follower_count}</div>
            <div className="label">粉丝</div>
          </Link>
          <div className="stat">
            <div className="num">{user.feed_count}</div>
            <div className="label">帖子</div>
          </div>
        </div>
      </div>

      <FeedCardGrid
        items={feeds.items}
        loading={feeds.loading}
        hasMore={feeds.hasMore}
        sentinelRef={feeds.sentinelRef}
      />
    </>
  );
}
