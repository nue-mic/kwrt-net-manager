import { useMemo, useState } from 'react';
import { App, Input, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import PageCard from '../../components/PageCard';
import { useNetData, extractErr } from '../../hooks/useNetData';
import * as ipv6 from '../../api/ipv6';

const { Text } = Typography;

// 秒 → HH:MM:SS
function fmtDuration(sec: number): string {
  if (!sec || sec <= 0) return '—';
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = Math.floor(sec % 60);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(h)}:${pad(m)}:${pad(s)}`;
}

const dash = (v: string) => (v ? v : '—');

export default function Ipv6Leases() {
  const { message } = App.useApp();
  const { data, loading, error } = useNetData(() => ipv6.listLeasesV6(), [] as ipv6.LeaseV6[], { pollMs: 5000 });
  const [kw, setKw] = useState('');

  if (error) message.error(extractErr(error));

  const filtered = useMemo(() => {
    const q = kw.trim().toLowerCase();
    if (!q) return data;
    return data.filter((r) =>
      [r.hostname, r.mac, r.ipv6_addr, r.duid, r.local_link]
        .some((f) => (f ?? '').toLowerCase().includes(q)),
    );
  }, [data, kw]);

  const columns: ColumnsType<ipv6.LeaseV6> = [
    { title: '主机名', dataIndex: 'hostname', key: 'hostname', width: 160, render: (v: string) => <Text strong>{dash(v)}</Text> },
    { title: 'Mac', dataIndex: 'mac', key: 'mac', width: 160, render: (v: string) => dash(v) },
    { title: '本地链接IPv6地址', dataIndex: 'local_link', key: 'local_link', width: 220, render: (v: string) => dash(v) },
    { title: '终端IPv6地址', dataIndex: 'ipv6_addr', key: 'ipv6_addr', width: 240, render: (v: string) => dash(v) },
    { title: 'DUID', dataIndex: 'duid', key: 'duid', width: 240, render: (v: string) => dash(v) },
    { title: '接口', dataIndex: 'interface', key: 'interface', width: 100, render: (v: string) => dash(v) },
    { title: '有效时间', dataIndex: 'valid_seconds', key: 'valid_seconds', width: 120, render: (v: number) => fmtDuration(v) },
    { title: '类型', dataIndex: 'static', key: 'static', width: 100, render: (v: boolean) => (v ? <Tag color="blue">前缀静态</Tag> : <Tag>动态</Tag>) },
    { title: '备注', dataIndex: 'remark', key: 'remark', render: (v: string) => (v ? v : <Text type="secondary">—</Text>) },
  ];

  return (
    <PageCard
      breadcrumb={['网络设置', 'IPv6', 'DHCPv6终端']}
      title="DHCPv6终端"
      extra={
        <Input.Search
          allowClear
          placeholder="Mac/IPv6地址/DUID"
          style={{ width: 240 }}
          value={kw}
          onChange={(e) => setKw(e.target.value)}
          onSearch={(v) => setKw(v)}
        />
      }
    >
      <Table
        rowKey={(r) => `${r.duid}-${r.ipv6_addr}-${r.mac}`}
        size="small"
        bordered
        loading={loading}
        dataSource={filtered}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
        scroll={{ x: 'max-content' }}
        locale={{ emptyText: '暂无 DHCPv6 租约（若本机 LAN 未开启 DHCPv6 服务端，则不会有 v6 租约）' }}
      />
    </PageCard>
  );
}
