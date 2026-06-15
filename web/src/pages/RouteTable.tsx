import { useCallback, useEffect, useState } from 'react';
import { App, Button, Table, Tabs } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ReloadOutlined } from '@ant-design/icons';
import PageCard from '../components/PageCard';
import { extractErr } from '../hooks/useNetData';
import * as net from '../api/netcfg';

type Family = 'ipv4' | 'ipv6';

const columns: ColumnsType<net.RouteEntry> = [
  { title: '线路', dataIndex: 'interface', key: 'interface' },
  { title: '目的地址', dataIndex: 'target', key: 'target' },
  { title: '子网掩码', dataIndex: 'netmask', key: 'netmask' },
  { title: '网关', dataIndex: 'gateway', key: 'gateway' },
  { title: '优先级', dataIndex: 'metric', key: 'metric' },
];

export default function RouteTablePage() {
  const { message } = App.useApp();
  const [family, setFamily] = useState<Family>('ipv4');
  const [rows, setRows] = useState<net.RouteEntry[]>([]);
  const [loading, setLoading] = useState(false);

  const load = useCallback(
    async (fam: Family) => {
      setLoading(true);
      try {
        const items = await net.getRouteTable(fam);
        setRows(items);
      } catch (e) {
        message.error(extractErr(e));
      } finally {
        setLoading(false);
      }
    },
    [message],
  );

  useEffect(() => {
    void load(family);
  }, [family, load]);

  return (
    <PageCard
      breadcrumb={['网络设置', '静态路由', '当前路由表']}
      title="当前路由表"
      extra={
        <Button icon={<ReloadOutlined />} loading={loading} onClick={() => void load(family)}>
          刷新
        </Button>
      }
    >
      <Tabs
        activeKey={family}
        onChange={(k) => setFamily(k as Family)}
        items={[
          { key: 'ipv4', label: 'IPv4' },
          { key: 'ipv6', label: 'IPv6' },
        ]}
      />
      <Table
        rowKey={(_, i) => String(i)}
        size="small"
        bordered
        loading={loading}
        dataSource={rows}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
      />
    </PageCard>
  );
}
