import { useCallback, useState } from 'react';
import { Button, Card, Descriptions, Divider, Drawer, Space, Spin, Table, Tag, Typography } from 'antd';
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

// 速率：Mb/s → 友好显示（≥1000 用 Gb/s）。
function fmtSpeed(s: number): string {
  if (!s || s <= 0) return '—';
  return s >= 1000 ? `${(s / 1000).toFixed(0)} Gb/s` : `${s} Mb/s`;
}

// 数字千分位（包数/错误数等大整数更易读）；非有限值兜底「—」。
function fmtNum(n: number): string {
  if (n == null || !Number.isFinite(n)) return '—';
  return n.toLocaleString('en-US');
}

// 通用文本兜底：空字符串/undefined/null → 「—」。
function dash(v: string | number | null | undefined): string {
  if (v == null) return '—';
  const s = String(v).trim();
  return s === '' ? '—' : s;
}

const kindLabel: Record<string, { txt: string; color: string }> = {
  physical: { txt: '物理网卡', color: 'blue' },
  bridge: { txt: '网桥', color: 'purple' },
  vlan: { txt: 'VLAN', color: 'cyan' },
  wifi: { txt: 'Wi-Fi', color: 'gold' },
  virtual: { txt: '虚拟', color: 'default' },
};

function kindTag(k: string) {
  const m = kindLabel[k] ?? { txt: k || '—', color: 'default' };
  return <Tag color={m.color}>{m.txt}</Tag>;
}

function roleTag(role: string, bound: string) {
  if (!bound) return <Text type="secondary">空闲</Text>;
  return (
    <Space size={4}>
      <Tag color={role === 'wan' ? 'volcano' : 'green'}>{role === 'wan' ? '外网' : '内网'}</Tag>
      {bound}
    </Space>
  );
}

function duplexLabel(v: string): string {
  if (!v) return '—';
  return v === 'full' ? '全双工' : v === 'half' ? '半双工' : v;
}

function autonegLabel(v: string): string {
  if (!v) return '—';
  return v === 'on' ? '开' : v === 'off' ? '关' : v;
}

// ---------- 详情抽屉 ----------

function NICDetailDrawer({ name, open, onClose }: { name: string | null; open: boolean; onClose: () => void }) {
  const [detail, setDetail] = useState<net.NICDetail | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async (nm: string) => {
    setLoading(true);
    try {
      const d = await net.getNICDetail(nm);
      setDetail(d);
    } finally {
      setLoading(false);
    }
  }, []);

  // 抽屉打开/切换网卡时拉取；关闭时清空。
  const afterOpenChange = useCallback(
    (o: boolean) => {
      if (o && name) {
        setDetail(null);
        void load(name);
      } else if (!o) {
        setDetail(null);
      }
    },
    [name, load],
  );

  const d = detail;
  const linkOnline = !!d?.up;
  const addrs = d?.addrs ?? [];
  const fallbackAddrs = d?.ip_addrs ?? [];
  const bridgePorts = d?.bridge_ports ?? [];
  const supported = d?.supported_modes ?? [];
  const advertised = d?.advertised_modes ?? [];

  return (
    <Drawer
      title={name ? `网卡详情 · ${name}` : '网卡详情'}
      open={open}
      onClose={onClose}
      afterOpenChange={afterOpenChange}
      width="min(92vw, 720px)"
      destroyOnHidden
      extra={
        <Button
          icon={<ReloadOutlined />}
          size="small"
          loading={loading}
          disabled={!name}
          onClick={() => name && load(name)}
        >
          刷新
        </Button>
      }
    >
      <Spin spinning={loading}>
        {!d ? (
          <Text type="secondary">{loading ? '加载中…' : '暂无数据'}</Text>
        ) : (
          <>
            {/* 基本信息 */}
            <Card size="small" title="基本信息" styles={{ body: { padding: 0 } }}>
              <Descriptions bordered size="small" column={{ xs: 1, sm: 1, md: 2 }}>
                <Descriptions.Item label="网卡名">
                  <Text strong>{dash(d.name)}</Text>
                </Descriptions.Item>
                <Descriptions.Item label="类型">{kindTag(d.kind)}</Descriptions.Item>
                <Descriptions.Item label="链路状态">
                  <Space size={6}>
                    {linkOnline ? <Tag color="success">已连接</Tag> : <Tag>未连接</Tag>}
                    <Text type="secondary">{dash(d.operstate)}</Text>
                  </Space>
                </Descriptions.Item>
                <Descriptions.Item label="角色 / 绑定接口">{roleTag(d.role, d.bound)}</Descriptions.Item>
                <Descriptions.Item label="MTU">{dash(d.mtu)}</Descriptions.Item>
                <Descriptions.Item label="ifindex">{dash(d.ifindex)}</Descriptions.Item>
                <Descriptions.Item label="别名(alias)">{dash(d.ifalias)}</Descriptions.Item>
              </Descriptions>
            </Card>

            <Divider style={{ margin: '16px 0' }} />

            {/* 链路 */}
            <Card size="small" title="链路" styles={{ body: { padding: 0 } }}>
              <Descriptions bordered size="small" column={{ xs: 1, sm: 1, md: 2 }}>
                <Descriptions.Item label="速率">{fmtSpeed(d.speed_mb)}</Descriptions.Item>
                <Descriptions.Item label="双工">{duplexLabel(d.duplex)}</Descriptions.Item>
                <Descriptions.Item label="自协商">{autonegLabel(d.autoneg)}</Descriptions.Item>
                <Descriptions.Item label="端口类型">{dash(d.port)}</Descriptions.Item>
                <Descriptions.Item label="carrier 变化次数">{dash(d.carrier_changes)}</Descriptions.Item>
                <Descriptions.Item label="发送队列长度">{dash(d.tx_queue_len)}</Descriptions.Item>
                <Descriptions.Item label="支持速率模式" span={2}>
                  {supported.length ? (
                    <Space size={[4, 4]} wrap>
                      {supported.map((m) => (
                        <Tag key={m}>{m}</Tag>
                      ))}
                    </Space>
                  ) : (
                    '—'
                  )}
                </Descriptions.Item>
                <Descriptions.Item label="广播速率模式" span={2}>
                  {advertised.length ? (
                    <Space size={[4, 4]} wrap>
                      {advertised.map((m) => (
                        <Tag key={m}>{m}</Tag>
                      ))}
                    </Space>
                  ) : (
                    '—'
                  )}
                </Descriptions.Item>
              </Descriptions>
            </Card>

            <Divider style={{ margin: '16px 0' }} />

            {/* 硬件 / 驱动 */}
            <Card size="small" title="硬件 / 驱动" styles={{ body: { padding: 0 } }}>
              <Descriptions bordered size="small" column={{ xs: 1, sm: 1, md: 2 }}>
                <Descriptions.Item label="当前 MAC">{dash(d.mac)}</Descriptions.Item>
                <Descriptions.Item label="永久 MAC">{dash(d.perm_mac)}</Descriptions.Item>
                <Descriptions.Item label="驱动">{dash(d.driver)}</Descriptions.Item>
                <Descriptions.Item label="驱动版本">{dash(d.driver_version)}</Descriptions.Item>
                <Descriptions.Item label="固件">{dash(d.firmware)}</Descriptions.Item>
                <Descriptions.Item label="总线信息">{dash(d.bus_info)}</Descriptions.Item>
              </Descriptions>
            </Card>

            <Divider style={{ margin: '16px 0' }} />

            {/* IP 地址 */}
            <Card size="small" title="IP 地址">
              {addrs.length ? (
                <Table<net.NICAddr>
                  rowKey={(r) => `${r.family}-${r.address}-${r.prefix}`}
                  size="small"
                  bordered
                  pagination={false}
                  dataSource={addrs}
                  columns={[
                    {
                      title: '协议',
                      dataIndex: 'family',
                      width: 80,
                      render: (f: string) => (
                        <Tag color={f === 'ipv6' ? 'geekblue' : 'blue'}>{f === 'ipv6' ? 'IPv6' : 'IPv4'}</Tag>
                      ),
                    },
                    { title: '地址', dataIndex: 'address', render: (v) => dash(v) },
                    { title: '前缀', dataIndex: 'prefix', width: 80, render: (v) => `/${v}` },
                    { title: '作用域', dataIndex: 'scope', width: 100, render: (v) => dash(v) },
                  ]}
                />
              ) : fallbackAddrs.length ? (
                <Space direction="vertical" size={4}>
                  {fallbackAddrs.map((a) => (
                    <Text key={a} code>
                      {a}
                    </Text>
                  ))}
                </Space>
              ) : (
                <Text type="secondary">—</Text>
              )}
            </Card>

            <Divider style={{ margin: '16px 0' }} />

            {/* 流量统计 */}
            <Card size="small" title="流量统计" styles={{ body: { padding: 0 } }}>
              <Descriptions bordered size="small" column={{ xs: 1, sm: 1, md: 2 }}>
                <Descriptions.Item label="接收字节">{fmtBytes(d.stats?.rx_bytes ?? 0)}</Descriptions.Item>
                <Descriptions.Item label="发送字节">{fmtBytes(d.stats?.tx_bytes ?? 0)}</Descriptions.Item>
                <Descriptions.Item label="接收包数">{fmtNum(d.stats?.rx_packets)}</Descriptions.Item>
                <Descriptions.Item label="发送包数">{fmtNum(d.stats?.tx_packets)}</Descriptions.Item>
                <Descriptions.Item label="接收错误">{fmtNum(d.stats?.rx_errors)}</Descriptions.Item>
                <Descriptions.Item label="发送错误">{fmtNum(d.stats?.tx_errors)}</Descriptions.Item>
                <Descriptions.Item label="接收丢弃">{fmtNum(d.stats?.rx_dropped)}</Descriptions.Item>
                <Descriptions.Item label="发送丢弃">{fmtNum(d.stats?.tx_dropped)}</Descriptions.Item>
                <Descriptions.Item label="多播">{fmtNum(d.stats?.multicast)}</Descriptions.Item>
                <Descriptions.Item label="冲突">{fmtNum(d.stats?.collisions)}</Descriptions.Item>
              </Descriptions>
            </Card>

            <Divider style={{ margin: '16px 0' }} />

            {/* 拓扑 */}
            <Card size="small" title="拓扑" styles={{ body: { padding: 0 } }}>
              <Descriptions bordered size="small" column={1}>
                <Descriptions.Item label="所属网桥">{dash(d.master)}</Descriptions.Item>
                <Descriptions.Item label="网桥成员">
                  {bridgePorts.length ? (
                    <Space size={[4, 4]} wrap>
                      {bridgePorts.map((p) => (
                        <Tag key={p}>{p}</Tag>
                      ))}
                    </Space>
                  ) : (
                    '—'
                  )}
                </Descriptions.Item>
                <Descriptions.Item label="VLAN">
                  {d.vlan_id ? `ID ${d.vlan_id}${d.vlan_proto ? ` · ${d.vlan_proto}` : ''}` : '—'}
                </Descriptions.Item>
              </Descriptions>
            </Card>
          </>
        )}
      </Spin>
    </Drawer>
  );
}

export default function NICs() {
  const { data, loading, reload } = useNetData<net.NIC[]>(() => net.listNICs(), [], { pollMs: 5000 });
  const [detailName, setDetailName] = useState<string | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);

  const openDetail = (name: string) => {
    setDetailName(name);
    setDrawerOpen(true);
  };

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
      render: (s: number) => fmtSpeed(s),
    },
    { title: '双工', dataIndex: 'duplex', render: (v) => duplexLabel(v) },
    { title: 'MTU', dataIndex: 'mtu' },
    {
      title: '类型',
      dataIndex: 'kind',
      render: (k: string) => kindTag(k),
    },
    {
      title: '绑定接口',
      dataIndex: 'bound',
      render: (b: string, r) => roleTag(r.role, b),
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
    {
      title: '操作',
      key: 'action',
      fixed: 'right',
      width: 80,
      render: (_, r) => (
        <Button type="link" size="small" onClick={() => openDetail(r.name)}>
          详情
        </Button>
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
      <NICDetailDrawer name={detailName} open={drawerOpen} onClose={() => setDrawerOpen(false)} />
    </PageCard>
  );
}
