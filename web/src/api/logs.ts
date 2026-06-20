import client, { getAPIToken } from './client';

// 日志中心 API 客户端。字段镜像 Go logcenter.Entry（snake_case）。

export type LogSource = 'system' | 'dhcp' | 'dialup' | 'ddns' | 'operation' | 'arp';

export type DialPhase = 'discovery' | 'auth' | 'ipcp' | 'established' | 'teardown' | 'other';
export type DialSeverity = 'info' | 'success' | 'warning' | 'error';

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
  // 拨号诊断（仅 dialup 源 / 实时拨号流）
  phase?: DialPhase;
  dial_state?: 'connecting' | 'connected' | 'failed';
  severity?: DialSeverity;
  diagnosis?: string;
  advice?: string;
  seq?: number;
}

// 拨号结论横幅（GET /api/v1/logs/dialup/diagnose）。
export interface DialDiagnosis {
  iface?: string;
  dial_state: 'connecting' | 'connected' | 'failed' | 'unknown';
  phase?: string;
  severity?: DialSeverity;
  headline: string;
  diagnosis?: string;
  advice?: string;
  matched_line?: string;
  updated_at?: string;
}

// 实时拨号日志 WS 帧。
export interface DialLogFrame {
  seq: number;
  type: string; // "dial.log"
  ts: string;
  data: LogEntry;
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

// 拨号诊断结论（打开拨号日志时先取一次，给横幅一个即时结论）。
export async function dialDiagnose(iface?: string): Promise<DialDiagnosis> {
  const { data } = await client.get('/api/v1/logs/dialup/diagnose', {
    params: iface ? { iface } : {},
  });
  return data as DialDiagnosis;
}

// 构造拨号实时日志 WebSocket URL（token 走 query，浏览器 WS 无法设 header）。
export function dialStreamWsURL(iface?: string, replay = 80): string {
  const token = getAPIToken();
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const p = new URLSearchParams();
  if (token) p.set('token', token);
  if (iface) p.set('iface', iface);
  if (replay > 0) p.set('replay', String(replay));
  return `${proto}//${window.location.host}/api/v1/logs/dialup/stream?${p.toString()}`;
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
