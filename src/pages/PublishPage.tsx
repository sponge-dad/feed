import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { createFeed } from '@/api/feed';
import { uploadFile } from '@/utils/upload';
import { getSignedUrl } from '@/utils/signUrl';
import { toast } from '@/utils/toast';

// feed_type 数值语义未在契约中枚举（已列入待后端确认清单），暂定 1=图文 2=视频
const FEED_TYPE_IMAGE = 1;
const FEED_TYPE_VIDEO = 2;

interface MediaItem {
  raw: string; // 提交用：file_url（未签名）
  signed: string; // 展示用：已签名 URL（私有桶可直接访问）
  kind: 'image' | 'video';
}

export default function PublishPage() {
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [media, setMedia] = useState<MediaItem[]>([]);
  const [cover, setCover] = useState<{ raw: string; signed: string } | null>(null);
  const [isVideo, setIsVideo] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [progress, setProgress] = useState(0);
  const [submitting, setSubmitting] = useState(false);
  const navigate = useNavigate();

  // 上传单个文件并拿回「提交用 raw 地址 + 展示用签名地址」。
  // biz 决定 COS 业务目录：image/video 存媒体，cover 存封面（视频帖必填）。
  const uploadOne = async (file: File, biz: 'image' | 'video' | 'cover') => {
    const r = await uploadFile(file, biz, setProgress);
    const signed = await getSignedUrl(r.file_url);
    return { raw: r.file_url, signed };
  };

  const onPickMedia = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files || []);
    if (files.length === 0) return;
    setUploading(true);
    try {
      for (const file of files) {
        const isVideoFile = file.type.startsWith('video/');
        if (isVideoFile) setIsVideo(true);
        const item = await uploadOne(file, isVideoFile ? 'video' : 'image');
        setMedia((prev) => [
          ...prev,
          { raw: item.raw, signed: item.signed, kind: isVideoFile ? 'video' : 'image' },
        ]);
      }
      toast('上传成功', 'success');
    } catch {
      toast('上传失败，请重试');
    } finally {
      setUploading(false);
      setProgress(0);
      e.target.value = '';
    }
  };

  const onPickCover = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploading(true);
    try {
      const item = await uploadOne(file, 'cover');
      setCover(item);
      toast('封面上传成功', 'success');
    } catch {
      toast('封面上传失败，请重试');
    } finally {
      setUploading(false);
      setProgress(0);
      e.target.value = '';
    }
  };

  const onSubmit = async () => {
    if (media.length === 0) {
      toast('请先上传图片或视频');
      return;
    }
    if (!description.trim()) {
      // 后端 createFeed 强制要求 description，否则返回「帖子内容为空」
      toast('请填写描述');
      return;
    }
    // 视频帖：封面为必填且须在 cover 业务目录（biz=cover），否则后端返回「媒体资源为空」；
    // 图文帖：用首张图作为封面。
    const coverUrl = isVideo ? cover?.raw : media[0]?.raw;
    if (isVideo && !coverUrl) {
      toast('请上传视频封面');
      return;
    }
    setSubmitting(true);
    try {
      const resp = await createFeed({
        feed_type: isVideo ? FEED_TYPE_VIDEO : FEED_TYPE_IMAGE,
        title: title || undefined,
        description: description.trim(),
        media_urls: media.map((m) => m.raw),
        cover_url: coverUrl,
      });
      toast('发布成功', 'success');
      navigate(`/feeds/${resp.feed.id}`);
    } catch {
      // 拦截器已 toast
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="card" style={{ maxWidth: 560, margin: '24px auto' }}>
      <h2 style={{ fontSize: 18, marginBottom: 16 }}>发布帖子</h2>
      <div className="field" style={{ marginBottom: 12 }}>
        <input
          style={{ width: '100%', padding: '10px 12px', border: '1px solid #ddd', borderRadius: 8 }}
          placeholder="标题（选填）"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
      </div>
      <div className="field" style={{ marginBottom: 12 }}>
        <textarea
          style={{ width: '100%', padding: '10px 12px', border: '1px solid #ddd', borderRadius: 8, minHeight: 100, resize: 'vertical' }}
          placeholder="说点什么…（必填）"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </div>
      <div className="field" style={{ marginBottom: 12 }}>
        <div style={{ fontSize: 13, color: '#888', marginBottom: 6 }}>媒体（图片或视频）</div>
        <input type="file" accept="image/*,video/*" multiple onChange={onPickMedia} disabled={uploading} />
        {uploading && <div style={{ fontSize: 13, color: '#888', marginTop: 6 }}>上传中… {progress}%</div>}
        <div className="upload-preview">
          {media.map((m) =>
            m.kind === 'video' ? (
              <video key={m.signed} src={m.signed} controls preload="metadata" style={{ maxWidth: '100%', borderRadius: 8 }} />
            ) : (
              <img key={m.signed} src={m.signed} alt="" />
            ),
          )}
        </div>
      </div>

      {isVideo && (
        <div className="field" style={{ marginBottom: 12 }}>
          <div style={{ fontSize: 13, color: '#888', marginBottom: 6 }}>视频封面（必填，图片）</div>
          <input type="file" accept="image/*" onChange={onPickCover} disabled={uploading} />
          {cover && <img src={cover.signed} alt="封面预览" style={{ maxWidth: 160, borderRadius: 8, marginTop: 6 }} />}
        </div>
      )}

      <button className="btn block" onClick={onSubmit} disabled={submitting || uploading}>
        {submitting ? '发布中…' : '发布'}
      </button>
    </div>
  );
}
