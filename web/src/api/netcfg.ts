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
  vendor?: string; // OUI 厂商识别（只读）
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

export type RouteType = '' | 'unicast' | 'blackhole' | 'unreachable' | 'prohibit';

export interface Route {
  id: string;
  family: 'ipv4' | 'ipv6';
  interface: string;
  target: string;
  netmask: string;
  prefix: number;
  gateway: string;
  metric: number;
  type: RouteType; // 正常/黑洞/拒绝/不可达
  mtu: number; // 0=不设
  table: number; // 路由表号（0=主表）
  remark: string;
  enabled: boolean;
  push_to_clients: boolean;
}

export interface PolicyRule {
  id: string;
  family: 'ipv4' | 'ipv6';
  enabled: boolean;
  priority: number;
  src: string;
  dest: string;
  in_iface: string;
  lookup: string; // 查询的路由表号
  remark: string;
}
export type PolicyRuleInput = Omit<PolicyRule, 'id'>;

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
export async function setLeaseNote(mac: string, remark: string): Promise<void> {
  await client.put('/api/v1/dhcp/leases/note', { mac, remark });
}
// 池内下一个空闲 IP（静态分配表单预填用，可空字符串）。
export async function suggestNextIp(iface = ''): Promise<string> {
  const { data } = await client.get('/api/v1/dhcp/statics/suggest-next-ip', { params: { interface: iface } });
  return data.ip ?? '';
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
// ---- 策略路由 ----
export async function listPolicyRules(): Promise<PolicyRule[]> {
  const { data } = await client.get('/api/v1/policy-rules');
  return data.items ?? [];
}
export async function createPolicyRule(body: PolicyRuleInput): Promise<PolicyRule> {
  const { data } = await client.post('/api/v1/policy-rules', body);
  return data;
}
export async function updatePolicyRule(id: string, body: PolicyRuleInput): Promise<PolicyRule> {
  const { data } = await client.put(`/api/v1/policy-rules/${id}`, body);
  return data;
}
export async function deletePolicyRule(id: string): Promise<void> {
  await client.delete(`/api/v1/policy-rules/${id}`);
}
export async function togglePolicyRule(id: string, enabled: boolean): Promise<void> {
  await client.post(`/api/v1/policy-rules/${id}/toggle`, { enabled });
}
export async function batchPolicyRules(action: BatchAction, ids: string[]): Promise<void> {
  await client.post('/api/v1/policy-rules/batch', { action, ids });
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

export interface IfaceAddr {
  address: string;
  prefix: number;
  family: 'ipv4' | 'ipv6';
  remark: string;
  enabled: boolean;
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
  extra_addrs: IfaceAddr[];
  metric?: number;
  peerdns?: boolean | null;
  broadcast?: string;
  force_link?: boolean | null;
  auto?: boolean | null;
  ip6assign?: number;
  ip6hint?: string;
  ip6gw?: string;
  ip6prefix?: string;
  ip6ifaceid?: string;
  keepalive?: string;
  pppoe_ipv6?: boolean | null;
  up: boolean;        // 只读运行态
  runtime_ip: string; // 只读运行 IP
  status?: 'connected' | 'connecting' | 'disconnected'; // 只读：拨号中(connecting) 区别于未连接
}
export type NetIfaceInput = Omit<NetIface, 'up' | 'runtime_ip' | 'status'>;

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

// 网卡上的一个地址（比 NIC.ip_addrs 纯 CIDR 字符串更结构化），供详情页逐条展示。
export interface NICAddr {
  family: 'ipv4' | 'ipv6';
  address: string; // 不含前缀，如 192.168.1.1
  prefix: number;  // CIDR 位数
  scope: string;   // global | link | host
}

// /sys/class/net/<name>/statistics 的收发计数明细。
export interface NICStats {
  rx_bytes: number;
  tx_bytes: number;
  rx_packets: number;
  tx_packets: number;
  rx_errors: number;
  tx_errors: number;
  rx_dropped: number;
  tx_dropped: number;
  multicast: number;
  collisions: number;
}

// 单块网卡的综合详情（内嵌 NIC 全部字段 + sysfs 链路/统计 + 网桥从属 + VLAN + ethtool 驱动/链路能力）。
export interface NICDetail extends NIC {
  ifindex: number;
  operstate: string;       // up/down/lowerlayerdown/…
  carrier: boolean;        // carrier == "1"
  carrier_changes: number;
  tx_queue_len: number;
  ifalias: string;
  master: string;          // 所属网桥（被 enslave 时）
  bridge_ports: string[] | null; // 若自身是网桥：成员端口列表
  vlan_id?: number;
  vlan_proto?: string;
  // ethtool 尽力而为（未安装/解析失败留空）。
  driver: string;
  driver_version: string;
  firmware: string;
  bus_info: string;
  perm_mac: string;
  autoneg: string; // on | off | ""
  port: string;    // Twisted Pair | Fibre | …
  supported_modes: string[] | null;
  advertised_modes: string[] | null;
  stats: NICStats;
  addrs: NICAddr[] | null; // 比 ip_addrs 更详细（family/prefix/scope）
}

export async function listNICs(): Promise<NIC[]> {
  const { data } = await client.get('/api/v1/nics');
  return data.items ?? [];
}
export async function getNICDetail(name: string): Promise<NICDetail> {
  const { data } = await client.get('/api/v1/nics/' + encodeURIComponent(name));
  return data;
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
