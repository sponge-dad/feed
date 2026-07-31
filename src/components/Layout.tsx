import { useState } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/store/auth';
import { toast } from '@/utils/toast';

export default function Layout() {
  const { user, token, logout } = useAuthStore();
  const navigate = useNavigate();
  const [query, setQuery] = useState('');

  const onSearch = (e: React.FormEvent) => {
    e.preventDefault();
    const q = query.trim();
    if (q) toast(`搜索「${q}」功能即将上线`);
  };

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <NavLink to="/" className="brand" end>
          Feed
        </NavLink>
        <nav className="sidebar-nav">
          <NavLink to="/" className="sidebar-link" end>
            首页
          </NavLink>
          <NavLink to="/me/likes" className="sidebar-link">
            赞·收藏
          </NavLink>
          {user && (
            <NavLink to={`/users/${user.id}`} className="sidebar-link">
              我的主页
            </NavLink>
          )}
        </nav>
        <span className="spacer" />
        {token ? (
          <button
            className="btn ghost block"
            onClick={() => {
              logout();
              navigate('/login');
            }}
          >
            退出（{user?.nickname || '我'}）
          </button>
        ) : (
          <NavLink to="/login" className="sidebar-link">
            登录
          </NavLink>
        )}
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
