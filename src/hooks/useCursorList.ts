import { useCallback, useEffect, useRef, useState } from 'react';

// Cursor 分页响应通用形状（对应 *.api：list / next_cursor / has_more）
export interface CursorResp<T> {
  list: T[];
  next_cursor: string;
  has_more: boolean;
}

export type CursorFetcher<T> = (cursor: string) => Promise<CursorResp<T>>;

/**
 * cursor 无限滚动通用 hook。
 * - fetcher(cursor)：cursor 为空串表示第一页
 * - loadMore：由滚动哨兵或按钮触发
 * - reload：重置并拉第一页（fetcher 变更时自动 reload）
 *
 * 注意：首屏内容不足一屏时，哨兵在挂载时即处于视口内，IntersectionObserver
 * 的首次回调常被首屏 reload 的 loading 守卫吞掉且不再触发，导致停在第一页。
 * 因此每次加载完成后若仍有更多且哨兵仍在视口内，会主动续拉以填满首屏。
 */
export function useCursorList<T>(fetcher: CursorFetcher<T>) {
  const [items, setItems] = useState<T[]>([]);
  const [hasMore, setHasMore] = useState(true);
  const [loading, setLoading] = useState(false);
  const cursorRef = useRef('');
  const loadingRef = useRef(false);
  const sentinelRef = useRef<HTMLDivElement | null>(null);

  const load = useCallback(
    async (reset: boolean) => {
      if (loadingRef.current) return;
      loadingRef.current = true;
      setLoading(true);
      try {
        const cursor = reset ? '' : cursorRef.current;
        const resp = await fetcher(cursor);
        cursorRef.current = resp.next_cursor || '';
        const more = Boolean(resp.has_more);
        setHasMore(more);
        const list = resp.list || [];
        setItems((prev) => (reset ? list : [...prev, ...list]));
        // 首屏不足一屏 / 当前视口内仍有哨兵：自动续拉直到填满或到底
        if (more && list.length > 0) {
          requestAnimationFrame(() => {
            const el = sentinelRef.current;
            if (el && el.getBoundingClientRect().top <= window.innerHeight) {
              void load(false);
            }
          });
        }
      } catch {
        // 错误提示已由 request 拦截器统一 toast
      } finally {
        loadingRef.current = false;
        setLoading(false);
      }
    },
    [fetcher],
  );

  const loadMore = useCallback(() => {
    if (hasMore) void load(false);
  }, [hasMore, load]);

  const reload = useCallback(() => {
    cursorRef.current = '';
    setHasMore(true);
    void load(true);
  }, [load]);

  useEffect(() => {
    reload();
  }, [reload]);

  // 滚动哨兵：将返回的 ref 挂到列表底部元素即可自动加载下一页
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries[0].isIntersecting) loadMore();
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [loadMore]);

  return { items, setItems, hasMore, loading, loadMore, reload, sentinelRef };
}
