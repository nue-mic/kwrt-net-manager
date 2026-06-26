import { useEffect, useState } from 'react';
import { Card, Space, Typography, Button, Descriptions, Tag, Row, Col } from 'antd';
import {
  GithubOutlined,
  BookOutlined,
  ApartmentOutlined,
  NodeIndexOutlined,
  SafetyCertificateOutlined,
  CloudServerOutlined,
} from '@ant-design/icons';
import client from '../api/client';
import UpdateCard from '../components/UpdateCard';
import PageCard from '../components/PageCard';
import { useBranding } from '../branding/BrandingContext';
import { fmtDateTime } from '../utils/time';
import { getStatus, type NetStatus } from '../api/netcfg';

const { Title, Text, Paragraph } = Typography;

interface VersionResp {
  daemon?: string;
  build_date?: string;
}

const APP_REPO = 'https://github.com/nue-mic/kwrt-net-manager';
const APP_RELEASES = 'https://github.com/nue-mic/kwrt-net-manager/releases';
const APP_DOCS_PATH = '/api/docs/';

const features = [
  { icon: <ApartmentOutlined />, title: 'DHCP 管理', desc: '服务端、静态分配、终端列表、黑白名单，仿爱快交互。' },
  { icon: <NodeIndexOutlined />, title: '静态路由', desc: 'IPv4 / IPv6 路由增删改、复制启停，实时查看当前路由表。' },
  { icon: <CloudServerOutlined />, title: '双后端', desc: 'OpenWrt 走 UCI/dnsmasq/ip；非 OpenWrt 走模拟后端，开发即测。' },
  { icon: <SafetyCertificateOutlined />, title: '安全壳子', desc: 'Bearer 鉴权、品牌定制、定时备份、一键自升级、OpenWrt ipk。' },
];

export default function About() {
  const { branding } = useBranding();
  const [version, setVersion] = useState<VersionResp>({});
  const [status, setStatus] = useState<NetStatus | null>(null);

  useEffect(() => {
    client.get<VersionResp>('/api/v1/version').then((r) => setVersion(r.data)).catch(() => undefined);
    getStatus().then(setStatus).catch(() => undefined);
  }, []);

  return (
    <PageCard breadcrumb={['系统', '关于']} title="关于">
      <Space direction="vertical" size={16} style={{ width: '100%' }}>
        <Card style={{ background: 'linear-gradient(120deg, #1f6fb2, #2a82c9)', border: 'none' }} styles={{ body: { padding: 24 } }}>
          <Title level={3} style={{ color: '#fff', margin: 0 }}>
            {branding.app_name}
          </Title>
          <Text style={{ color: 'rgba(255,255,255,0.85)' }}>{branding.app_subtitle}</Text>
          <Paragraph style={{ color: 'rgba(255,255,255,0.85)', marginTop: 12, marginBottom: 0, maxWidth: 720 }}>
            面向 OpenWrt 的网络管理面板：把 DHCP（dnsmasq）与静态路由的繁琐配置，变成「打开网页点鼠标」。
            单 Go 二进制（无 cgo），前端 React/Ant Design 内嵌，自带 systemd / OpenRC / Windows 服务与 OpenWrt ipk 安装脚本。
          </Paragraph>
          <Space style={{ marginTop: 16 }} wrap>
            <Tag color="blue">Daemon v{version.daemon || '—'}</Tag>
            <Tag>构建 {version.build_date ? fmtDateTime(version.build_date) : '—'}</Tag>
            {status && <Tag color={status.backend === 'uci' ? 'green' : 'default'}>后端：{status.backend === 'uci' ? 'OpenWrt UCI' : '模拟(store)'}</Tag>}
          </Space>
        </Card>

        <UpdateCard />

        <Row gutter={[16, 16]}>
          {features.map((f) => (
            <Col xs={24} sm={12} key={f.title}>
              <Card>
                <Space align="start">
                  <span style={{ fontSize: 22, color: '#1f6fb2' }}>{f.icon}</span>
                  <div>
                    <Text strong>{f.title}</Text>
                    <Paragraph type="secondary" style={{ margin: 0, fontSize: 13 }}>{f.desc}</Paragraph>
                  </div>
                </Space>
              </Card>
            </Col>
          ))}
        </Row>

        <Card title="版本与链接">
          <Descriptions column={1} size="small" styles={{ label: { width: 120 } }}>
            <Descriptions.Item label="守护进程版本">{version.daemon || '—'}</Descriptions.Item>
            <Descriptions.Item label="构建日期">{version.build_date ? fmtDateTime(version.build_date) : '—'}</Descriptions.Item>
            <Descriptions.Item label="网络后端">{status?.backend === 'uci' ? 'OpenWrt UCI（真机）' : '模拟 store（开发/测试）'}</Descriptions.Item>
          </Descriptions>
          <Space style={{ marginTop: 12 }} wrap>
            <Button icon={<GithubOutlined />} href={APP_REPO} target="_blank">项目仓库</Button>
            <Button icon={<BookOutlined />} href={APP_DOCS_PATH} target="_blank">API 文档</Button>
            <Button href={APP_RELEASES} target="_blank">发布版本</Button>
          </Space>
        </Card>
      </Space>
    </PageCard>
  );
}
