import client from './client';

// 线路测速 API 客户端（多节点）。字段镜像 Go speedtest（snake_case）。

export interface SpeedtestServer {
  id: string;
  name: string; // 城市(国家)
  sponsor: string; // 运营商
  distance_km: number;
  ping_ms: number; // -1=Timeout
  reachable: boolean;
  recommended: boolean;
}

export type NodeStatus = 'pending' | 'testing' | 'done' | 'failed';

export interface SpeedtestNode {
  id: string;
  name: string;
  sponsor: string;
  distance_km: number;
  status: NodeStatus;
  download_mbps: number;
  upload_mbps: number;
  ping_ms: number;
  jitter_ms: number;
  error?: string;
}

export type SpeedtestPhase = 'idle' | 'starting' | 'installing' | 'listing' | 'testing' | 'done' | 'error';

export interface SpeedtestStatus {
  phase: SpeedtestPhase;
  running: boolean;
  message?: string;
  isp?: string;
  error?: string;
  started_at?: string;
  finished_at?: string;
  nodes: SpeedtestNode[];
}

export interface SpeedtestSvcInfo {
  installed: boolean;
  can_install: boolean;
  pkg_manager: string;
}

export interface SpeedtestServersResp {
  installed: boolean;
  isp: string;
  items: SpeedtestServer[];
}

export interface SpeedtestHistoryEntry {
  time: string;
  best_node: string;
  best_download_mbps: number;
  best_upload_mbps: number;
  min_ping_ms: number;
  nodes: SpeedtestNode[];
}

export async function getSpeedtestStatus(): Promise<SpeedtestStatus> {
  const { data } = await client.get('/api/v1/speedtest/status');
  return data;
}
export async function getSpeedtestServers(): Promise<SpeedtestServersResp> {
  const { data } = await client.get('/api/v1/speedtest/servers');
  return data;
}
export async function runSpeedtest(serverIds: string[]): Promise<SpeedtestStatus> {
  const { data } = await client.post('/api/v1/speedtest/run', { server_ids: serverIds });
  return data;
}
export async function getSpeedtestService(): Promise<SpeedtestSvcInfo> {
  const { data } = await client.get('/api/v1/speedtest/service');
  return data;
}
export async function installSpeedtest(): Promise<{ output: string }> {
  const { data } = await client.post('/api/v1/speedtest/install');
  return data;
}
export async function getSpeedtestHistory(): Promise<SpeedtestHistoryEntry[]> {
  const { data } = await client.get('/api/v1/speedtest/history');
  return data.items ?? [];
}
export async function clearSpeedtestHistory(): Promise<void> {
  await client.post('/api/v1/speedtest/history/clear');
}
