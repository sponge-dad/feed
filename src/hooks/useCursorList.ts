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
 */
export function useCursorList<T>(fetcher: CursorFetcher<T>) {
  const [items, setItems] = useState<T[]>([]);
  const [hasMore, setHasMore] = useState(true);
  const [loading, setLoading] = useState(false);
  const cursorRef = useRef('');
  const loadingRef = useRef(false);

  const load = useCallback(
    async (reset: boolean) => {
      if (loadingRef.current) return;
      loadingRef.current = true;
      setLoading(true);
      try {
        const cursor = reset ? '' : cursorRef.current;
        const resp = await fetcher(cursor);
        cursorRef.current = resp.next_cursor || '';
        setHasMore(Boolean(resp.has_more));
        setItems((prev) => (reset ? resp.list || [] : [...prev, ...(resp.list || [])]));
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
  const sentinelRef = useRef<HTMLDivElement | null>(null);
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
