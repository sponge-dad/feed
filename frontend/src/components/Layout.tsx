import { useState } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/store/auth';
import { toast } from '@/utils/toast';
import Avatar from './Avatar';

export default function Layout() {
  const { user, logout } = useAuthStore();
  const navigate = useNavigate();
  const [query, setQuery] = useState('');

  const onSearch = (e: React.FormEvent) => {
    e.preventDefault();
    const q = query.trim();
    if (q) toast(`搜索「${q}」功能即将上线`);
  };

  const linkClass = ({ isActive }: { isActive: boolean }) =>
    isActive ? 'sidebar-link active' : 'sidebar-link';

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <NavLink to="/" className="sidebar-brand" end>
          <span aria-hidden="true">📕</span> 小红薯
        </NavLink>
        <nav className="sidebar-nav">
          <NavLink to="/" className={linkClass} end>
            <span className="nav-icon" aria-hidden="true">🏠</span> 首页
          </NavLink>
          <NavLink to="/me/likes" className={linkClass}>
            <span className="nav-icon" aria-hidden="true">❤️</span> 赞·收藏
          </NavLink>
          {user && (
            <NavLink to={`/users/${user.id}`} className={linkClass}>
              <span className="nav-icon" aria-hidden="true">👤</span> 我的主页
            </NavLink>
          )}
        </nav>
        <div className="sidebar-footer">
          <div className="sidebar-user">
            {user ? (
              <>
                <Avatar src={user.avatar} size={36} alt={user.nickname} />
                <div className="info">
                  <div className="nick">{user.nickname}</div>
                  <div className="uname">@{user.username}</div>
                </div>
                <button
                  type="button"
                  className="link-action-btn"
                  aria-label="退出登录"
                  onClick={() => {
                    logout();
                    navigate('/login');
                  }}
                >
                  <span className="nav-icon" aria-hidden="true">⏻</span>
                </button>
              </>
            ) : (
              <NavLink to="/login" className="sidebar-link" style={{ padding: 0 }}>
                <span className="nav-icon" aria-hidden="true">👤</span> 登录
              </NavLink>
            )}
          </div>
        </div>
      </aside>

      <div className="main-area">
        <header className="topbar">
          <form className="searchbar" onSubmit={onSearch}>
            <input
              className="input"
              placeholder="搜索用户、帖子…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <button className="btn" type="submit">
              搜索
            </button>
          </form>
        </header>
        <main className="content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
