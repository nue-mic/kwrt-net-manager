// 网络配置 API 客户端（DHCP + 静态路由）。
// 所有类型逐字段对齐后端 Go 结构体（internal/netcfg/types.go），全部 snake_case。
// 后端 decodeJSON 启用 DisallowUnknownFields：请求体多一个 key 会 400，务必字段一致。
import client from './client';

// ---------- 类型 ----------

export interface CustomOption {
  code: number;
  value: string;
  type?: string;
}

export interface DHCPServer {
  id: string;
  interface: string;
  enabled: boolean;
  ip_start: string;
  ip_end: string;
  force: boolean;
  netmask: string;
  gateway: string;
  dns_primary: string;
  dns_secondary: string;
  lease_minutes: number;
  exclude: string[];
  custom_options: CustomOption[];
  remaining: number; // 只读
}

export interface StaticLease {
  id: string;
  hostname: string;
  ip: string;
  mac: string;
  gateway: string;
  interface: string;
  dns_primary: string;
  dns_secondary: string;
  remark: string;
  enabled: boolean;
  route_push: boolean;
}

export type RoutePushMode = 'off' | 'all' | 'tagged';

export interface Lease {
  hostname: string;
  ip: string;
  mac: string;
  expiry: number;
  remaining_seconds: number;
  interface: string;
  static: boolean;
  remark: string;
}

export interface ACLEntry {
  id: string;
  mac: string;
  remark: string;
  enabled: boolean;
}

export interface ACL {
  mode: 'blacklist' | 'whitelist';
  entries: ACLEntry[];
}

export interface Route {
  id: string;
  family: 'ipv4' | 'ipv6';
  interface: string;
  target: string;
  netmask: string;
  prefix: number;
  gateway: string;
  metric: number;
  remark: string;
  enabled: boolean;
  push_to_clients: boolean;
}

export interface RouteEntry {
  interface: string;
  target: string;
  netmask: string;
  gateway: string;
  metric: number;
}

export interface NetInterface {
  name: string;
  ipv4: string;
  netmask: string;
  prefix: number;
  up: boolean;
}

export interface NetStatus {
  backend: 'uci' | 'store';
  dhcp_ok: boolean;
  enabled_servers: number;
  pending: boolean;
  message: string;
}

export type BatchAction = 'enable' | 'disable' | 'delete';

// 创建/编辑表单使用的“无 id（无只读字段）”载荷类型。
export type DHCPServerInput = Omit<DHCPServer, 'id' | 'remaining'>;
export type StaticLeaseInput = Omit<StaticLease, 'id'>;
export type RouteInput = Omit<Route, 'id'>;
export type ACLEntryInput = Omit<ACLEntry, 'id'>;

// ---------- 公共 ----------

export async function getStatus(): Promise<NetStatus> {
  const { data } = await client.get('/api/v1/netcfg/status');
  return data;
}

export async function listInterfaces(): Promise<NetInterface[]> {
  const { data } = await client.get('/api/v1/interfaces');
  return data.items ?? [];
}

// ---------- DHCP 服务端 ----------

export async function listServers(): Promise<DHCPServer[]> {
  const { data } = await client.get('/api/v1/dhcp/servers');
  return data.items ?? [];
}
export async function createServer(body: DHCPServerInput): Promise<DHCPServer> {
  const { data } = await client.post('/api/v1/dhcp/servers', body);
  return data;
}
export async function updateServer(id: string, body: DHCPServerInput): Promise<DHCPServer> {
  const { data } = await client.put(`/api/v1/dhcp/servers/${id}`, body);
  return data;
}
export async function deleteServer(id: string): Promise<void> {
  await client.delete(`/api/v1/dhcp/servers/${id}`);
}
export async function toggleServer(id: string, enabled: boolean): Promise<void> {
  await client.post(`/api/v1/dhcp/servers/${id}/toggle`, { enabled });
}
export async function batchServers(action: BatchAction, ids: string[]): Promise<void> {
  await client.post('/api/v1/dhcp/servers/batch', { action, ids });
}
export async function restartDHCP(): Promise<void> {
  await client.post('/api/v1/dhcp/restart');
}
// 路由下发到客户端总开关（DHCP option 121/249）。GET 复用服务端列表里的 route_push_mode。
export async function getRoutePushMode(): Promise<RoutePushMode> {
  const { data } = await client.get('/api/v1/dhcp/servers');
  return (data.route_push_mode as RoutePushMode) ?? 'off';
}
export async function setRoutePushMode(mode: RoutePushMode): Promise<void> {
  await client.put('/api/v1/dhcp/route-push', { mode });
}

// ---------- DHCP 静态分配 ----------

export async function listStatics(): Promise<{ items: StaticLease[]; arp_bind: boolean }> {
  const { data } = await client.get('/api/v1/dhcp/statics');
  return { items: data.items ?? [], arp_bind: !!data.arp_bind };
}
export async function createStatic(body: StaticLeaseInput): Promise<StaticLease> {
  const { data } = await client.post('/api/v1/dhcp/statics', body);
  return data;
}
export async function updateStatic(id: string, body: StaticLeaseInput): Promise<StaticLease> {
  const { data } = await client.put(`/api/v1/dhcp/statics/${id}`, body);
  return data;
}
export async function deleteStatic(id: string): Promise<void> {
  await client.delete(`/api/v1/dhcp/statics/${id}`);
}
export async function toggleStatic(id: string, enabled: boolean): Promise<void> {
  await client.post(`/api/v1/dhcp/statics/${id}/toggle`, { enabled });
}
export async function batchStatics(action: BatchAction, ids: string[]): Promise<void> {
  await client.post('/api/v1/dhcp/statics/batch', { action, ids });
}
export async function setARPBind(enabled: boolean): Promise<void> {
  await client.put('/api/v1/dhcp/statics/arp-bind', { enabled });
}

// ---------- DHCP 终端列表 ----------

export interface LeaseQuery {
  interface?: string;
  status?: 'static' | 'dynamic' | '';
  q?: string;
}
export async function listLeases(query: LeaseQuery = {}): Promise<Lease[]> {
  const { data } = await client.get('/api/v1/dhcp/leases', { params: query });
  return data.items ?? [];
}
export async function reserveLease(body: { ip: string; mac: string; hostname: string; interface: string }): Promise<StaticLease> {
  const { data } = await client.post('/api/v1/dhcp/leases/reserve', body);
  return data;
}
export async function blacklistLease(body: { mac: string; remark: string }): Promise<ACLEntry> {
  const { data } = await client.post('/api/v1/dhcp/leases/blacklist', body);
  return data;
}
export async function fixSubnet(iface: string): Promise<number> {
  const { data } = await client.post('/api/v1/dhcp/leases/fix-subnet', { interface: iface });
  return data.added ?? 0;
}

// ---------- DHCP 黑白名单 ----------

export async function getACL(): Promise<ACL> {
  const { data } = await client.get('/api/v1/dhcp/acl');
  return data;
}
export async function setACLMode(mode: 'blacklist' | 'whitelist'): Promise<ACL> {
  const { data } = await client.put('/api/v1/dhcp/acl/mode', { mode });
  return data;
}
export async function addACLEntry(body: ACLEntryInput): Promise<ACLEntry> {
  const { data } = await client.post('/api/v1/dhcp/acl/entries', body);
  return data;
}
export async function updateACLEntry(id: string, body: ACLEntryInput): Promise<ACLEntry> {
  const { data } = await client.put(`/api/v1/dhcp/acl/entries/${id}`, body);
  return data;
}
export async function deleteACLEntry(id: string): Promise<void> {
  await client.delete(`/api/v1/dhcp/acl/entries/${id}`);
}
export async function toggleACLEntry(id: string): Promise<ACLEntry> {
  const { data } = await client.post(`/api/v1/dhcp/acl/entries/${id}/toggle`);
  return data;
}

// ---------- 静态路由 ----------

export async function listRoutes(): Promise<Route[]> {
  const { data } = await client.get('/api/v1/routes');
  return data.items ?? [];
}
export async function createRoute(body: RouteInput): Promise<Route> {
  const { data } = await client.post('/api/v1/routes', body);
  return data;
}
export async function updateRoute(id: string, body: RouteInput): Promise<Route> {
  const { data } = await client.put(`/api/v1/routes/${id}`, body);
  return data;
}
export async function deleteRoute(id: string): Promise<void> {
  await client.delete(`/api/v1/routes/${id}`);
}
export async function toggleRoute(id: string, enabled: boolean): Promise<void> {
  await client.post(`/api/v1/routes/${id}/toggle`, { enabled });
}
export async function duplicateRoute(id: string): Promise<Route> {
  const { data } = await client.post(`/api/v1/routes/${id}/duplicate`);
  return data;
}
export async function batchRoutes(action: BatchAction, ids: string[]): Promise<void> {
  await client.post('/api/v1/routes/batch', { action, ids });
}
export async function getRouteTable(family: 'ipv4' | 'ipv6'): Promise<RouteEntry[]> {
  const { data } = await client.get('/api/v1/route-table', { params: { family } });
  return data.items ?? [];
}

// ===================== 内外网设置 / 网卡 =====================

export interface NIC {
  name: string;
  mac: string;
  up: boolean;       // 链路(carrier)
  running: boolean;  // operstate up
  speed_mb: number;
  duplex: string;
  mtu: number;
  kind: 'physical' | 'bridge' | 'vlan' | 'wifi' | 'virtual';
  bound: string;     // 绑定到的接口(lan/wan)或空
  role: '' | 'lan' | 'wan';
  rx_bytes: number;
  tx_bytes: number;
  ip_addrs: string[] | null; // 该网卡全部地址（IPv4+IPv6，CIDR）
}

export interface NetIface {
  id: string;
  name: string;
  role: 'lan' | 'wan';
  proto: 'static' | 'dhcp' | 'pppoe';
  device: string;
  ports: string[];
  ipaddr: string;
  netmask: string;
  gateway: string;
  dns_primary: string;
  dns_secondary: string;
  username: string;
  password: string;
  service: string;
  ac: string;
  mtu: number;
  default_gw: boolean;
  clone_mac: string;
  remark: string;
  up: boolean;        // 只读运行态
  runtime_ip: string; // 只读运行 IP
}
export type NetIfaceInput = Omit<NetIface, 'up' | 'runtime_ip'>;

export interface NetOverview {
  wan_count: number;
  wan_up: number;
  connections: number;
  lan_count: number;
  lan_up: number;
  dhcp_on: number;
  terminals: number;
  free_ports: number;
  wans: NetIface[];
  lans: NetIface[];
}

export interface DHCPSvcInfo {
  daemon: string; // dnsmasq | odhcpd | ""
  dnsmasq_installed: boolean;
  odhcpd_installed: boolean;
  can_install: boolean;
  pkg_manager: string;
}

export async function listNICs(): Promise<NIC[]> {
  const { data } = await client.get('/api/v1/nics');
  return data.items ?? [];
}
export async function getNetOverview(): Promise<NetOverview> {
  const { data } = await client.get('/api/v1/netcfg/overview');
  return data;
}
export async function listNetIfaces(): Promise<NetIface[]> {
  const { data } = await client.get('/api/v1/ifaces');
  return data.items ?? [];
}
export async function createNetIface(body: NetIfaceInput): Promise<NetIface> {
  const { data } = await client.post('/api/v1/ifaces', body);
  return data;
}
export async function updateNetIface(id: string, body: NetIfaceInput): Promise<NetIface> {
  const { data } = await client.put(`/api/v1/ifaces/${id}`, body);
  return data;
}
export async function deleteNetIface(id: string): Promise<void> {
  await client.delete(`/api/v1/ifaces/${id}`);
}
export async function ifaceAction(id: string, action: 'connect' | 'disconnect' | 'restart'): Promise<void> {
  await client.post(`/api/v1/ifaces/${id}/action`, { action });
}
export async function getDHCPService(): Promise<DHCPSvcInfo> {
  const { data } = await client.get('/api/v1/dhcp/service');
  return data;
}
export async function installDHCP(): Promise<{ ok: boolean; output: string }> {
  const { data } = await client.post('/api/v1/dhcp/install', {}, { timeout: 180000 });
  return data;
}
