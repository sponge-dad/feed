import { useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { login } from '@/api/user';
import { useAuthStore } from '@/store/auth';
import { toast } from '@/utils/toast';

export default function LoginPage() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const setAuth = useAuthStore((s) => s.setAuth);
  const navigate = useNavigate();
  const [params] = useSearchParams();

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username || !password) {
      toast('请输入用户名和密码');
      return;
    }
    setSubmitting(true);
    try {
      const resp = await login({ username, password });
      setAuth(resp.token, resp.user);
      toast('登录成功', 'success');
      navigate(params.get('redirect') || '/', { replace: true });
    } catch {
      // 错误已由拦截器 toast
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form className="form" onSubmit={onSubmit}>
      <h1>登录</h1>
      <div className="field">
        <label>用户名</label>
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
      </div>
      <div className="field">
        <label>密码</label>
        <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
      </div>
      <button className="btn block" disabled={submitting}>
        {submitting ? '登录中…' : '登录'}
      </button>
      <p className="form-hint">
        没有账号？<Link to="/register" className="text-brand">去注册</Link>
      </p>
    </form>
  );
}
