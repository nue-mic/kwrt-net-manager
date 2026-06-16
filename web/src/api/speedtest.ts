import client from './client';

// 线路测速 API 客户端。字段镜像 Go speedtest（snake_case）。

export interface SpeedtestResult {
  download_mbps: number;
  upload_mbps: number;
  ping_ms: number;
  server: string;
  isp: string;
}

export interface SpeedtestStatus {
  running: boolean;
  result?: SpeedtestResult;
  error?: string;
  started_at?: string;
  finished_at?: string;
}

export interface SpeedtestSvcInfo {
  installed: boolean;
  can_install: boolean;
  pkg_manager: string;
}

export async function getSpeedtestStatus(): Promise<SpeedtestStatus> {
  const { data } = await client.get('/api/v1/speedtest/status');
  return data;
}
export async function runSpeedtest(): Promise<SpeedtestStatus> {
  const { data } = await client.post('/api/v1/speedtest/run');
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
