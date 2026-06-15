import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Card, Col, Row, Statistic, Tag, Typography, Space, Button, Skeleton } from 'antd';
import {
  ApartmentOutlined,
  TeamOutlined,
  PushpinOutlined,
  NodeIndexOutlined,
  RightOutlined,
} from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import PageCard from '../components/PageCard';
import { useNetData } from '../hooks/useNetData';
import * as net from '../api/netcfg';

const { Text } = Typography;

interface Overview {
  servers: number;
  serversOn: number;
  leases: number;
  leasesStatic: number;
  statics: number;
  routes: number;
  routesOn: number;
}

export default function Dashboard() {
  const navigate = useNavigate();
  const [status, setStatus] = useState<net.NetStatus | null>(null);

  useEffect(() => {
    net.getStatus().then(setStatus).catch(() => {});
  }, []);

  const { data, loading } = useNetData<Overview>(async () => {
    const [servers, leases, statics, routes] = await Promise.all([
      net.listServers(),
      net.listLeases(),
      net.listStatics(),
      net.listRoutes(),
    ]);
    return {
      servers: servers.length,
      serversOn: servers.filter((s) => s.enabled).length,
      leases: leases.length,
      leasesStatic: leases.filter((l) => l.static).length,
      statics: statics.items.length,
      routes: routes.length,
      routesOn: routes.filter((r) => r.enabled).length,
    };
  }, { servers: 0, serversOn: 0, leases: 0, leasesStatic: 0, statics: 0, routes: 0, routesOn: 0 });

  const cards: { title: string; value: number; sub: string; icon: ReactNode; color: string; to: string }[] = [
    { title: 'DHCP 服务端', value: data.servers, sub: `${data.serversOn} 个已启用`, icon: <ApartmentOutlined />, color: '#1f6fb2', to: '/dhcp/servers' },
    { title: '活动终端', value: data.leases, sub: `${data.leasesStatic} 个静态分配`, icon: <TeamOutlined />, color: '#52c41a', to: '/dhcp/leases' },
    { title: '静态分配', value: data.statics, sub: 'IP-MAC 绑定', icon: <PushpinOutlined />, color: '#722ed1', to: '/dhcp/statics' },
    { title: '静态路由', value: data.routes, sub: `${data.routesOn} 条已启用`, icon: <NodeIndexOutlined />, color: '#fa8c16', to: '/routes' },
  ];

  return (
    <PageCard
      breadcrumb={['总览', '概览']}
      title="网络概览"
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
      {loading ? (
        <Skeleton active />
      ) : (
        <Row gutter={[16, 16]}>
          {cards.map((c) => (
            <Col xs={24} sm={12} xl={6} key={c.title}>
              <Card hoverable onClick={() => navigate(c.to)} style={{ borderTop: `3px solid ${c.color}` }}>
                <Space align="start" style={{ justifyContent: 'space-between', width: '100%' }}>
                  <Statistic title={c.title} value={c.value} valueStyle={{ color: c.color }} />
                  <span style={{ fontSize: 28, color: c.color, opacity: 0.85 }}>{c.icon}</span>
                </Space>
                <div style={{ marginTop: 8, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <Text type="secondary" style={{ fontSize: 12 }}>{c.sub}</Text>
                  <Button type="link" size="small" onClick={(e) => { e.stopPropagation(); navigate(c.to); }}>
                    管理 <RightOutlined />
                  </Button>
                </div>
              </Card>
            </Col>
          ))}
        </Row>
      )}
    </PageCard>
  );
}
