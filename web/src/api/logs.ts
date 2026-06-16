import client, { getAPIToken } from './client';

// 日志中心 API 客户端。字段镜像 Go logcenter.Entry（snake_case）。

export type LogSource = 'system' | 'dhcp' | 'dialup' | 'ddns' | 'operation' | 'arp';

export interface LogEntry {
  time: string;
  ts: number;
  level?: string;
  proc?: string;
  message?: string;
  type?: string;
  iface?: string;
  mac?: string;
  ip?: string;
  user?: string;
  client_ip?: string;
  module?: string;
  action?: string;
}

export interface LogQuery {
  start?: number; // unix 秒
  end?: number;
  keyword?: string;
  page?: number; // 1-based
  page_size?: number;
}

export interface LogResult {
  items: LogEntry[];
  total: number;
}

export async function queryLogs(source: LogSource, q: LogQuery): Promise<LogResult> {
  const { data } = await client.get(`/api/v1/logs/${source}`, { params: q });
  return { items: data.items ?? [], total: data.total ?? 0 };
}

export async function clearLogs(source: LogSource): Promise<void> {
  await client.post(`/api/v1/logs/${source}/clear`);
}

// 导出：带鉴权直接下载为文本文件。
export function exportLogsURL(source: LogSource, q: LogQuery): string {
  const p = new URLSearchParams();
  if (q.start) p.set('start', String(q.start));
  if (q.end) p.set('end', String(q.end));
  if (q.keyword) p.set('keyword', q.keyword);
  return `/api/v1/logs/${source}/export?${p.toString()}`;
}

export async function downloadLogs(source: LogSource, q: LogQuery): Promise<void> {
  const resp = await client.get(exportLogsURL(source, q), { responseType: 'blob' });
  void getAPIToken; // client 拦截器已加鉴权头
  const url = window.URL.createObjectURL(resp.data as Blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `${source}-log.txt`;
  a.click();
  window.URL.revokeObjectURL(url);
}
