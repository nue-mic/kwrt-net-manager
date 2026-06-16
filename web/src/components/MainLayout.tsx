import { useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { Layout, Menu, Button, Space, Typography, Modal, App } from 'antd';
import {
  DashboardOutlined,
  ApartmentOutlined,
  NodeIndexOutlined,
  HddOutlined,
  CloudUploadOutlined,
  SettingOutlined,
  InfoCircleOutlined,
  PoweroffOutlined,
  WifiOutlined,
  GlobalOutlined,
  ArrowUpOutlined,
  ArrowDownOutlined,
} from '@ant-design/icons';
import type { MenuProps } from 'antd';
import { useNavigate, useLocation, Outlet } from 'react-router-dom';
import client, { getAPIToken, clearAPIToken } from '../api/client';
import ThemeSwitcher from '../theme/ThemeSwitcher';
import { useBranding } from '../branding/BrandingContext';

const { Sider, Header, Content } = Layout;
const { Text } = Typography;

type MenuItem = Required<MenuProps>['items'][number];

// ---- 顶栏实时指标轮询 ----
interface Metrics {
  cpu: number;
  mem: number;
  up: number; // bytes/s
  down: number; // bytes/s
}

function useSysMetrics(): Metrics {
  const [m, setM] = useState<Metrics>({ cpu: 0, mem: 0, up: 0, down: 0 });
  const prev = useRef<{ sent: number; recv: number; t: number } | null>(null);

  useEffect(() => {
    let alive = true;
    const tick = async () => {
      try {
        const [cpu, mem, net] = await Promise.all([
          client.get('/api/v1/system/cpu').then((r) => r.data),
          client.get('/api/v1/system/memory').then((r) => r.data),
          client.get('/api/v1/system/network').then((r) => r.data),
        ]);
        let sent = 0;
        let recv = 0;
        for (const it of net.items ?? []) {
          sent += it.bytes_sent ?? 0;
          recv += it.bytes_recv ?? 0;
        }
        const now = Date.now();
        let up = 0;
        let down = 0;
        if (prev.current) {
          const dt = (now - prev.current.t) / 1000;
          if (dt > 0) {
            up = Math.max(0, (sent - prev.current.sent) / dt);
            down = Math.max(0, (recv - prev.current.recv) / dt);
          }
        }
        prev.current = { sent, recv, t: now };
        if (alive) {
          setM({ cpu: cpu.usage_percent ?? 0, mem: mem.used_percent ?? 0, up, down });
        }
      } catch {
        /* 静默 */
      }
    };
    void tick();
    const id = setInterval(tick, 2500);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, []);

  return m;
}

function fmtSpeed(bps: number): string {
  if (bps >= 1024 * 1024) return (bps / 1024 / 1024).toFixed(2) + ' MB/s';
  if (bps >= 1024) return (bps / 1024).toFixed(2) + ' KB/s';
  return Math.round(bps) + ' B/s';
}

const MainLayout: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const { message } = App.useApp();
  const { branding } = useBranding();
  const metrics = useSysMetrics();

  useEffect(() => {
    if (!getAPIToken()) navigate('/login');
  }, [navigate]);

  const handleLogout = () => {
    Modal.confirm({
      title: '确认注销登录？',
      content: '退出后将清除本地 API 令牌，需重新输入才能继续使用。',
      okText: '退出',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: () => {
        clearAPIToken();
        message.success('已安全登出');
        navigate('/login');
      },
    });
  };

  const menuItems: MenuItem[] = useMemo(
    () => [
      {
        key: 'g-dash',
        type: 'group',
        label: 'Dashboard',
        children: [
          { key: '/dashboard', icon: <DashboardOutlined />, label: '系统概览' },
          { key: '/system', icon: <HddOutlined />, label: '系统监控' },
        ],
      },
      {
        key: 'g-net',
        type: 'group',
        label: '网络设置',
        children: [
          {
            key: 'net',
            icon: <GlobalOutlined />,
            label: '内外网设置',
            children: [
              { key: '/net', label: '内外网设置' },
              { key: '/nics', label: '网卡列表' },
            ],
          },
          {
            key: 'dhcp',
            icon: <ApartmentOutlined />,
            label: 'DHCP 设置',
            children: [
              { key: '/dhcp/servers', label: 'DHCP 服务端' },
              { key: '/dhcp/statics', label: 'DHCP 静态分配' },
              { key: '/dhcp/leases', label: 'DHCP 终端列表' },
              { key: '/dhcp/acl', label: 'DHCP 黑白名单' },
            ],
          },
          {
            key: 'dns',
            icon: <GlobalOutlined />,
            label: 'DNS 设置',
            children: [
              { key: '/dns/settings', label: 'DNS 设置' },
              { key: '/dns/cache', label: 'DNS 缓存状态' },
              { key: '/dns/records', label: '自定义解析' },
              { key: '/dns/domain-routes', label: '域名分流 DNS' },
            ],
          },
          {
            key: 'routes',
            icon: <NodeIndexOutlined />,
            label: '静态路由',
            children: [
              { key: '/routes', label: '静态路由' },
              { key: '/route-table', label: '当前路由表' },
            ],
          },
          {
            key: 'ipv6',
            icon: <GlobalOutlined />,
            label: 'IPv6',
            children: [
              { key: '/ipv6/settings', label: 'IPv6设置' },
              { key: '/ipv6/line-detail', label: 'IPv6线路详情' },
              { key: '/ipv6/leases', label: 'DHCPv6终端' },
              { key: '/ipv6/prefix-static', label: '前缀静态分配' },
              { key: '/ipv6/acl', label: 'DHCPv6黑白名单' },
              { key: '/ipv6/neighbors', label: '邻居列表' },
            ],
          },
        ],
      },
      {
        key: 'g-sys',
        type: 'group',
        label: '系统',
        children: [
          { key: '/backup', icon: <CloudUploadOutlined />, label: '定时备份' },
          { key: '/settings', icon: <SettingOutlined />, label: '系统设置' },
          { key: '/about', icon: <InfoCircleOutlined />, label: '关于我们' },
        ],
      },
    ],
    []
  );

  const selectedKey = useMemo(() => location.pathname, [location.pathname]);
  const openKeys = useMemo(() => {
    if (location.pathname === '/net' || location.pathname === '/nics') return ['net'];
    if (location.pathname.startsWith('/ipv6')) return ['ipv6'];
    if (location.pathname.startsWith('/dhcp')) return ['dhcp'];
    if (location.pathname.startsWith('/dns')) return ['dns'];
    if (location.pathname === '/routes' || location.pathname === '/route-table') return ['routes'];
    return [];
  }, [location.pathname]);

  const metric = (icon: ReactNode, label: string, value: string) => (
    <Space size={4} style={{ color: 'rgba(255,255,255,0.92)', fontSize: 13 }}>
      {icon}
      <span style={{ opacity: 0.8 }}>{label}</span>
      <span style={{ fontVariantNumeric: 'tabular-nums' }}>{value}</span>
    </Space>
  );

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider width={216} theme="dark" breakpoint="lg" collapsedWidth={0} style={{ position: 'sticky', top: 0, height: '100vh', overflow: 'auto' }}>
        <div
          style={{
            height: 52,
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            padding: '0 16px',
            background: 'rgba(0,0,0,0.2)',
          }}
        >
          <WifiOutlined style={{ fontSize: 20, color: '#4fc3f7' }} />
          <Text strong style={{ color: '#fff', fontSize: 14, letterSpacing: 0.5, whiteSpace: 'nowrap' }}>
            {branding.app_name}
          </Text>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[selectedKey]}
          defaultOpenKeys={openKeys}
          onClick={({ key }) => navigate(key)}
          items={menuItems}
          style={{ borderInlineEnd: 'none', marginTop: 4 }}
        />
      </Sider>

      <Layout>
        <Header
          style={{
            background: 'linear-gradient(90deg, #1f6fb2, #2a82c9)',
            padding: '0 20px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            height: 52,
            position: 'sticky',
            top: 0,
            zIndex: 9,
          }}
        >
          <Text style={{ color: 'rgba(255,255,255,0.75)', fontSize: 12 }}>{branding.app_subtitle}</Text>
          <Space size="large" align="center" wrap>
            {metric(<DashboardOutlined />, 'CPU', metrics.cpu.toFixed(1) + '%')}
            {metric(<HddOutlined />, '内存', metrics.mem.toFixed(0) + '%')}
            {metric(<ArrowUpOutlined style={{ color: '#a5d6a7' }} />, '上行', fmtSpeed(metrics.up))}
            {metric(<ArrowDownOutlined style={{ color: '#90caf9' }} />, '下行', fmtSpeed(metrics.down))}
            <ThemeSwitcher />
            <Button type="text" size="small" danger icon={<PoweroffOutlined />} onClick={handleLogout} style={{ color: '#ffd1d1' }}>
              登出
            </Button>
          </Space>
        </Header>

        <Content style={{ margin: 16, background: 'transparent', minHeight: 'calc(100vh - 84px)' }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
};

export default MainLayout;
