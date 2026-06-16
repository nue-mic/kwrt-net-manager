import { useEffect, useRef, useState } from 'react';
import { App, Button, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ReloadOutlined } from '@ant-design/icons';
import PageCard from '../../components/PageCard';
import { extractErr } from '../../hooks/useNetData';
import * as ipv6 from '../../api/ipv6';

const { Text } = Typography;

const POLL_MS = 5000;

/** 字节数 → 人类可读（B/KB/MB/GB/TB）。 */
function fmtBytes(n: number): string {
  if (!n || n < 0) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i ? 1 : 0)} ${u[i]}`;
}

/** 速率（字节/秒）→ 人类可读（KB/s 起，自动进位）。 */
function fmtRate(bps: number): string {
  if (!bps || bps <= 0) return '0 KB/s';
  const kb = bps / 1024;
  if (kb < 1024) return `${kb.toFixed(1)} KB/s`;
  const mb = kb / 1024;
  if (mb < 1024) return `${mb.toFixed(2)} MB/s`;
  return `${(mb / 1024).toFixed(2)} GB/s`;
}

interface RateRow extends ipv6.LineV6 {
  up_rate: number; // 字节/秒
  down_rate: number; // 字节/秒
}

interface Sample {
  up: number;
  down: number;
  t: number; // 毫秒时间戳
}

export default function Ipv6LineDetail() {
  const { message } = App.useApp();
  const [rows, setRows] = useState<RateRow[]>([]);
  const [loading, setLoading] = useState(true);
  // 上次采样：line → { up, down, t }，用于差分计算速率。
  const lastRef = useRef<Record<string, Sample>>({});

  const load = async () => {
    try {
      const list = await ipv6.listLinesV6();
      const now = Date.now();
      const next: Record<string, Sample> = {};
      const computed: RateRow[] = list.map((l) => {
        const prev = lastRef.current[l.line];
        next[l.line] = { up: l.total_up, down: l.total_down, t: now };
        let upRate = 0;
        let downRate = 0;
        if (prev) {
          const dt = (now - prev.t) / 1000;
          if (dt > 0) {
            upRate = Math.max(0, (l.total_up - prev.up) / dt);
            downRate = Math.max(0, (l.total_down - prev.down) / dt);
          }
        }
        // 后端若直接给出速率（字节/秒）则优先采用，否则用差分值。
        return {
          ...l,
          up_rate: l.up_bps > 0 ? l.up_bps : upRate,
          down_rate: l.down_bps > 0 ? l.down_bps : downRate,
        };
      });
      lastRef.current = next;
      setRows(computed);
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    let alive = true;
    const tick = async () => {
      if (alive) await load();
    };
    void tick();
    const timer = setInterval(tick, POLL_MS);
    return () => {
      alive = false;
      clearInterval(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const columns: ColumnsType<RateRow> = [
    { title: '线路', dataIndex: 'line', render: (v: string) => <Text strong>{v || '—'}</Text> },
    {
      title: '连接数',
      dataIndex: 'connections',
      align: 'right',
      render: (v: number) => (v ?? 0).toLocaleString(),
    },
    {
      title: '上行速率',
      dataIndex: 'up_rate',
      align: 'right',
      render: (v: number) => <Text type="success">↑ {fmtRate(v)}</Text>,
    },
    {
      title: '下行速率',
      dataIndex: 'down_rate',
      align: 'right',
      render: (v: number) => <Text style={{ color: '#1677ff' }}>↓ {fmtRate(v)}</Text>,
    },
    {
      title: '累计上行',
      dataIndex: 'total_up',
      align: 'right',
      render: (v: number) => fmtBytes(v),
    },
    {
      title: '累计下行',
      dataIndex: 'total_down',
      align: 'right',
      render: (v: number) => fmtBytes(v),
    },
  ];

  return (
    <PageCard
      breadcrumb={['网络设置', 'IPv6', 'IPv6线路详情']}
      title="IPv6线路详情"
      extra={
        <Space size="middle">
          <Tag color="green">自动刷新 5s</Tag>
          <Button icon={<ReloadOutlined />} onClick={() => load()} loading={loading}>
            刷新
          </Button>
        </Space>
      }
    >
      <Table
        rowKey="line"
        size="small"
        bordered
        loading={loading}
        dataSource={rows}
        columns={columns}
        pagination={false}
        scroll={{ x: 'max-content' }}
        locale={{ emptyText: '暂无 IPv6 线路' }}
      />
    </PageCard>
  );
}
