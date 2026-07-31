import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { User } from '@/types/user';

// token 持久化在 localStorage。
// XSS 注意：本项目所有渲染均通过 React 文本节点 / textContent，不使用
// dangerouslySetInnerHTML，不 eval 任何服务端字符串，避免 token 被脚本窃取。
interface AuthState {
  token: string;
  user: User | null;
  setAuth: (token: string, user: User) => void;
  setUser: (user: User) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set) => ({
      token: '',
      user: null,
      setAuth: (token, user) => set({ token, user }),
      setUser: (user) => set({ user }),
      logout: () => set({ token: '', user: null }),
    }),
    {
      name: 'feed-auth',
      // 只持久化 token；user 不落盘，每次启动由 /users/me 拉取正确数据。
      // 否则旧会话残留的 user（可能带错误/截断的 id）会被用来拼主页链接，
      // 导致进入"非自己"的主页、显示"关注"而非"编辑资料"。
      partialize: (state) => ({ token: state.token }),
      // 自定义 merge：只读回 token，彻底丢弃旧存储中可能残留的 user，
      // 保证 user 初始为 null，由启动时的 getMe() 填充服务端正确的用户。
      merge: (persisted, current) => ({
        ...current,
        token: (persisted as { token?: string })?.token ?? current.token,
      }),
    },
  ),
);

export const isLoggedIn = () => Boolean(useAuthStore.getState().token);
