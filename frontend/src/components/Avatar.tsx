import { useEffect, useState } from 'react';
import { getSignedUrl } from '@/utils/signUrl';

interface AvatarProps {
  src?: string;
  size?: number;
  alt?: string;
}

export default function Avatar({ src, size = 32, alt = '' }: AvatarProps) {
  const [resolved, setResolved] = useState('');

  useEffect(() => {
    let cancelled = false;
    if (!src) {
      setResolved('');
      return;
    }
    // 仅对私有 COS 地址走签名；其它地址（如外链）直接使用
    if (src.includes('myqcloud.com')) {
      getSignedUrl(src)
        .then((u) => !cancelled && setResolved(u))
        .catch(() => !cancelled && setResolved(src));
    } else {
      setResolved(src);
    }
    return () => {
      cancelled = true;
    };
  }, [src]);

  if (!src) {
    return (
      <div
        className="avatar"
        style={{
          width: size,
          height: size,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: '#fff',
          fontSize: size * 0.42,
          background: '#bbb',
        }}
      >
        {alt ? alt.slice(0, 1).toUpperCase() : '?'}
      </div>
    );
  }

  return (
    <img
      className="avatar"
      style={{ width: size, height: size }}
      src={resolved || src}
      alt={alt}
    />
  );
}
