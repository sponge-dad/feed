import type { CSSProperties } from 'react';

interface SkeletonProps {
  variant?: 'card' | 'line' | 'circle';
  width?: number | string;
  height?: number | string;
  className?: string;
  style?: CSSProperties;
}

/**
 * 加载占位骨架屏组件。
 * - card:   瀑布流卡片占位（封面 + 两行文字 + 作者行），复用 .waterfall-card 外壳
 * - line:   文本/区块占位条
 * - circle: 圆形占位（头像）
 */
export default function Skeleton({ variant = 'line', width, height, className = '', style }: SkeletonProps) {
  if (variant === 'card') {
    return (
      <div className={`skeleton-card ${className}`} style={style} aria-hidden="true">
        <div className="skeleton skeleton-cover" />
        <div className="skeleton-card-body">
          <div className="skeleton" style={{ height: 12, width: '92%', marginBottom: 8 }} />
          <div className="skeleton" style={{ height: 12, width: '55%' }} />
          <div className="skeleton-card-footer">
            <div className="skeleton skeleton-avatar" />
            <div className="skeleton" style={{ height: 10, width: 72 }} />
          </div>
        </div>
      </div>
    );
  }

  const dims: CSSProperties = {
    display: variant === 'circle' ? 'inline-block' : 'block',
    width,
    height,
    ...style,
  };
  if (variant === 'circle') dims.borderRadius = '999px';

  return <span className={`skeleton ${className}`} style={dims} aria-hidden="true" />;
}
