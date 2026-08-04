import { getSignUrl } from '@/api/user';

// 把私有 COS 地址（file_url 或 file_key）换算成临时可访问 URL。
// 后端 /upload/sign-url 要求传入 file_key；若传入的是完整 file_url，
// 则取 pathname 作为 file_key。结果按 file_key 缓存，避免重复签名。
const cache = new Map<string, string>();

export async function getSignedUrl(raw: string): Promise<string> {
  // 已是签名地址，直接复用
  if (raw.includes('?sign=') || raw.includes('&sign=')) return raw;

  let fileKey = raw;
  try {
    const u = new URL(raw);
    if (u.hostname.includes('myqcloud.com')) {
      fileKey = decodeURIComponent(u.pathname.replace(/^\//, ''));
    }
  } catch {
    // 不是合法 URL，按 file_key 原样处理
  }

  const cached = cache.get(fileKey);
  if (cached) return cached;

  const resp = await getSignUrl({ file_key: fileKey });
  cache.set(fileKey, resp.signed_url);
  return resp.signed_url;
}
