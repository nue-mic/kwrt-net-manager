import { useState } from 'react';
import { App, Button, Input, Popconfirm, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import PageCard from '../../components/PageCard';
import { useNetData, extractErr } from '../../hooks/useNetData';
import * as ipv6 from '../../api/ipv6';

const { Text } = Typography;

// 邻居状态中文化 + Tag 着色（对齐爱快）。
const STATE_META: Record<string, { txt: string; color: string }> = {
  REACHABLE: { txt: '可达', color: 'success' },
  STALE: { txt: '陈旧', color: 'default' },
  DELAY: { txt: '延迟', color: 'processing' },
  PROBE: { txt: '探测', color: 'warning' },
  FAILED: { txt: '失败', color: 'error' },
  PERMANENT: { txt: '永久', color: 'blue' },
  NOARP: { txt: '无ARP', color: 'default' },
  INCOMPLETE: { txt: '未完成', color: 'default' },
};

export default function Ipv6Neighbors() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData(() => ipv6.listNeighborsV6(), [] as ipv6.NeighborV6[]);
  const [keyword, setKeyword] = useState('');

  const onDelete = async (record: ipv6.NeighborV6) => {
    try {
      await ipv6.deleteNeighborV6(record.ipv6, record.interface);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onFlush = async () => {
    try {
      await ipv6.flushNeighborsV6();
      message.success('已全部清空');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const kw = keyword.trim().toLowerCase();
  const filtered = kw
    ? data.filter(
        (n) => n.mac.toLowerCase().includes(kw) || n.ipv6.toLowerCase().includes(kw),
      )
    : data;

  const columns: ColumnsType<ipv6.NeighborV6> = [
    { title: '终端MAC', dataIndex: 'mac', key: 'mac', width: 200, render: (v: string) => v || '—' },
    { title: 'IPv6地址', dataIndex: 'ipv6', key: 'ipv6', render: (v: string) => <Text copyable={!!v}>{v || '—'}</Text> },
    { title: '接口', dataIndex: 'interface', key: 'interface', width: 120, render: (v: string) => v || '—' },
    {
      title: '状态',
      dataIndex: 'state',
      key: 'state',
      width: 120,
      render: (v: string) => {
        const m = STATE_META[v] ?? { txt: v || '未知', color: 'default' };
        return <Tag color={m.color}>{m.txt}</Tag>;
      },
    },
    {
      title: '备注',
      dataIndex: 'remark',
      key: 'remark',
      render: (v: string) => v || <Text type="secondary">-</Text>,
    },
    {
      title: '操作',
      key: 'action',
      width: 100,
      fixed: 'right',
      render: (_, record) => (
        <Popconfirm title="确认删除该邻居？" onConfirm={() => onDelete(record)}>
          <Typography.Link type="danger">删除</Typography.Link>
        </Popconfirm>
      ),
    },
  ];

  return (
    <PageCard
      breadcrumb={['网络设置', 'IPv6', '邻居列表']}
      title="邻居列表"
      extra={
        <Popconfirm title="确认清空全部邻居？" onConfirm={onFlush}>
          <Button danger>全部清空</Button>
        </Popconfirm>
      }
      toolbar={
        <Space>
          <Input.Search
            allowClear
            placeholder="MAC/IPv6地址"
            style={{ width: 240 }}
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            onSearch={setKeyword}
          />
        </Space>
      }
    >
      <Table
        rowKey={(r) => `${r.interface}/${r.ipv6}`}
        size="small"
        bordered
        loading={loading}
        dataSource={filtered}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
        scroll={{ x: 'max-content' }}
        locale={{ emptyText: '暂无 IPv6 邻居' }}
      />
    </PageCard>
  );
}
