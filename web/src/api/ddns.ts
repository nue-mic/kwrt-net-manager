import client from './client';
import type { BatchAction } from './netcfg';

// 动态域名 DDNS API 客户端。字段镜像 Go ddns.Entry（snake_case）。

export interface DDNSEntry {
  id: string;
  provider: string;
  domain: string;
  auth_mode: 'token' | 'userpass';
  username: string;
  password: string;
  ip_source: 'web' | 'network' | 'device'; // 出口IP | 接口IP | 按终端 MAC 解析
  interface: string;
  mac: string; // 仅 ip_source=device：目标 LAN 终端 MAC
  record_type: 'A' | 'AAAA';
  enabled: boolean;
  remark: string;
  // 只读运行态
  last_result?: string;
  current_ip?: string;
  last_update?: string;
}

export interface DDNSSvcInfo {
  installed: boolean;
  can_install: boolean;
  pkg_manager: string;
  providers: string[];
}

// 候选 LAN 终端（按终端解析时选目标设备）。
export interface DDNSDevice {
  mac: string;
  hostname?: string;
  ipv6?: string; // 当前解析到的稳定 GUA，可空
  source?: 'dhcpv6' | 'slaac' | 'neighbor';
  vendor?: string; // OUI 厂商识别
}

export type DDNSInput = Omit<DDNSEntry, 'id' | 'last_result' | 'current_ip' | 'last_update'>;

export async function listDDNS(): Promise<DDNSEntry[]> {
  const { data } = await client.get('/api/v1/ddns');
  return data.items ?? [];
}
export async function createDDNS(body: DDNSInput): Promise<DDNSEntry> {
  const { data } = await client.post('/api/v1/ddns', body);
  return data;
}
export async function updateDDNS(id: string, body: DDNSInput): Promise<DDNSEntry> {
  const { data } = await client.put(`/api/v1/ddns/${id}`, body);
  return data;
}
export async function deleteDDNS(id: string): Promise<void> {
  await client.delete(`/api/v1/ddns/${id}`);
}
export async function toggleDDNS(id: string, enabled: boolean): Promise<void> {
  await client.post(`/api/v1/ddns/${id}/toggle`, { enabled });
}
export async function batchDDNS(action: BatchAction, ids: string[]): Promise<void> {
  await client.post('/api/v1/ddns/batch', { action, ids });
}
export async function getDDNSService(): Promise<DDNSSvcInfo> {
  const { data } = await client.get('/api/v1/ddns/service');
  return data;
}
export async function listDDNSDevices(): Promise<DDNSDevice[]> {
  const { data } = await client.get('/api/v1/ddns/devices');
  return data.items ?? [];
}
export async function installDDNS(): Promise<{ output: string }> {
  const { data } = await client.post('/api/v1/ddns/install');
  return data;
}
