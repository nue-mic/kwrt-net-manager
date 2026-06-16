// IPv6 API 客户端（爱快 IPv6 菜单全套）。
// 所有类型逐字段对齐后端 Go struct（internal/netcfg/ipv6_types.go），全部 snake_case。
// 后端 decodeJSON 启用 DisallowUnknownFields：请求体多一个 key 会 400，务必字段一致。
import client from './client';
import type { BatchAction } from './netcfg';

// ---------- 类型 ----------

export type IPv6Proto = 'dhcpv6' | 'static6' | '6in4' | '6to4' | '6rd';
export type DHCPv6Mode = 'stateless' | 'stateful' | 'stateful_only';
export type ConfigType = 'auto' | 'static';
export type ACLv6Method = 'duid' | 'l2mac';

export interface WANv6 {
  id: string;
  name: string;
  wan_iface: string;
  device: string;
  proto: IPv6Proto;
  enabled: boolean;
  req_prefix: string;
  fixed_prefix: string;
  force_prefix: boolean;
  client_id: string;
  no_release: boolean;
  peer_dns: boolean;
  dns_primary: string;
  dns_secondary: string;
  static_ip6: string;
  static_gateway: string;
  peer_addr: string;
  tun_prefix: string;
  mtu: number;
  remark: string;
  // 只读运行态
  ip6_address: string;
  ip6_gateway: string;
  ip6_prefix: string;
  local_link: string;
  up: boolean;
  managed?: boolean;
}
export type WANv6Input = Omit<WANv6, 'ip6_address' | 'ip6_gateway' | 'ip6_prefix' | 'local_link' | 'up' | 'managed'>;

export interface LANv6 {
  id: string;
  interface: string;
  config_type: ConfigType;
  bind_wan: string;
  prefix_assign_len: number;
  prefix_hint: string;
  static_ip6: string;
  dhcpv6_enabled: boolean;
  dhcpv6_mode: DHCPv6Mode;
  ipv6_dns_enabled: boolean;
  dns_servers: string[];
  lease_minutes: number;
  ra_mtu_enabled: boolean;
  ra_mtu: number;
  enabled: boolean;
  remark: string;
  // 只读运行态
  ip6_address: string;
  local_link: string;
  managed?: boolean;
}
export type LANv6Input = Omit<LANv6, 'ip6_address' | 'local_link' | 'managed'>;

export interface LeaseV6 {
  hostname: string;
  mac: string;
  local_link: string;
  ipv6_addr: string;
  duid: string;
  iaid: string;
  interface: string;
  valid_seconds: number;
  static: boolean;
  remark: string;
}

export interface PrefixStaticV6 {
  id: string;
  local_link: string;
  lan_interface: string;
  wan_line: string;
  duid: string;
  host_id: string;
  mac: string;
  remark: string;
  enabled: boolean;
  managed?: boolean;
}
export type PrefixStaticV6Input = Omit<PrefixStaticV6, 'id' | 'managed'>;

export interface ACLv6Entry {
  id: string;
  mac: string;
  duid: string;
  method: ACLv6Method;
  remark: string;
  enabled: boolean;
  managed?: boolean;
}
export type ACLv6EntryInput = Omit<ACLv6Entry, 'id' | 'managed'>;

export interface ACLv6 {
  mode: 'blacklist' | 'whitelist';
  entries: ACLv6Entry[];
}

export interface NeighborV6 {
  mac: string;
  ipv6: string;
  interface: string;
  state: string;
  router: boolean;
  remark: string;
}

export interface LineV6 {
  line: string;
  connections: number;
  up_bps: number;
  down_bps: number;
  total_up: number;
  total_down: number;
}

export interface DHCPv6SvcInfo {
  odhcpd_installed: boolean;
  odhcpd_running: boolean;
  ip_full: boolean;
  pkg_manager: string;
  lan_server_on: boolean;
}

// ---------- WANv6（外网） ----------

export async function listWANv6(): Promise<WANv6[]> {
  const { data } = await client.get('/api/v1/ipv6/wan');
  return data.items ?? [];
}
export async function getWANv6(id: string): Promise<WANv6> {
  const { data } = await client.get(`/api/v1/ipv6/wan/${id}`);
  return data;
}
export async function createWANv6(body: WANv6Input): Promise<WANv6> {
  const { data } = await client.post('/api/v1/ipv6/wan', body);
  return data;
}
export async function updateWANv6(id: string, body: WANv6Input): Promise<WANv6> {
  const { data } = await client.put(`/api/v1/ipv6/wan/${id}`, body);
  return data;
}
export async function deleteWANv6(id: string): Promise<void> {
  await client.delete(`/api/v1/ipv6/wan/${id}`);
}
export async function toggleWANv6(id: string, enabled: boolean): Promise<void> {
  await client.post(`/api/v1/ipv6/wan/${id}/toggle`, { enabled });
}
export async function batchWANv6(action: BatchAction, ids: string[]): Promise<void> {
  await client.post('/api/v1/ipv6/wan/batch', { action, ids });
}
export async function regenWANv6DUID(id: string): Promise<WANv6> {
  const { data } = await client.post(`/api/v1/ipv6/wan/${id}/duid`);
  return data;
}
export async function transitionPkg(proto: string): Promise<{ installed: boolean; pkg: string }> {
  const { data } = await client.get('/api/v1/ipv6/transition-pkg', { params: { proto } });
  return data;
}

// ---------- LANv6（内网） ----------

export async function listLANv6(): Promise<LANv6[]> {
  const { data } = await client.get('/api/v1/ipv6/lan');
  return data.items ?? [];
}
export async function getLANv6(id: string): Promise<LANv6> {
  const { data } = await client.get(`/api/v1/ipv6/lan/${id}`);
  return data;
}
export async function createLANv6(body: LANv6Input): Promise<LANv6> {
  const { data } = await client.post('/api/v1/ipv6/lan', body);
  return data;
}
export async function updateLANv6(id: string, body: LANv6Input): Promise<LANv6> {
  const { data } = await client.put(`/api/v1/ipv6/lan/${id}`, body);
  return data;
}
export async function deleteLANv6(id: string): Promise<void> {
  await client.delete(`/api/v1/ipv6/lan/${id}`);
}
export async function toggleLANv6(id: string, enabled: boolean): Promise<void> {
  await client.post(`/api/v1/ipv6/lan/${id}/toggle`, { enabled });
}
export async function batchLANv6(action: BatchAction, ids: string[]): Promise<void> {
  await client.post('/api/v1/ipv6/lan/batch', { action, ids });
}

// ---------- DHCPv6 终端 ----------

export async function listLeasesV6(query: { interface?: string; query?: string } = {}): Promise<LeaseV6[]> {
  const { data } = await client.get('/api/v1/ipv6/leases', { params: query });
  return data.items ?? [];
}

// ---------- 前缀静态分配 ----------

export async function listPrefixStaticsV6(): Promise<PrefixStaticV6[]> {
  const { data } = await client.get('/api/v1/ipv6/prefix-static');
  return data.items ?? [];
}
export async function createPrefixStaticV6(body: PrefixStaticV6Input): Promise<PrefixStaticV6> {
  const { data } = await client.post('/api/v1/ipv6/prefix-static', body);
  return data;
}
export async function updatePrefixStaticV6(id: string, body: PrefixStaticV6Input): Promise<PrefixStaticV6> {
  const { data } = await client.put(`/api/v1/ipv6/prefix-static/${id}`, body);
  return data;
}
export async function deletePrefixStaticV6(id: string): Promise<void> {
  await client.delete(`/api/v1/ipv6/prefix-static/${id}`);
}
export async function togglePrefixStaticV6(id: string, enabled: boolean): Promise<void> {
  await client.post(`/api/v1/ipv6/prefix-static/${id}/toggle`, { enabled });
}
export async function batchPrefixStaticsV6(action: BatchAction, ids: string[]): Promise<void> {
  await client.post('/api/v1/ipv6/prefix-static/batch', { action, ids });
}

// ---------- DHCPv6 接入控制（黑白名单） ----------

export async function getACLv6(): Promise<ACLv6> {
  const { data } = await client.get('/api/v1/ipv6/acl');
  return data;
}
export async function setACLv6Mode(mode: 'blacklist' | 'whitelist'): Promise<ACLv6> {
  const { data } = await client.put('/api/v1/ipv6/acl/mode', { mode });
  return data;
}
export async function addACLv6Entry(body: ACLv6EntryInput): Promise<ACLv6Entry> {
  const { data } = await client.post('/api/v1/ipv6/acl/entries', body);
  return data;
}
export async function updateACLv6Entry(id: string, body: ACLv6EntryInput): Promise<ACLv6Entry> {
  const { data } = await client.put(`/api/v1/ipv6/acl/entries/${id}`, body);
  return data;
}
export async function deleteACLv6Entry(id: string): Promise<void> {
  await client.delete(`/api/v1/ipv6/acl/entries/${id}`);
}
export async function toggleACLv6Entry(id: string): Promise<ACLv6Entry> {
  const { data } = await client.post(`/api/v1/ipv6/acl/entries/${id}/toggle`);
  return data;
}

// ---------- 邻居列表 / 线路详情 / 服务信息 ----------

export async function listNeighborsV6(): Promise<NeighborV6[]> {
  const { data } = await client.get('/api/v1/ipv6/neighbors');
  return data.items ?? [];
}
export async function deleteNeighborV6(addr: string, dev: string): Promise<void> {
  await client.delete('/api/v1/ipv6/neighbors', { data: { addr, dev } });
}
export async function flushNeighborsV6(dev = ''): Promise<void> {
  await client.post('/api/v1/ipv6/neighbors/flush', { dev });
}
export async function listLinesV6(): Promise<LineV6[]> {
  const { data } = await client.get('/api/v1/ipv6/lines');
  return data.items ?? [];
}
export async function getDHCPv6Service(): Promise<DHCPv6SvcInfo> {
  const { data } = await client.get('/api/v1/ipv6/service');
  return data;
}
