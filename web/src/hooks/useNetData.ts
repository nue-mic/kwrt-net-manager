import { useCallback, useEffect, useRef, useState } from 'react';
import { useEventStream } from '../events/EventStreamContext';

/**
 * 通用列表加载 hook：挂载时加载一次，并在收到任意 WS 事件（lastSeq 变化）时
 * 自动重载，使「增删改启停」后列表即时刷新（后端 netcfg 变更会广播事件）。
 *
 * 返回 { data, loading, error, reload }。
 */
export function useNetData<T>(loader: () => Promise<T>, initial: T) {
  const [data, setData] = useState<T>(initial);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>('');
  const { lastSeq } = useEventStream();
  const loaderRef = useRef(loader);
  loaderRef.current = loader;

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const d = await loaderRef.current();
      setData(d);
      setError('');
    } catch (e) {
      setError(extractErr(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [lastSeq, reload]);

  return { data, loading, error, reload };
}

/** 从 axios 错误里抽出后端 message。 */
export function extractErr(e: unknown): string {
  const anyErr = e as { response?: { data?: { error?: { message?: string } } }; message?: string };
  return anyErr?.response?.data?.error?.message || anyErr?.message || '请求失败';
}
