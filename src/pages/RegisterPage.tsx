import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { register } from '@/api/user';
import { useAuthStore } from '@/store/auth';
import { toast } from '@/utils/toast';

export default function RegisterPage() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [nickname, setNickname] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const setAuth = useAuthStore((s) => s.setAuth);
  const navigate = useNavigate();

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username || !password || !nickname) {
      toast('请填写完整信息');
      return;
    }
    setSubmitting(true);
    try {
      const resp = await register({ username, password, nickname });
      setAuth(resp.token, resp.user);
      toast('注册成功', 'success');
      navigate('/', { replace: true });
    } catch {
      // 错误已由拦截器 toast
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form className="form" onSubmit={onSubmit}>
      <h1>注册</h1>
      <div className="field">
        <label>用户名</label>
        <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
      </div>
      <div className="field">
        <label>昵称</label>
        <input value={nickname} onChange={(e) => setNickname(e.target.value)} />
      </div>
      <div className="field">
        <label>密码</label>
        <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
      </div>
      <button className="btn block" disabled={submitting}>
        {submitting ? '注册中…' : '注册'}
      </button>
      <p className="form-hint">
        已有账号？<Link to="/login" className="text-brand">去登录</Link>
      </p>
    </form>
  );
}
