import { useEffect, useState } from 'react';
import { Alert, App, Button, Card, Popconfirm, Space, Statistic, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ReloadOutlined } from '@ant-design/icons';
import PageCard from '../../components/PageCard';
import { extractErr } from '../../hooks/useNetData';
import * as dns from '../../api/dns';

interface Row {
  key: string;
  metric: string;
  value: string;
}

export default function DnsCacheStatusPage() {
  const { message } = App.useApp();
  const [stats, setStats] = useState<dns.DNSCacheStats | null>(null);
  const [loading, setLoading] = useState(false);
  const [flushing, setFlushing] = useState(false);

  const load = async () => {
    setLoading(true);
    try {
      setStats(await dns.getDNSCacheStats());
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
    const id = setInterval(() => void load(), 60000); // 1 分钟刷新（同爱快）
    return () => clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onFlush = async () => {
    setFlushing(true);
    try {
      await dns.flushDNSCache();
      message.success('已清空 DNS 缓存');
      void load();
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setFlushing(false);
    }
  };

  const total = stats ? stats.hits + stats.misses : 0;
  const rows: Row[] = stats
    ? [
        { key: '1', metric: '请求 DNS 次数（累计）', value: String(total) },
        { key: '2', metric: '缓存命中次数', value: String(stats.hits) },
        { key: '3', metric: '未命中次数', value: String(stats.misses) },
        { key: '4', metric: '命中比例', value: (stats.hit_ratio * 100).toFixed(2) + '%' },
        { key: '5', metric: '缓存容量', value: String(stats.cache_size) },
        { key: '6', metric: '缓存插入数', value: String(stats.insertions) },
        { key: '7', metric: '缓存淘汰数', value: String(stats.evictions) },
      ]
    : [];

  const columns: ColumnsType<Row> = [
    { title: '指标', dataIndex: 'metric' },
    { title: '数值', dataIndex: 'value', width: 220, render: (v: string) => <Typography.Text strong>{v}</Typography.Text> },
  ];

  return (
    <PageCard
      breadcrumb={['网络设置', 'DNS 设置', 'DNS 缓存状态']}
      title="DNS 缓存状态"
      toolbar={
        <Space>
          <Button icon={<ReloadOutlined />} loading={loading} onClick={() => void load()}>
            刷新
          </Button>
          <Popconfirm title="确认清空 DNS 缓存？" onConfirm={onFlush}>
            <Button danger loading={flushing}>
              清除所有数据
            </Button>
          </Popconfirm>
        </Space>
      }
    >
      {stats && !stats.supported && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="无法读取 dnsmasq 缓存统计（CHAOS 查询不可用，或为 store 开发后端）。"
        />
      )}
      {stats && stats.supported && (
        <Space size="large" style={{ marginBottom: 16 }} wrap>
          <Card size="small">
            <Statistic title="累计请求" value={total} />
          </Card>
          <Card size="small">
            <Statistic title="命中比例" value={(stats.hit_ratio * 100).toFixed(2)} suffix="%" valueStyle={{ color: '#3f8600' }} />
          </Card>
          <Card size="small">
            <Statistic title="缓存容量" value={stats.cache_size} />
          </Card>
        </Space>
      )}
      <Table rowKey="key" size="small" bordered loading={loading} dataSource={rows} columns={columns} pagination={false} style={{ maxWidth: 560 }} />
      <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
        说明：统计为 dnsmasq 进程启动以来的累计值（重启清零）；「昨日/今日」分日维度需后端定时采样，列为后续增强。
      </Typography.Paragraph>
    </PageCard>
  );
}
