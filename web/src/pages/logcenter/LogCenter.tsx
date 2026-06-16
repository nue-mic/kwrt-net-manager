import { useCallback, useEffect, useMemo, useState } from 'react';
import { App, Button, DatePicker, Input, Popconfirm, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { DownloadOutlined, ReloadOutlined, DeleteOutlined } from '@ant-design/icons';
import type { Dayjs } from 'dayjs';
import PageCard from '../../components/PageCard';
import { extractErr } from '../../hooks/useNetData';
import * as logs from '../../api/logs';

const { RangePicker } = DatePicker;

interface SourceMeta {
  title: string;
  breadcrumb: string[];
  columns: ColumnsType<logs.LogEntry>;
  clearable: boolean;
  note?: string;
}

const tagByLevel = (lv?: string) => {
  const m: Record<string, string> = { err: 'error', error: 'error', warn: 'warning', warning: 'warning', notice: 'blue', info: 'default', debug: 'default' };
  return m[(lv ?? '').toLowerCase()] ?? 'default';
};

const META: Record<logs.LogSource, SourceMeta> = {
  system: {
    title: '系统日志',
    breadcrumb: ['日志中心', '系统日志', '系统日志'],
    clearable: false,
    note: '来源：OpenWrt 系统日志（logread）。由系统维护，不支持清空。',
    columns: [
      { title: '时间', dataIndex: 'time', width: 170 },
      { title: '级别', dataIndex: 'level', width: 90, render: (v: string) => (v ? <Tag color={tagByLevel(v)}>{v}</Tag> : '-') },
      { title: '来源', dataIndex: 'proc', width: 160, render: (v: string) => v || '-' },
      { title: '事件', dataIndex: 'message', render: (v: string) => v || '-' },
    ],
  },
  operation: {
    title: '操作日志',
    breadcrumb: ['日志中心', '系统日志', '操作日志'],
    clearable: true,
    note: '来源：本工具审计——记录每次通过本面板的写操作（新增/修改/删除/启停）及客户端 IP。',
    columns: [
      { title: '时间', dataIndex: 'time', width: 170 },
      { title: '用户名', dataIndex: 'user', width: 110, render: (v: string) => v || '-' },
      { title: 'IP', dataIndex: 'client_ip', width: 150, render: (v: string) => v || '-' },
      { title: '功能', dataIndex: 'module', width: 150, render: (v: string) => v || '-' },
      { title: '事件', dataIndex: 'action', render: (v: string) => v || '-' },
    ],
  },
  dhcp: {
    title: 'DHCP日志',
    breadcrumb: ['日志中心', '功能日志', 'DHCP日志'],
    clearable: false,
    note: '来源：dnsmasq-dhcp / odhcpd（logread）。DHCP 服务端停用或无活动时此处可能为空。',
    columns: [
      { title: '时间', dataIndex: 'time', width: 170 },
      { title: '消息类型', dataIndex: 'type', width: 150, render: (v: string) => (v ? <Tag color="blue">{v}</Tag> : '-') },
      { title: '接口', dataIndex: 'iface', width: 90, render: (v: string) => v || '-' },
      { title: 'MAC地址', dataIndex: 'mac', width: 160, render: (v: string) => v || '-' },
      { title: 'IP地址', dataIndex: 'ip', width: 200, render: (v: string) => v || '-' },
      { title: '事件描述', dataIndex: 'message', render: (v: string) => v || '-' },
    ],
  },
  dialup: {
    title: '外网拨号日志',
    breadcrumb: ['日志中心', '功能日志', '外网拨号日志'],
    clearable: false,
    note: '来源：pppd / netifd / udhcpc（logread）。旁路由若无外网拨号（PPPoE/DHCP 客户端）此处可能为空。',
    columns: [
      { title: '时间', dataIndex: 'time', width: 170 },
      { title: '来源', dataIndex: 'proc', width: 140, render: (v: string) => v || '-' },
      { title: '事件', dataIndex: 'message', render: (v: string) => v || '-' },
    ],
  },
  ddns: {
    title: '动态域名日志',
    breadcrumb: ['日志中心', '功能日志', '动态域名日志'],
    clearable: false,
    note: '来源：ddns-scripts 日志（/var/log/ddns）。需先在「动态域名」启用配置。',
    columns: [
      { title: '时间', dataIndex: 'time', width: 170 },
      { title: '配置', dataIndex: 'proc', width: 180, render: (v: string) => v || '-' },
      { title: '事件', dataIndex: 'message', render: (v: string) => v || '-' },
    ],
  },
  arp: {
    title: 'ARP日志',
    breadcrumb: ['日志中心', '用户日志', 'ARP日志'],
    clearable: true,
    note: '来源：本工具轮询 ip neigh 差分，记录同一 IP 的 MAC 变化（疑似 ARP 欺骗/冲突）。',
    columns: [
      { title: '时间', dataIndex: 'time', width: 170 },
      { title: '类型', dataIndex: 'type', width: 120, render: (v: string) => (v ? <Tag color="warning">{v}</Tag> : '-') },
      { title: '接口', dataIndex: 'iface', width: 90, render: (v: string) => v || '-' },
      { title: 'IP', dataIndex: 'ip', width: 150, render: (v: string) => v || '-' },
      { title: 'MAC', dataIndex: 'mac', width: 160, render: (v: string) => v || '-' },
      { title: '事件', dataIndex: 'message', render: (v: string) => v || '-' },
    ],
  },
};

export default function LogCenter({ source }: { source: logs.LogSource }) {
  const { message } = App.useApp();
  const meta = META[source];
  const [data, setData] = useState<logs.LogEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [keyword, setKeyword] = useState('');
  const [range, setRange] = useState<[Dayjs | null, Dayjs | null] | null>(null);

  const query = useMemo<logs.LogQuery>(
    () => ({
      start: range?.[0] ? range[0].startOf('second').unix() : undefined,
      end: range?.[1] ? range[1].endOf('second').unix() : undefined,
      keyword: keyword.trim() || undefined,
      page,
      page_size: pageSize,
    }),
    [range, keyword, page, pageSize]
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const r = await logs.queryLogs(source, query);
      setData(r.items);
      setTotal(r.total);
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setLoading(false);
    }
  }, [source, query, message]);

  useEffect(() => {
    void load();
  }, [load]);

  // 切换日志源时重置筛选/分页。
  useEffect(() => {
    setPage(1);
    setKeyword('');
    setRange(null);
  }, [source]);

  const onClear = async () => {
    try {
      await logs.clearLogs(source);
      message.success('已清空');
      setPage(1);
      void load();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onExport = async () => {
    try {
      await logs.downloadLogs(source, query);
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  return (
    <PageCard
      breadcrumb={meta.breadcrumb}
      title={meta.title}
      toolbar={
        <>
          <Space wrap>
            <RangePicker showTime value={range as never} onChange={(v) => setRange(v as never)} placeholder={['开始时间', '结束时间']} />
            <Input.Search allowClear placeholder="搜索事件" style={{ width: 220 }} value={keyword} onChange={(e) => setKeyword(e.target.value)} onSearch={() => { setPage(1); void load(); }} />
          </Space>
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => void load()}>刷新</Button>
            <Button icon={<DownloadOutlined />} onClick={onExport}>导出</Button>
            {meta.clearable && (
              <Popconfirm title="确认清空该日志？" onConfirm={onClear}>
                <Button danger icon={<DeleteOutlined />}>全部清空</Button>
              </Popconfirm>
            )}
          </Space>
        </>
      }
    >
      {meta.note && (
        <Typography.Paragraph type="secondary" style={{ marginTop: -4, marginBottom: 12 }}>
          {meta.note}
        </Typography.Paragraph>
      )}
      <Table
        rowKey={(_, i) => String((page - 1) * pageSize + (i ?? 0))}
        size="small"
        bordered
        loading={loading}
        dataSource={data}
        columns={meta.columns}
        scroll={{ x: 'max-content' }}
        pagination={{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, ps) => {
            setPage(p);
            setPageSize(ps);
          },
        }}
      />
    </PageCard>
  );
}
