import client from './client';
import type { BatchAction } from './netcfg';

// 字段逐一镜像 Go struct（snake_case）。

export interface DNSSettings {
  enabled: boolean;
  dns_primary: string;
  dns_secondary: string;
  no_resolv: boolean;
  filter_aaaa: boolean;
  cache_size: number;
  local_ttl: number;
  min_cache_ttl: number;
  max_cache_ttl: number;
  force_proxy: boolean;
  dnssec: boolean;
  rebind_protection: boolean;
  all_servers: boolean;
}

export interface DNSDoH {
  enabled: boolean;
  resolver_url: string;
  listen_port: number;
  bootstrap_dns: string;
}

export interface DNSRecord {
  id: string;
  domain: string;
  record_type: 'A' | 'AAAA';
  address: string;
  wildcard: boolean;
  src_ip_scope: string;
  remark: string;
  enabled: boolean;
}

export interface DNSDomainRoute {
  id: string;
  domain: string;
  server: string;
  out_iface: string;
  remark: string;
  enabled: boolean;
}

export interface DNSCacheStats {
  supported: boolean;
  cache_size: number;
  insertions: number;
  evictions: number;
  hits: number;
  misses: number;
  hit_ratio: number;
}

export interface DNSSvcInfo {
  backend: 'uci' | 'store';
  filter_aaaa_supported: boolean;
  doh_installed: boolean;
  pkg_manager: string;
  can_install: boolean;
}

export type DNSRecordInput = Omit<DNSRecord, 'id'>;
export type DNSDomainRouteInput = Omit<DNSDomainRoute, 'id'>;

// ---------- DNS 设置 / DoH ----------

export async function getDNSSettings(): Promise<DNSSettings> {
  const { data } = await client.get('/api/v1/dns/settings');
  return data;
}
export async function saveDNSSettings(body: DNSSettings): Promise<DNSSettings> {
  const { data } = await client.put('/api/v1/dns/settings', body);
  return data;
}
export async function getDNSDoH(): Promise<DNSDoH> {
  const { data } = await client.get('/api/v1/dns/doh');
  return data;
}
export async function saveDNSDoH(body: DNSDoH): Promise<DNSDoH> {
  const { data } = await client.put('/api/v1/dns/doh', body);
  return data;
}
export async function installDoH(): Promise<{ output: string }> {
  const { data } = await client.post('/api/v1/dns/doh/install');
  return data;
}
export async function getDNSService(): Promise<DNSSvcInfo> {
  const { data } = await client.get('/api/v1/dns/service');
  return data;
}

// ---------- 自定义解析记录 ----------

export async function listDNSRecords(): Promise<DNSRecord[]> {
  const { data } = await client.get('/api/v1/dns/records');
  return data.items ?? [];
}
export async function createDNSRecord(body: DNSRecordInput): Promise<DNSRecord> {
  const { data } = await client.post('/api/v1/dns/records', body);
  return data;
}
export async function updateDNSRecord(id: string, body: DNSRecordInput): Promise<DNSRecord> {
  const { data } = await client.put(`/api/v1/dns/records/${id}`, body);
  return data;
}
export async function deleteDNSRecord(id: string): Promise<void> {
  await client.delete(`/api/v1/dns/records/${id}`);
}
export async function toggleDNSRecord(id: string, enabled: boolean): Promise<void> {
  await client.post(`/api/v1/dns/records/${id}/toggle`, { enabled });
}
export async function batchDNSRecords(action: BatchAction, ids: string[]): Promise<void> {
  await client.post('/api/v1/dns/records/batch', { action, ids });
}

// ---------- 域名分流 DNS ----------

export async function listDNSDomainRoutes(): Promise<DNSDomainRoute[]> {
  const { data } = await client.get('/api/v1/dns/domain-routes');
  return data.items ?? [];
}
export async function createDNSDomainRoute(body: DNSDomainRouteInput): Promise<DNSDomainRoute> {
  const { data } = await client.post('/api/v1/dns/domain-routes', body);
  return data;
}
export async function updateDNSDomainRoute(id: string, body: DNSDomainRouteInput): Promise<DNSDomainRoute> {
  const { data } = await client.put(`/api/v1/dns/domain-routes/${id}`, body);
  return data;
}
export async function deleteDNSDomainRoute(id: string): Promise<void> {
  await client.delete(`/api/v1/dns/domain-routes/${id}`);
}
export async function toggleDNSDomainRoute(id: string, enabled: boolean): Promise<void> {
  await client.post(`/api/v1/dns/domain-routes/${id}/toggle`, { enabled });
}
export async function batchDNSDomainRoutes(action: BatchAction, ids: string[]): Promise<void> {
  await client.post('/api/v1/dns/domain-routes/batch', { action, ids });
}

// ---------- 缓存状态 ----------

export async function getDNSCacheStats(): Promise<DNSCacheStats> {
  const { data } = await client.get('/api/v1/dns/cache-stats');
  return data;
}
export async function flushDNSCache(): Promise<void> {
  await client.post('/api/v1/dns/cache/flush');
}
