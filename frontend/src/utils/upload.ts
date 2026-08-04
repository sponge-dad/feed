import COS from 'cos-js-sdk-v5';
import { getUploadToken } from '@/api/user';

export interface UploadResult {
  file_key: string;
  file_url: string;
}

export type UploadBiz = 'avatar' | 'cover' | 'image' | 'video';

function extOf(file: File): string {
  const idx = file.name.lastIndexOf('.');
  return idx >= 0 ? file.name.slice(idx + 1).toLowerCase() : '';
}

// upload_url 形如 https://{bucket}.cos.{region}.myqcloud.com/{file_key}
function parseCosTarget(uploadUrl: string): { Bucket: string; Region: string } {
  const u = new URL(uploadUrl);
  const m = u.hostname.match(/^(.+)\.cos\.(.+)\.myqcloud\.com$/);
  if (!m) throw new Error('无法解析 COS 上传地址');
  return { Bucket: m[1], Region: m[2] };
}

/**
 * 按后端 STS 设计直传 COS：
 * 1. 先拿后端签发的临时凭证（已用 CAM policy 限定到当前用户目录的 PutObject）；
 * 2. 用 cos-js-sdk-v5 + 该临时密钥签名并 PUT 对象；
 * 3. 返回后端生成的 file_key / file_url，供业务接口（如 updateMe）保存。
 */
export async function uploadFile(
  file: File,
  biz: UploadBiz = 'image',
  onProgress?: (percent: number) => void,
): Promise<UploadResult> {
  const token = await getUploadToken({ file_type: biz, file_ext: extOf(file) });

  const { Bucket, Region } = parseCosTarget(token.upload_url);
  const { credentials } = token;

  const cos = new COS({
    getAuthorization: (_options, callback) => {
      callback({
        TmpSecretId: credentials.tmp_secret_id,
        TmpSecretKey: credentials.tmp_secret_key,
        SecurityToken: credentials.session_token,
        StartTime: Math.floor(Date.now() / 1000),
        ExpiredTime: credentials.expired_time,
      });
    },
  });

  await new Promise<void>((resolve, reject) => {
    cos.putObject(
      {
        Bucket,
        Region,
        Key: token.file_key,
        Body: file,
        onProgress: (info: { percent?: number }) => {
          if (onProgress && typeof info.percent === 'number') {
            onProgress(Math.round(info.percent * 100));
          }
        },
      },
      (err) => {
        if (err) reject(err);
        else resolve();
      },
    );
  });

  return { file_key: token.file_key, file_url: token.file_url };
}
