import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { Card, Col, Row, Statistic, Tag, Typography, Space, Button, Empty } from 'antd';
import {
  GlobalOutlined,
  ApartmentOutlined,
  TeamOutlined,
  PushpinOutlined,
  NodeIndexOutlined,
  RightOutlined,
  ArrowUpOutlined,
  ArrowDownOutlined,
  ApiOutlined,
} from '@ant-design/icons';
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip as RTooltip,
  ResponsiveContainer,
  CartesianGrid,
} from 'recharts';
import { useNavigate } from 'react-router-dom';
import PageCard from '../components/PageCard';
import { useNetData } from '../hooks/useNetData';
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

function fmtUptime(sec: number): string {
  if (!sec || sec < 0) return '—';
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  if (d > 0) return `${d} 天 ${h} 时 ${m} 分`;
  if (h > 0) return `${h} 时 ${m} 分 ${s} 秒`;
  return `${m} 分 ${s} 秒`;
}

// 实时上下行速率采样：每 3 秒拉一次 /system/network，按差值算速率，保留最近 windowN 个点。
function useSpeedHistory(windowN = 80) {
  const [series, setSeries] = useState<{ t: string; up: number; down: number }[]>([]);
  const [rate, setRate] = useState({ up: 0, down: 0 });
  const prev = useRef<{ sent: number; recv: number; t: number } | null>(null);

  useEffect(() => {
    let alive = true;
    const tick = async () => {
      try {
        const data = await client.get('/api/v1/system/network').then((r) => r.data);
        let sent = 0;
        let recv = 0;
        for (const it of data.items ?? []) {
          sent += it.bytes_sent ?? 0;
          recv += it.bytes_recv ?? 0;
        }
        const now = Date.now();
        if (prev.current) {
          const dt = (now - prev.current.t) / 1000;
          if (dt > 0 && alive) {
            const up = Math.max(0, (sent - prev.current.sent) / dt);
            const down = Math.max(0, (recv - prev.current.recv) / dt);
            setRate({ up, down });
            const label = new Date().toLocaleTimeString('zh-CN', { hour12: false });
            setSeries((arr) => [...arr, { t: label, up: +(up / 1024).toFixed(1), down: +(down / 1024).toFixed(1) }].slice(-windowN));
          }
        }
        prev.current = { sent, recv, t: now };
      } catch {
        /* 静默 */
      }
    };
    void tick();
    const id = setInterval(tick, 3000);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, [windowN]);

  return { series, rate };
}

// 主机运行时长：每 5 秒刷新一次 uptime。
function useUptime() {
  const [up, setUp] = useState(0);
  useEffect(() => {
    let alive = true;
    const tick = async () => {
      try {
        const info = await client.get('/api/v1/system/info').then((r) => r.data);
        if (alive) setUp(info?.host?.uptime_seconds ?? info?.uptime_s ?? 0);
      } catch {
        /* 静默 */
      }
    };
    void tick();
    const id = setInterval(tick, 5000);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, []);
  return up;
}

export default function Dashboard() {
  const navigate = useNavigate();
  const [status, setStatus] = useState<net.NetStatus | null>(null);
  const { series, rate } = useSpeedHistory();
  const uptime = useUptime();

  useEffect(() => {
    net.getStatus().then(setStatus).catch(() => {});
  }, []);

  const { data } = useNetData(async () => {
    const [ov, servers, statics, routes] = await Promise.all([
      net.getNetOverview(),
      net.listServers(),
      net.listStatics(),
      net.listRoutes(),
    ]);
    return {
      ov,
      poolFree: servers.reduce((a, s) => a + (s.remaining || 0), 0),
      servers: servers.length,
      serversOn: servers.filter((s) => s.enabled).length,
      statics: statics.items.length,
      routes: routes.length,
      routesOn: routes.filter((r) => r.enabled).length,
    };
  }, { ov: EMPTY_OV, poolFree: 0, servers: 0, serversOn: 0, statics: 0, routes: 0, routesOn: 0 });

  const ov = data.ov;
  const wanOnline = ov.wan_up > 0;
  const online = wanOnline || ov.lan_up > 0;

  const quick: { title: string; value: number; sub: string; icon: ReactNode; color: string; to: string }[] = [
    { title: 'DHCP 服务端', value: data.servers, sub: `${data.serversOn} 个已启用`, icon: <ApartmentOutlined />, color: '#1f6fb2', to: '/dhcp/servers' },
    { title: '活动终端', value: ov.terminals, sub: '在线设备', icon: <TeamOutlined />, color: '#52c41a', to: '/dhcp/leases' },
    { title: '静态分配', value: data.statics, sub: 'IP-MAC 绑定', icon: <PushpinOutlined />, color: '#722ed1', to: '/dhcp/statics' },
    { title: '静态路由', value: data.routes, sub: `${data.routesOn} 条已启用`, icon: <NodeIndexOutlined />, color: '#fa8c16', to: '/routes' },
  ];

  return (
    <PageCard
      breadcrumb={['总览', '系统概况']}
      title="系统概况"
      extra={
        status && (
          <Space>
            <Text type="secondary">后端</Text>
            <Tag color={status.backend === 'uci' ? 'blue' : 'default'}>{status.backend === 'uci' ? 'OpenWrt UCI' : '模拟(store)'}</Tag>
            <Tag color={status.dhcp_ok ? 'success' : 'error'}>{status.dhcp_ok ? 'DHCP 正常' : 'DHCP 异常'}</Tag>
            {status.pending && <Tag color="warning">有未生效变更</Tag>}
          </Space>
        )
      }
    >
      {/* 第一行：连接状态 / 速率 / 物理连接 */}
      <Row gutter={[16, 16]}>
        <Col xs={24} md={8}>
          <Card style={{ background: online ? 'linear-gradient(135deg,#3aab73,#52c41a)' : '#8c8c8c', color: '#fff', height: '100%' }} styles={{ body: { padding: 20 } }}>
            <Space direction="vertical" size={2}>
              <Space><GlobalOutlined style={{ fontSize: 18 }} /><Text style={{ color: 'rgba(255,255,255,0.92)' }}>{wanOnline ? '外网' : ov.wan_count > 0 ? '外网' : '内网'}</Text></Space>
              <Text style={{ color: '#fff', fontSize: 30, fontWeight: 700, lineHeight: 1.2 }}>
                {online ? '已连接' : ov.wan_count > 0 ? '未连接' : '运行中'}
              </Text>
              <Text style={{ color: 'rgba(255,255,255,0.85)', fontSize: 12 }}>已运行 {fmtUptime(uptime)}</Text>
            </Space>
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card title="速率状态" size="small" style={{ height: '100%' }}>
            <Space direction="vertical" size={10} style={{ width: '100%' }}>
              <Space size={8}><ArrowUpOutlined style={{ color: '#cf1322' }} /><Text type="secondary">上行</Text><Text strong style={{ fontSize: 18 }}>{fmtSpeed(rate.up)}</Text></Space>
              <Space size={8}><ArrowDownOutlined style={{ color: '#1f6fb2' }} /><Text type="secondary">下行</Text><Text strong style={{ fontSize: 18 }}>{fmtSpeed(rate.down)}</Text></Space>
            </Space>
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card title="物理连接" size="small" style={{ height: '100%' }}>
            <Space size="large" wrap>
              <Statistic title="连接设备" value={ov.terminals} />
              <Statistic title="连接数" value={ov.connections} />
              <div>
                <div style={{ fontSize: 13 }}>有线：{ov.terminals}</div>
                <div style={{ fontSize: 13, color: '#999' }}>无线：0</div>
              </div>
            </Space>
          </Card>
        </Col>
      </Row>

      {/* 第二行：接口状态 */}
      <Card title="接口状态" size="small" style={{ marginTop: 16 }} extra={<Button type="link" size="small" onClick={() => navigate('/net')}>内外网设置 <RightOutlined /></Button>}>
        <Row gutter={[16, 16]} align="middle">
          <Col xs={24} md={8}>
            <Space size="large" wrap>
              <Statistic title="WAN 已启用" value={ov.wan_count} prefix={<GlobalOutlined style={{ color: '#1f6fb2' }} />} />
              <Statistic title="LAN 已启用" value={ov.lan_count} prefix={<ApartmentOutlined style={{ color: '#52c41a' }} />} />
              <Statistic title="DHCP 池剩余" value={data.poolFree} />
            </Space>
          </Col>
          <Col xs={24} md={16}>
            <Space wrap size={[12, 12]}>
              {ov.wans.map((w) => <PortMini key={w.id} title={w.name} up={w.up} color="#1f6fb2" icon={<GlobalOutlined />} onClick={() => navigate('/net')} />)}
              {ov.lans.map((l) => <PortMini key={l.id} title={l.name} up={l.up} color="#52c41a" icon={<ApartmentOutlined />} onClick={() => navigate('/net')} />)}
              {ov.wan_count + ov.lan_count === 0 && <Text type="secondary" style={{ fontSize: 12 }}>暂无接口</Text>}
            </Space>
          </Col>
        </Row>
      </Card>

      {/* 第三行：近 4 分钟上下行速率 */}
      <Card title="近 4 分钟上下行速率（KB/s）" size="small" style={{ marginTop: 16 }}>
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

      {/* 第四行：业务快捷入口 */}
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
    </PageCard>
  );
}

function PortMini({ title, up, color, icon, onClick }: { title: string; up: boolean; color: string; icon: ReactNode; onClick?: () => void }) {
  return (
    <Card size="small" hoverable onClick={onClick} style={{ width: 104, textAlign: 'center', borderColor: up ? color : '#d9d9d9', cursor: 'pointer' }} styles={{ body: { padding: '8px 4px' } }}>
      <div style={{ fontSize: 20, color: up ? color : '#bfbfbf', position: 'relative' }}>
        {up ? <ApiOutlined style={{ position: 'absolute', top: -4, right: 18, fontSize: 11, color }} /> : null}
        {icon}
      </div>
      <div style={{ fontWeight: 600, fontSize: 13, marginTop: 2 }}>{title}</div>
      <Text type="secondary" style={{ fontSize: 11 }}>{up ? '已连接' : '未连接'}</Text>
    </Card>
  );
}
