import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { updateMe } from '@/api/user';
import { useAuthStore } from '@/store/auth';
import { uploadFile } from '@/utils/upload';
import { toast } from '@/utils/toast';
import Avatar from '@/components/Avatar';

export default function EditProfilePage() {
  const me = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const navigate = useNavigate();

  const [nickname, setNickname] = useState(me?.nickname ?? '');
  const [bio, setBio] = useState(me?.bio ?? '');
  const [cityName, setCityName] = useState(me?.city_name ?? '');
  const [avatar, setAvatar] = useState(me?.avatar ?? '');
  const [avatarProgress, setAvatarProgress] = useState<number | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const onPickAvatar = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ''; // 允许重复选择同一文件
    if (!file) return;
    if (!file.type.startsWith('image/')) {
      toast('请选择图片文件');
      return;
    }
    if (file.size > 5 * 1024 * 1024) {
      toast('头像图片不能超过 5MB');
      return;
    }
    setAvatarProgress(0);
    try {
      const res = await uploadFile(file, 'avatar', setAvatarProgress);
      setAvatar(res.file_url);
      toast('头像已上传', 'success');
    } catch {
      toast('头像上传失败');
    } finally {
      setAvatarProgress(null);
    }
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    // 注意：昵称允许为空。后端 updateUserLogic 仅在 Nickname != "" 时才更新昵称，
    // 因此即便前端留空也不会覆盖已有昵称。此前此处强制「昵称不能为空」会静默阻断保存，
    // 导致只上传了头像（落 COS）却从未调用 updateMe，刷新后头像消失、DB 也不更新。
    setSubmitting(true);
    try {
      const resp = await updateMe({
        nickname: nickname.trim(),
        bio: bio.trim(),
        city_name: cityName.trim(),
        avatar: avatar.trim(),
      });
      setUser(resp.user);
      toast('资料已更新', 'success');
      navigate(`/users/${resp.user.id}`, { replace: true });
    } catch {
      // 错误已由拦截器 toast
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form className="form" onSubmit={onSubmit}>
      <h1>编辑资料</h1>

      <div className="field avatar-field">
        <label>头像</label>
        <div className="avatar-edit">
          <Avatar src={avatar} size={96} alt={nickname} />
          <div className="avatar-actions">
            <label className="btn small">
              {avatarProgress !== null ? `上传中 ${avatarProgress}%` : '更换头像'}
              <input type="file" accept="image/*" onChange={onPickAvatar} hidden />
            </label>
            {avatar && (
              <button
                type="button"
                className="btn small ghost"
                onClick={() => setAvatar('')}
              >
                移除
              </button>
            )}
          </div>
        </div>
      </div>

      <div className="field">
        <label>昵称</label>
        <input
          value={nickname}
          onChange={(e) => setNickname(e.target.value)}
          maxLength={20}
        />
      </div>
      <div className="field">
        <label>个人简介</label>
          <textarea
            className="textarea"
            value={bio}
            onChange={(e) => setBio(e.target.value)}
            rows={3}
            maxLength={200}
            placeholder="介绍一下自己…"
          />
      </div>
      <div className="field">
        <label>城市</label>
        <input
          value={cityName}
          onChange={(e) => setCityName(e.target.value)}
          maxLength={20}
          placeholder="如：深圳"
        />
      </div>
      <button className="btn block" disabled={submitting || avatarProgress !== null}>
        {submitting ? '保存中…' : '保存'}
      </button>
      <button type="button" className="btn ghost block" onClick={() => navigate(-1)}>
        取消
      </button>
    </form>
  );
}
