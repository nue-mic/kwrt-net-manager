import { Button, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ReloadOutlined } from '@ant-design/icons';
import PageCard from '../components/PageCard';
import { useNetData } from '../hooks/useNetData';
import * as net from '../api/netcfg';

const { Text } = Typography;

function fmtBytes(n: number): string {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i ? 1 : 0)} ${u[i]}`;
}

const kindLabel: Record<string, { txt: string; color: string }> = {
  physical: { txt: '物理网卡', color: 'blue' },
  bridge: { txt: '网桥', color: 'purple' },
  vlan: { txt: 'VLAN', color: 'cyan' },
  wifi: { txt: 'Wi-Fi', color: 'gold' },
  virtual: { txt: '虚拟', color: 'default' },
};

export default function NICs() {
  const { data, loading, reload } = useNetData<net.NIC[]>(() => net.listNICs(), [], { pollMs: 5000 });

  const columns: ColumnsType<net.NIC> = [
    { title: '网卡', dataIndex: 'name', render: (v) => <Text strong>{v}</Text> },
    { title: 'MAC', dataIndex: 'mac', render: (v) => v || '—' },
    {
      title: '链路',
      dataIndex: 'up',
      render: (up: boolean) => (up ? <Tag color="success">已连接</Tag> : <Tag>未连接</Tag>),
    },
    {
      title: '速率',
      dataIndex: 'speed_mb',
      render: (s: number) => (s > 0 ? (s >= 1000 ? `${(s / 1000).toFixed(0)} Gb/s` : `${s} Mb/s`) : '—'),
    },
    { title: '双工', dataIndex: 'duplex', render: (v) => (v ? (v === 'full' ? '全双工' : '半双工') : '—') },
    { title: 'MTU', dataIndex: 'mtu' },
    {
      title: '类型',
      dataIndex: 'kind',
      render: (k: string) => {
        const m = kindLabel[k] ?? { txt: k, color: 'default' };
        return <Tag color={m.color}>{m.txt}</Tag>;
      },
    },
    {
      title: '绑定接口',
      dataIndex: 'bound',
      render: (b: string, r) =>
        b ? (
          <Space size={4}>
            <Tag color={r.role === 'wan' ? 'volcano' : 'green'}>{r.role === 'wan' ? '外网' : '内网'}</Tag>
            {b}
          </Space>
        ) : (
          <Text type="secondary">空闲</Text>
        ),
    },
    {
      title: '收 / 发',
      key: 'traffic',
      render: (_, r) => (
        <Text type="secondary" style={{ fontSize: 12 }}>
          ↓{fmtBytes(r.rx_bytes)} / ↑{fmtBytes(r.tx_bytes)}
        </Text>
      ),
    },
  ];

  return (
    <PageCard
      breadcrumb={['网络设置', '内外网设置', '网卡列表']}
      title="网卡列表"
      extra={
        <Button icon={<ReloadOutlined />} onClick={() => reload()} loading={loading}>
          刷新
        </Button>
      }
    >
      <Table
        rowKey="name"
        size="small"
        bordered
        loading={loading}
        dataSource={data}
        columns={columns}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 个网卡` }}
        scroll={{ x: 'max-content' }}
      />
    </PageCard>
  );
}
