import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { follow, getFollowerList, getFollowingList, unfollow } from '@/api/relation';
import type { RelationUser } from '@/types/relation';
import Avatar from '@/components/Avatar';

/**
 * 关注 / 粉丝列表。
 * 注意：relation.api 为 offset 分页（page / page_size / total / has_more），
 * 与信息流的 cursor 分页不同，此处按 *.api 实现「加载更多」。
 */
export default function RelationPage() {
  const { userId = '', tab = 'following' } = useParams<{ userId: string; tab: string }>();
  const id = userId;
  const isFollowingTab = tab !== 'followers';
  const navigate = useNavigate();

  const [list, setList] = useState<RelationUser[]>([]);
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(false);

  const load = useCallback(
    async (p: number, reset: boolean) => {
      setLoading(true);
      try {
        const api = isFollowingTab ? getFollowingList : getFollowerList;
        const resp = await api({ user_id: id || undefined, page: p, page_size: 20 });
        setList((prev) => (reset ? resp.list || [] : [...prev, ...(resp.list || [])]));
        setPage(resp.page);
        setHasMore(resp.has_more);
      } finally {
        setLoading(false);
      }
    },
    [id, isFollowingTab],
  );

  useEffect(() => {
    void load(1, true);
  }, [load]);

  const toggleFollow = async (u: RelationUser) => {
    if (u.is_following) await unfollow(u.id);
    else await follow(u.id);
    setList((prev) =>
      prev.map((x) => (x.id === u.id ? { ...x, is_following: !u.is_following } : x)),
    );
  };

  return (
    <div className="card page-narrow">
      <div className="category-tabs">
        <button
          className={`category-tab ${isFollowingTab ? 'active' : ''}`}
          onClick={() => navigate(`/users/${id}/relations/following`)}
        >
          关注
        </button>
        <button
          className={`category-tab ${!isFollowingTab ? 'active' : ''}`}
          onClick={() => navigate(`/users/${id}/relations/followers`)}
        >
          粉丝
        </button>
      </div>

      {list.map((u) => (
        <div className="user-row" key={u.id}>
          <Link to={`/users/${u.id}`}>
            <Avatar src={u.avatar} size={44} />
          </Link>
          <div className="info">
            <Link to={`/users/${u.id}`}><strong>{u.nickname}</strong></Link>
            <div className="bio">{u.bio}</div>
          </div>
          <button
            className={`btn small ${u.is_following ? 'active-state' : ''}`}
            onClick={() => toggleFollow(u)}
          >
            {u.is_following ? '已关注' : '关注'}
          </button>
        </div>
      ))}

      <div className="list-end">
        {loading ? (
          '加载中…'
        ) : hasMore ? (
          <button className="btn ghost small" onClick={() => load(page + 1, false)}>
            加载更多
          </button>
        ) : list.length === 0 ? (
          '暂无数据'
        ) : (
          '没有更多了'
        )}
      </div>
    </div>
  );
}
