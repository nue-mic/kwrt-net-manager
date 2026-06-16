import { useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import {
  Card, Col, Row, Statistic, Tag, Typography, Space, Button, Empty, Progress, Table, Drawer,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  GlobalOutlined, ApartmentOutlined, TeamOutlined, PushpinOutlined, NodeIndexOutlined, RightOutlined,
  ArrowUpOutlined, ArrowDownOutlined, ApiOutlined, ClusterOutlined, DesktopOutlined, WifiOutlined,
  DatabaseOutlined, HddOutlined, ThunderboltOutlined, EyeOutlined, ReloadOutlined,
} from '@ant-design/icons';
import { AreaChart, Area, XAxis, YAxis, Tooltip as RTooltip, ResponsiveContainer, CartesianGrid } from 'recharts';
import { useNavigate } from 'react-router-dom';
import PageCard from '../components/PageCard';
import client from '../api/client';
import * as net from '../api/netcfg';

const { Text } = Typography;

const EMPTY_OV: net.NetOverview = {
  wan_count: 0, wan_up: 0, connections: 0, lan_count: 0, lan_up: 0,
  dhcp_on: 0, terminals: 0, free_ports: 0, wans: [], lans: [],
};

function fmtSpeed(bps: number): string {
  if (bps >= 1024 * 1024) return (bps / 1024 / 1024).toFixed(2) + ' MB/s';
  if (bps >= 1024) return (bps / 1024).toFixed(1) + ' KB/s';
  return Math.round(bps) + ' B/s';
}
function fmtBytes(n: number): string {
  if (n >= 1024 ** 3) return (n / 1024 ** 3).toFixed(2) + ' GB';
  if (n >= 1024 ** 2) return (n / 1024 ** 2).toFixed(1) + ' MB';
  if (n >= 1024) return (n / 1024).toFixed(1) + ' KB';
  return n + ' B';
}
function fmtUptime(sec: number): string {
  if (!sec || sec < 0) return '—';
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60), s = Math.floor(sec % 60);
  if (d > 0) return `${d} 天 ${h} 时 ${m} 分`;
  if (h > 0) return `${h} 时 ${m} 分 ${s} 秒`;
  return `${m} 分 ${s} 秒`;
}

interface SysMetrics { cpu: number; mem: number; memUsed: number; memTotal: number; disk: number; diskUsed: number; diskTotal: number; uptime: number; }
interface NicRate { up: number; down: number; }
interface Conns { tcp: number; udp: number; byStatus: Record<string, number>; ownedTcp: number; ownedUdp: number; }
interface ConnFlow { family: string; proto: string; src: string; dst: string; packets: number; bytes: number; }
interface FlowResult { flows: ConnFlow[]; total: number; acct_available: boolean; }

// 全量实时轮询：网卡(含全部IP)+总览+在线设备+系统资源+连接数，每 intervalMs 刷新一次，
// 并按 rx/tx 差值算每块网卡与总的实时速率（满足「物理连接区块实时刷新」）。
function useDashboard(intervalMs = 4000) {
  const [nics, setNics] = useState<net.NIC[]>([]);
  const [ov, setOv] = useState<net.NetOverview>(EMPTY_OV);
  const [leases, setLeases] = useState<net.Lease[]>([]);
  const [sys, setSys] = useState<SysMetrics>({ cpu: 0, mem: 0, memUsed: 0, memTotal: 0, disk: 0, diskUsed: 0, diskTotal: 0, uptime: 0 });
  const [conns, setConns] = useState<Conns>({ tcp: 0, udp: 0, byStatus: {}, ownedTcp: 0, ownedUdp: 0 });
  const [rates, setRates] = useState<Record<string, NicRate>>({});
  const [total, setTotal] = useState<NicRate>({ up: 0, down: 0 });
  const [series, setSeries] = useState<{ t: string; up: number; down: number }[]>([]);
  const [ts, setTs] = useState(0);
  const prev = useRef<Record<string, { rx: number; tx: number }>>({});
  const prevT = useRef<number>(0);

  useEffect(() => {
    let alive = true;
    const tick = async () => {
      try {
        const [nicList, ovData, leaseList, cpu, mem, disk, info, cs] = await Promise.all([
          net.listNICs(),
          net.getNetOverview(),
          net.listLeases(),
          client.get('/api/v1/system/cpu').then((r) => r.data).catch(() => ({})),
          client.get('/api/v1/system/memory').then((r) => r.data).catch(() => ({})),
          client.get('/api/v1/system/disk').then((r) => r.data).catch(() => ({})),
          client.get('/api/v1/system/info').then((r) => r.data).catch(() => ({})),
          client.get('/api/v1/system/connections').then((r) => r.data).catch(() => ({})),
        ]);
        if (!alive) return;
        const now = Date.now();
        const dt = prevT.current ? (now - prevT.current) / 1000 : 0;
        const nr: Record<string, NicRate> = {};
        let tup = 0, tdown = 0;
        for (const n of nicList) {
          const p = prev.current[n.name];
          if (p && dt > 0) {
            const up = Math.max(0, (n.tx_bytes - p.tx) / dt);
            const down = Math.max(0, (n.rx_bytes - p.rx) / dt);
            nr[n.name] = { up, down };
            // 总速率只统计 WAN/物理口，避免桥与成员口重复计数。
            if (n.role === 'wan') { tup += up; tdown += down; }
          }
          prev.current[n.name] = { rx: n.rx_bytes, tx: n.tx_bytes };
        }
        // 无 WAN 角色时，退化为所有物理网卡之和。
        if (tup === 0 && tdown === 0) {
          for (const n of nicList) {
            if (n.kind === 'physical' && nr[n.name]) { tup += nr[n.name].up; tdown += nr[n.name].down; }
          }
        }
        prevT.current = now;
        setNics(nicList);
        setOv(ovData);
        setLeases(leaseList);
        setRates(nr);
        setTotal({ up: tup, down: tdown });
        setSys({
          cpu: cpu.usage_percent ?? 0,
          mem: mem.used_percent ?? 0, memUsed: mem.used ?? 0, memTotal: mem.total ?? 0,
          disk: disk.used_percent ?? disk.items?.[0]?.used_percent ?? 0,
          diskUsed: disk.used ?? disk.items?.[0]?.used ?? 0, diskTotal: disk.total ?? disk.items?.[0]?.total ?? 0,
          uptime: info?.host?.uptime_seconds ?? info?.uptime_s ?? 0,
        });
        setConns({
          tcp: cs.tcp_total ?? 0, udp: cs.udp_total ?? 0, byStatus: cs.tcp_by_status ?? {},
          ownedTcp: cs.owned_tcp_conns ?? 0, ownedUdp: cs.owned_udp_conns ?? 0,
        });
        if (dt > 0) {
          const label = new Date().toLocaleTimeString('zh-CN', { hour12: false });
          setSeries((arr) => [...arr, { t: label, up: +(tup / 1024).toFixed(1), down: +(tdown / 1024).toFixed(1) }].slice(-80));
        }
        setTs(now);
      } catch {
        /* 静默 */
      }
    };
    void tick();
    const id = setInterval(tick, intervalMs);
    return () => { alive = false; clearInterval(id); };
  }, [intervalMs]);

  return { nics, ov, leases, sys, conns, rates, total, series, ts };
}

// 业务计数（变化较少）：每 15 秒刷新。
function useCounts() {
  const [c, setC] = useState({ servers: 0, serversOn: 0, statics: 0, routes: 0, routesOn: 0, poolFree: 0 });
  useEffect(() => {
    let alive = true;
    const tick = async () => {
      try {
        const [servers, statics, routes] = await Promise.all([net.listServers(), net.listStatics(), net.listRoutes()]);
        if (!alive) return;
        setC({
          servers: servers.length, serversOn: servers.filter((s) => s.enabled).length,
          statics: statics.items.length, routes: routes.length, routesOn: routes.filter((r) => r.enabled).length,
          poolFree: servers.reduce((a, s) => a + (s.remaining || 0), 0),
        });
      } catch { /* 静默 */ }
    };
    void tick();
    const id = setInterval(tick, 15000);
    return () => { alive = false; clearInterval(id); };
  }, []);
  return c;
}

const nicIcon = (k: string): ReactNode =>
  k === 'wifi' ? <WifiOutlined /> : k === 'bridge' ? <ClusterOutlined /> : k === 'physical' ? <ApiOutlined /> : <GlobalOutlined />;

function roleTag(n: net.NIC) {
  if (n.role === 'wan') return <Tag color="blue">WAN</Tag>;
  if (n.role === 'lan') return <Tag color="green">LAN</Tag>;
  const m: Record<string, string> = { bridge: '桥接', physical: '物理', wifi: '无线', vlan: 'VLAN', virtual: '虚拟' };
  return <Tag>{m[n.kind] ?? n.kind}</Tag>;
}

export default function Dashboard() {
  const navigate = useNavigate();
  const [status, setStatus] = useState<net.NetStatus | null>(null);
  const [connOpen, setConnOpen] = useState(false);
  const [flows, setFlows] = useState<FlowResult>({ flows: [], total: 0, acct_available: false });
  const { nics, ov, leases, sys, conns, rates, total, series, ts } = useDashboard();
  const counts = useCounts();

  useEffect(() => { net.getStatus().then(setStatus).catch(() => {}); }, []);

  // 连接详情逐条明细（conntrack）：抽屉打开时每 3s 拉一次，关闭即停。
  useEffect(() => {
    if (!connOpen) return;
    let alive = true;
    const tick = async () => {
      try {
        const r = await client.get('/api/v1/system/conntrack?limit=100').then((x) => x.data);
        if (alive) setFlows({ flows: r.flows ?? [], total: r.total ?? 0, acct_available: !!r.acct_available });
      } catch { /* 静默 */ }
    };
    void tick();
    const id = setInterval(tick, 3000);
    return () => { alive = false; clearInterval(id); };
  }, [connOpen]);

  const online = ov.wan_up > 0 || ov.lan_up > 0;
  const wanIPs = useMemo(
    () => nics.filter((n) => n.role === 'wan').flatMap((n) => (n.ip_addrs ?? []).filter((a) => a.includes('.'))),
    [nics],
  );
  // 展示用网卡：有角色 / 有 IP / 物理·桥·无线（过滤掉无意义的纯虚拟口）。
  const showNics = useMemo(
    () => nics.filter((n) => n.role || (n.ip_addrs && n.ip_addrs.length) || ['physical', 'bridge', 'wifi'].includes(n.kind)),
    [nics],
  );

  const quick = [
    { title: 'DHCP 服务端', value: counts.servers, sub: `${counts.serversOn} 个已启用`, icon: <ApartmentOutlined />, color: '#1f6fb2', to: '/dhcp/servers' },
    { title: '活动终端', value: ov.terminals, sub: '在线设备', icon: <TeamOutlined />, color: '#52c41a', to: '/dhcp/leases' },
    { title: '静态分配', value: counts.statics, sub: 'IP-MAC 绑定', icon: <PushpinOutlined />, color: '#722ed1', to: '/dhcp/statics' },
    { title: '静态路由', value: counts.routes, sub: `${counts.routesOn} 条已启用`, icon: <NodeIndexOutlined />, color: '#fa8c16', to: '/routes' },
  ];

  const leaseCols: ColumnsType<net.Lease> = [
    { title: '主机名称', dataIndex: 'hostname', render: (v: string) => v || '-', ellipsis: true },
    { title: 'IP', dataIndex: 'ip', width: 130 },
    { title: 'MAC', dataIndex: 'mac', width: 150, responsive: ['lg'] },
    { title: '接口', dataIndex: 'interface', width: 70, responsive: ['md'], render: (v: string) => v || '-' },
    { title: '状态', dataIndex: 'static', width: 90, render: (v: boolean) => (v ? <Tag color="success">静态</Tag> : <Tag color="processing">动态</Tag>) },
  ];

  return (
    <PageCard
      breadcrumb={['总览', '系统概况']}
      title="系统概况"
      extra={
        <Space>
          {ts > 0 && <Text type="secondary" style={{ fontSize: 12 }}><ReloadOutlined spin style={{ marginRight: 4 }} />实时</Text>}
          {status && (
            <>
              <Tag color={status.backend === 'uci' ? 'blue' : 'default'}>{status.backend === 'uci' ? 'OpenWrt UCI' : '模拟(store)'}</Tag>
              <Tag color={status.dhcp_ok ? 'success' : 'error'}>{status.dhcp_ok ? 'DHCP 正常' : 'DHCP 异常'}</Tag>
              {status.pending && <Tag color="warning">有未生效变更</Tag>}
            </>
          )}
        </Space>
      }
    >
      {/* 第一行：连接状态 / 系统资源 / 实时速率+连接数 */}
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={8}>
          <Card style={{ background: online ? 'linear-gradient(135deg,#3aab73,#52c41a)' : 'linear-gradient(135deg,#8c8c8c,#bfbfbf)', color: '#fff', height: '100%' }} styles={{ body: { padding: 20 } }}>
            <Space direction="vertical" size={4} style={{ width: '100%' }}>
              <Space><GlobalOutlined style={{ fontSize: 18 }} /><Text style={{ color: 'rgba(255,255,255,0.92)' }}>外网</Text></Space>
              <Text style={{ color: '#fff', fontSize: 30, fontWeight: 700, lineHeight: 1.2 }}>{online ? '已连接' : '未连接'}</Text>
              <Text style={{ color: 'rgba(255,255,255,0.9)', fontSize: 12 }}>已运行 {fmtUptime(sys.uptime)}</Text>
              {wanIPs.length > 0 && (
                <Text style={{ color: 'rgba(255,255,255,0.95)', fontSize: 13 }}>出口 IP：{wanIPs.join('、')}</Text>
              )}
            </Space>
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card title="系统资源" size="small" style={{ height: '100%' }}>
            <Space direction="vertical" size={6} style={{ width: '100%' }}>
              <ResLine icon={<ThunderboltOutlined />} label="CPU" pct={sys.cpu} text={`${sys.cpu.toFixed(1)}%`} />
              <ResLine icon={<DatabaseOutlined />} label="内存" pct={sys.mem} text={sys.memTotal ? `${fmtBytes(sys.memUsed)} / ${fmtBytes(sys.memTotal)}` : `${sys.mem.toFixed(0)}%`} />
              <ResLine icon={<HddOutlined />} label="磁盘" pct={sys.disk} text={sys.diskTotal ? `${fmtBytes(sys.diskUsed)} / ${fmtBytes(sys.diskTotal)}` : `${sys.disk.toFixed(0)}%`} />
            </Space>
          </Card>
        </Col>
        <Col xs={24} lg={8}>
          <Card title="实时速率 / 连接" size="small" style={{ height: '100%' }} extra={<Button type="link" size="small" icon={<EyeOutlined />} onClick={() => setConnOpen(true)}>连接详情</Button>}>
            <Row gutter={8}>
              <Col span={12}>
                <Space direction="vertical" size={8} style={{ width: '100%' }}>
                  <Space size={8}><ArrowUpOutlined style={{ color: '#cf1322' }} /><Text type="secondary">上行</Text><Text strong>{fmtSpeed(total.up)}</Text></Space>
                  <Space size={8}><ArrowDownOutlined style={{ color: '#1f6fb2' }} /><Text type="secondary">下行</Text><Text strong>{fmtSpeed(total.down)}</Text></Space>
                </Space>
              </Col>
              <Col span={12}>
                <Statistic title="活动连接数" value={conns.tcp + conns.udp} valueStyle={{ fontSize: 22 }} />
                <Text type="secondary" style={{ fontSize: 12 }}>TCP {conns.tcp} · UDP {conns.udp}</Text>
              </Col>
            </Row>
          </Card>
        </Col>
      </Row>

      {/* 第二行：网卡 / 物理连接（含全部 IP + 实时速率，自动刷新） */}
      <Card
        title={<Space><ClusterOutlined />物理连接 / 网卡</Space>}
        size="small"
        style={{ marginTop: 16 }}
        extra={
          <Space size="large">
            <Text type="secondary" style={{ fontSize: 12 }}>连接设备 {ov.terminals} · 连接数 {conns.tcp + conns.udp}</Text>
            <Button type="link" size="small" onClick={() => navigate('/nics')}>网卡列表 <RightOutlined /></Button>
          </Space>
        }
      >
        {showNics.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="正在加载网卡…" />
        ) : (
          <Row gutter={[12, 12]}>
            {showNics.map((n) => {
              const r = rates[n.name];
              const ips = n.ip_addrs ?? [];
              return (
                <Col xs={24} sm={12} xl={8} key={n.name}>
                  <Card size="small" hoverable onClick={() => navigate('/nics')} styles={{ body: { padding: 12 } }}
                    style={{ borderLeft: `3px solid ${n.up ? (n.role === 'wan' ? '#1f6fb2' : '#52c41a') : '#d9d9d9'}` }}>
                    <Space style={{ justifyContent: 'space-between', width: '100%' }} align="start">
                      <Space size={6}>
                        <span style={{ fontSize: 16, color: n.up ? '#1f6fb2' : '#bfbfbf' }}>{nicIcon(n.kind)}</span>
                        <Text strong>{n.name}</Text>
                        {roleTag(n)}
                      </Space>
                      <Tag color={n.up ? 'success' : 'default'} style={{ marginInlineEnd: 0 }}>{n.up ? '已连接' : '未连接'}</Tag>
                    </Space>
                    <div style={{ marginTop: 8, fontSize: 12, color: '#888' }}>
                      {n.up && n.speed_mb > 0 ? `${n.speed_mb >= 1000 ? n.speed_mb / 1000 + ' Gbps' : n.speed_mb + ' Mbps'}${n.duplex ? ' · ' + (n.duplex === 'full' ? '全双工' : '半双工') : ''}` : '链路速率未知'}
                      {n.mac ? ` · ${n.mac}` : ''}
                    </div>
                    <div style={{ marginTop: 6 }}>
                      {ips.length === 0 ? (
                        <Text type="secondary" style={{ fontSize: 12 }}>无 IP 地址</Text>
                      ) : (
                        <Space size={[6, 4]} wrap>
                          {ips.map((ip) => (
                            <Tag key={ip} color={ip.includes('.') ? 'geekblue' : 'purple'} style={{ marginInlineEnd: 0, fontFamily: 'monospace' }}>{ip}</Tag>
                          ))}
                        </Space>
                      )}
                    </div>
                    <div style={{ marginTop: 8, display: 'flex', gap: 16, fontSize: 12 }}>
                      <span><ArrowUpOutlined style={{ color: '#cf1322' }} /> {fmtSpeed(r?.up ?? 0)}</span>
                      <span><ArrowDownOutlined style={{ color: '#1f6fb2' }} /> {fmtSpeed(r?.down ?? 0)}</span>
                      <span style={{ color: '#bbb' }}>累计 ↑{fmtBytes(n.tx_bytes)} ↓{fmtBytes(n.rx_bytes)}</span>
                    </div>
                  </Card>
                </Col>
              );
            })}
          </Row>
        )}
      </Card>

      {/* 第三行：实时流量曲线 */}
      <Card title="实时上下行速率（KB/s）" size="small" style={{ marginTop: 16 }}>
        {series.length < 2 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="正在采样…" style={{ padding: 24 }} />
        ) : (
          <ResponsiveContainer width="100%" height={220}>
            <AreaChart data={series} margin={{ top: 8, right: 16, left: 0, bottom: 0 }}>
              <defs>
                <linearGradient id="gUp" x1="0" y1="0" x2="0" y2="1"><stop offset="5%" stopColor="#cf1322" stopOpacity={0.35} /><stop offset="95%" stopColor="#cf1322" stopOpacity={0} /></linearGradient>
                <linearGradient id="gDown" x1="0" y1="0" x2="0" y2="1"><stop offset="5%" stopColor="#1f6fb2" stopOpacity={0.35} /><stop offset="95%" stopColor="#1f6fb2" stopOpacity={0} /></linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" vertical={false} />
              <XAxis dataKey="t" tick={{ fontSize: 11 }} minTickGap={40} />
              <YAxis tick={{ fontSize: 11 }} width={48} />
              <RTooltip />
              <Area type="monotone" dataKey="up" name="上行" stroke="#cf1322" fill="url(#gUp)" strokeWidth={2} isAnimationActive={false} />
              <Area type="monotone" dataKey="down" name="下行" stroke="#1f6fb2" fill="url(#gDown)" strokeWidth={2} isAnimationActive={false} />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </Card>

      {/* 第四行：在线设备（实时，可点击查看） */}
      <Card
        title={<Space><DesktopOutlined />在线设备（{leases.length}）</Space>}
        size="small"
        style={{ marginTop: 16 }}
        extra={<Button type="link" size="small" onClick={() => navigate('/dhcp/leases')}>查看全部 <RightOutlined /></Button>}
      >
        <Table
          rowKey={(r) => r.ip}
          size="small"
          dataSource={leases.slice(0, 8)}
          columns={leaseCols}
          pagination={false}
          onRow={(r) => ({ style: { cursor: 'pointer' }, onClick: () => navigate(r.static ? `/dhcp/statics?q=${encodeURIComponent(r.ip)}` : '/dhcp/leases') })}
          locale={{ emptyText: '暂无在线设备' }}
        />
      </Card>

      {/* 第五行：业务快捷入口 */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        {quick.map((c) => (
          <Col xs={24} sm={12} xl={6} key={c.title}>
            <Card hoverable onClick={() => navigate(c.to)} style={{ borderTop: `3px solid ${c.color}` }}>
              <Space align="start" style={{ justifyContent: 'space-between', width: '100%' }}>
                <Statistic title={c.title} value={c.value} valueStyle={{ color: c.color }} />
                <span style={{ fontSize: 28, color: c.color, opacity: 0.85 }}>{c.icon}</span>
              </Space>
              <div style={{ marginTop: 8, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <Text type="secondary" style={{ fontSize: 12 }}>{c.sub}</Text>
                <Button type="link" size="small" onClick={(e) => { e.stopPropagation(); navigate(c.to); }}>管理 <RightOutlined /></Button>
              </div>
            </Card>
          </Col>
        ))}
      </Row>

      {/* 连接详情抽屉：逐条 conntrack 明细（仿爱快），按流量降序 */}
      <Drawer title={`连接详情（conntrack 共 ${flows.total} 条）`} width={820} open={connOpen} onClose={() => setConnOpen(false)}>
        <Row gutter={8} style={{ marginBottom: 12 }}>
          <Col span={8}><Card size="small"><Statistic title="TCP" value={conns.tcp} valueStyle={{ fontSize: 20 }} /></Card></Col>
          <Col span={8}><Card size="small"><Statistic title="UDP" value={conns.udp} valueStyle={{ fontSize: 20 }} /></Card></Col>
          <Col span={8}><Card size="small"><Statistic title="本工具占用" value={conns.ownedTcp + conns.ownedUdp} valueStyle={{ fontSize: 20 }} /></Card></Col>
        </Row>
        <Space wrap size={6} style={{ marginBottom: 12 }}>
          {Object.entries(conns.byStatus).sort((a, b) => b[1] - a[1]).map(([k, v]) => (
            <Tag key={k} color={k === 'ESTABLISHED' ? 'success' : k === 'LISTEN' ? 'blue' : 'default'}>{k} {v}</Tag>
          ))}
        </Space>
        {!flows.acct_available && flows.flows.length > 0 && (
          <Typography.Paragraph type="warning" style={{ fontSize: 12 }}>
            未开启 conntrack 流量计数（nf_conntrack_acct），故「传输」为 0；如需按流量统计，可在系统里执行 sysctl net.netfilter.nf_conntrack_acct=1。
          </Typography.Paragraph>
        )}
        <Table<ConnFlow>
          rowKey={(r) => `${r.proto}-${r.src}-${r.dst}`}
          size="small"
          dataSource={flows.flows}
          pagination={{ pageSize: 20, showTotal: (t) => `前 ${t} 条（按流量）` }}
          scroll={{ x: 'max-content' }}
          locale={{ emptyText: '暂无连接（或本机无 conntrack）' }}
          columns={[
            { title: '网络', dataIndex: 'family', width: 70, render: (v: string) => <Tag color={v === 'ipv6' ? 'purple' : 'geekblue'}>{v === 'ipv6' ? 'IPv6' : 'IPv4'}</Tag> },
            { title: '协议', dataIndex: 'proto', width: 70, render: (v: string) => <Tag>{(v || '-').toUpperCase()}</Tag> },
            { title: '源地址', dataIndex: 'src', render: (v: string) => <span style={{ fontFamily: 'monospace', fontSize: 12 }}>{v}</span> },
            { title: '目标', dataIndex: 'dst', render: (v: string) => <span style={{ fontFamily: 'monospace', fontSize: 12 }}>{v}</span> },
            { title: '传输', dataIndex: 'bytes', width: 150, render: (b: number, r) => <span>{fmtBytes(b)} <Text type="secondary" style={{ fontSize: 12 }}>({r.packets} 包)</Text></span> },
          ]}
        />
        <Typography.Paragraph type="secondary" style={{ marginTop: 12, fontSize: 12 }}>
          数据来自内核 conntrack（/proc/net/nf_conntrack），每 3 秒刷新，按流量降序取前 100 条。
        </Typography.Paragraph>
      </Drawer>
    </PageCard>
  );
}

function ResLine({ icon, label, pct, text }: { icon: ReactNode; label: string; pct: number; text: string }) {
  const color = pct >= 90 ? '#cf1322' : pct >= 70 ? '#fa8c16' : '#52c41a';
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, marginBottom: 2 }}>
        <Space size={4}>{icon}<Text type="secondary">{label}</Text></Space>
        <Text style={{ fontSize: 12 }}>{text}</Text>
      </div>
      <Progress percent={Math.round(pct)} showInfo={false} strokeColor={color} size="small" />
    </div>
  );
}
