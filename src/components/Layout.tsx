import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/store/auth';

export default function Layout() {
  const { user, token, logout } = useAuthStore();
  const navigate = useNavigate();

  return (
    <>
      <header className="navbar">
        <div className="navbar-inner">
          <NavLink to="/" className="brand">Feed</NavLink>
          <NavLink to="/" className="nav-link" end>首页</NavLink>
          <NavLink to="/publish" className="nav-link">发布</NavLink>
          <NavLink to="/me/likes" className="nav-link">赞·收藏</NavLink>
          {user && (
            <NavLink to={`/users/${user.id}`} className="nav-link">我的主页</NavLink>
          )}
          <span className="spacer" />
          {token ? (
            <button
              className="btn ghost small"
              onClick={() => {
                logout();
                navigate('/login');
              }}
            >
              退出（{user?.nickname || '我'}）
            </button>
          ) : (
            <NavLink to="/login" className="nav-link">登录</NavLink>
          )}
        </div>
      </header>
      <main className="container">
        <Outlet />
      </main>
    </>
  );
}
