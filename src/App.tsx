import { useEffect } from 'react';
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { useAuthStore } from '@/store/auth';
import { getMe } from '@/api/user';
import Layout from '@/components/Layout';
import LoginPage from '@/pages/LoginPage';
import RegisterPage from '@/pages/RegisterPage';
import HomePage from '@/pages/HomePage';
import FeedDetailPage from '@/pages/FeedDetailPage';
import ProfilePage from '@/pages/ProfilePage';
import EditProfilePage from '@/pages/EditProfilePage';
import RelationPage from '@/pages/RelationPage';
import MyLikesCollectsPage from '@/pages/MyLikesCollectsPage';
import PublishPage from '@/pages/PublishPage';

/** 路由守卫：契约中除注册/登录外全部需 JWT，未登录直接进登录页 */
function RequireAuth({ children }: { children: React.ReactNode }) {
  const token = useAuthStore((s) => s.token);
  const location = useLocation();
  if (!token) {
    const redirect = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?redirect=${redirect}`} replace />;
  }
  return <>{children}</>;
}

export default function App() {
  // 启动自愈：localStorage 持久化的 user 可能因旧代码存了被截断的 id，
  // 用 token 拉一次 /users/me 刷新为服务端返回的正确字符串 id。
  // token 失效则清空登录态，交由路由守卫跳登录。
  useEffect(() => {
    const { token, setUser, logout } = useAuthStore.getState();
    if (!token) return;
    // 始终用 token 拉取服务端最新的用户信息，覆盖 localStorage 中可能
    // 因旧代码而截断的 id，避免用错误 id 拼出 /users/:id 导致"用户不存在"。
    getMe()
      .then(setUser)
      .catch(() => logout());
  }, []);

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route element={<Layout />}>
          <Route path="/" element={<RequireAuth><HomePage /></RequireAuth>} />
          <Route path="/publish" element={<RequireAuth><PublishPage /></RequireAuth>} />
          <Route path="/feeds/:feedId" element={<RequireAuth><FeedDetailPage /></RequireAuth>} />
          <Route path="/users/:userId" element={<RequireAuth><ProfilePage /></RequireAuth>} />
          <Route path="/me/edit" element={<RequireAuth><EditProfilePage /></RequireAuth>} />
          <Route
            path="/users/:userId/relations/:tab"
            element={<RequireAuth><RelationPage /></RequireAuth>}
          />
          <Route path="/me/:tab" element={<RequireAuth><MyLikesCollectsPage /></RequireAuth>} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
