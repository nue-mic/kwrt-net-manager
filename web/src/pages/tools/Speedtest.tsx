import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Alert, App, Badge, Button, Card, Drawer, Empty, Input, Popconfirm, Space, Table, Tag, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ThunderboltOutlined, DashboardOutlined, ReloadOutlined } from '@ant-design/icons';
import { LineChart, Line, XAxis, YAxis, Tooltip as RTooltip, ResponsiveContainer, CartesianGrid, Legend } from 'recharts';
import PageCard from '../../components/PageCard';
import { extractErr } from '../../hooks/useNetData';
import * as st from '../../api/speedtest';

const { Text } = Typography;
const MAX_NODES = 8;

const fmtMbps = (v: number, done: boolean) => (done ? v.toFixed(2) : '—');
const pingText = (v: number) => (v < 0 ? 'Timeout' : `${v.toFixed(0)}ms`);

const nodeStatusTag = (n: st.SpeedtestNode) => {
  switch (n.status) {
    case 'done':
      return <Tag color="success">完成</Tag>;
    case 'testing':
      return <Badge status="processing" text="测试中" />;
    case 'failed':
      return (
        <Tooltip title={n.error || '失败'}>
          <Tag color="error">失败</Tag>
        </Tooltip>
      );
    default:
      return <Tag>待测</Tag>;
  }
};

export default function SpeedtestPage() {
  const { message } = App.useApp();
  const [svc, setSvc] = useState<st.SpeedtestSvcInfo | null>(null);
  const [status, setStatus] = useState<st.SpeedtestStatus | null>(null);
  const [servers, setServers] = useState<st.SpeedtestServer[]>([]);
  const [isp, setIsp] = useState('');
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [history, setHistory] = useState<st.SpeedtestHistoryEntry[]>([]);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [kw, setKw] = useState('');
  const [installing, setInstalling] = useState(false);
  const [starting, setStarting] = useState(false);
  const timer = useRef<number | null>(null);

  const loadSvc = async () => {
    try {
      setSvc(await st.getSpeedtestService());
    } catch {
      /* 忽略 */
    }
  };
  const loadServers = async () => {
    try {
      const resp = await st.getSpeedtestServers();
      setServers(resp.items);
      setIsp(resp.isp);
      // 默认勾选 recommended（仅在用户尚未选过时）。
      setSelectedIds((prev) => (prev.length ? prev : resp.items.filter((s) => s.recommended).map((s) => s.id)));
    } catch {
      /* 忽略：未装或离线 */
    }
  };
  const loadHistory = async () => {
    try {
      setHistory(await st.getSpeedtestHistory());
    } catch {
      /* 忽略 */
    }
  };

  // running 时 1.5s 轮询 status；结束即停并刷新历史/组件状态。
  const poll = useCallback(async () => {
    try {
      const s = await st.getSpeedtestStatus();
      setStatus(s);
      if (s.running) {
        if (!timer.current) timer.current = window.setInterval(() => void poll(), 1500);
      } else if (timer.current) {
        window.clearInterval(timer.current);
        timer.current = null;
        void loadHistory();
        void loadSvc();
        void loadServers();
      }
    } catch {
      /* 忽略 */
    }
  }, []);

  useEffect(() => {
    void loadSvc();
    void loadServers();
    void loadHistory();
    void poll();
    return () => {
      if (timer.current) window.clearInterval(timer.current);
    };
  }, [poll]);

  const onRun = async () => {
    setStarting(true);
    try {
      const s = await st.runSpeedtest(selectedIds);
      setStatus(s);
      void poll();
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setStarting(false);
    }
  };

  const onInstall = async () => {
    setInstalling(true);
    try {
      await st.installSpeedtest();
      message.success('测速组件安装完成');
      void loadSvc();
      void loadServers();
    } catch (e) {
      message.error('安装失败：' + extractErr(e));
    } finally {
      setInstalling(false);
    }
  };

  const onClearHistory = async () => {
    try {
      await st.clearSpeedtestHistory();
      setHistory([]);
      message.success('历史已清空');
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const uninstalled = svc !== null && !svc.installed;
  const running = !!status?.running;
  const nodes = status?.nodes ?? [];
  const bestDl = useMemo(() => Math.max(0, ...nodes.filter((n) => n.status === 'done').map((n) => n.download_mbps)), [nodes]);

  // 趋势图数据：历史最新在前，反转为时间正序，取近 20 条。
  const trend = useMemo(
    () =>
      history
        .slice(0, 20)
        .reverse()
        .map((h) => ({
          time: h.time.slice(5, 16), // MM-DD HH:mm
          下载: Math.round(h.best_download_mbps * 10) / 10,
          上传: Math.round(h.best_upload_mbps * 10) / 10,
        })),
    [history],
  );

  const filteredServers = useMemo(() => {
    const q = kw.trim().toLowerCase();
    if (!q) return servers;
    return servers.filter((s) => `${s.name} ${s.sponsor} ${s.id}`.toLowerCase().includes(q));
  }, [servers, kw]);

  const resultColumns: ColumnsType<st.SpeedtestNode> = [
    { title: '节点', dataIndex: 'name', key: 'name', render: (v: string) => v || '—' },
    { title: '运营商', dataIndex: 'sponsor', key: 'sponsor', width: 180, render: (v: string) => v || '—' },
    {
      title: '下载(Mbps)',
      dataIndex: 'download_mbps',
      key: 'dl',
      width: 120,
      render: (v: number, n) => {
        const done = n.status === 'done';
        const isBest = done && v === bestDl && bestDl > 0;
        return <Text strong={isBest} type={isBest ? 'success' : undefined}>{fmtMbps(v, done)}</Text>;
      },
    },
    { title: '上传(Mbps)', dataIndex: 'upload_mbps', key: 'ul', width: 120, render: (v: number, n) => fmtMbps(v, n.status === 'done') },
    { title: '延迟(ms)', dataIndex: 'ping_ms', key: 'ping', width: 100, render: (v: number, n) => (n.status === 'done' ? v.toFixed(1) : '—') },
    { title: '抖动(ms)', dataIndex: 'jitter_ms', key: 'jitter', width: 100, render: (v: number, n) => (n.status === 'done' ? v.toFixed(1) : '—') },
    { title: '状态', key: 'status', width: 100, render: (_, n) => nodeStatusTag(n) },
  ];

  const pickerColumns: ColumnsType<st.SpeedtestServer> = [
    { title: '节点', dataIndex: 'name', key: 'name' },
    { title: '运营商', dataIndex: 'sponsor', key: 'sponsor', width: 200 },
    { title: '距离', dataIndex: 'distance_km', key: 'dist', width: 100, render: (v: number) => `${v.toFixed(0)}km` },
    {
      title: '延迟',
      dataIndex: 'ping_ms',
      key: 'ping',
      width: 100,
      render: (v: number) => (v < 0 ? <Text type="secondary">Timeout</Text> : pingText(v)),
    },
    { title: '推荐', dataIndex: 'recommended', key: 'rec', width: 70, render: (v: boolean) => (v ? <Tag color="blue">荐</Tag> : null) },
  ];

  const historyColumns: ColumnsType<st.SpeedtestHistoryEntry> = [
    { title: '时间', dataIndex: 'time', key: 'time', width: 170 },
    { title: '最优节点', dataIndex: 'best_node', key: 'best', render: (v: string) => v || '—' },
    { title: '最高下载', dataIndex: 'best_download_mbps', key: 'dl', width: 120, render: (v: number) => `${v.toFixed(2)} Mbps` },
    { title: '最高上传', dataIndex: 'best_upload_mbps', key: 'ul', width: 120, render: (v: number) => `${v.toFixed(2)} Mbps` },
    { title: '最低延迟', dataIndex: 'min_ping_ms', key: 'ping', width: 110, render: (v: number) => (v > 0 ? `${v.toFixed(1)} ms` : '—') },
  ];

  return (
    <PageCard
      breadcrumb={['应用工具', '线路测速']}
      title="线路测速"
      toolbar={
        <Space wrap>
          {uninstalled && (
            <Tooltip title="也可直接「开始测速」，未装时会自动安装">
              <Button icon={<ThunderboltOutlined />} loading={installing} onClick={onInstall}>
                一键安装测速组件
              </Button>
            </Tooltip>
          )}
          <Button disabled={uninstalled} onClick={() => setPickerOpen(true)}>
            选择节点{selectedIds.length ? `（已选 ${selectedIds.length}）` : ''}
          </Button>
          <Button type="primary" icon={<DashboardOutlined />} loading={starting} disabled={running} onClick={onRun}>
            {running ? '测速中…' : uninstalled ? '开始测速（将先自动安装）' : '开始测速'}
          </Button>
        </Space>
      }
    >
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="多节点线路测速：列出附近 speedtest.net 节点，挨个测下载/上传/延迟/抖动并出对比表。未装 speedtest-go 会自动安装；旁路由测的是经主路由的共享上行带宽。"
      />

      {/* 运行阶段提示 */}
      {running && (
        <Alert
          type="warning"
          showIcon
          icon={<ReloadOutlined spin />}
          style={{ marginBottom: 12 }}
          message={status?.message || '测速中…'}
          description={status?.started_at ? `开始于 ${status.started_at}` : undefined}
        />
      )}
      {!running && status?.phase === 'error' && status?.error && (
        <Alert type="error" showIcon style={{ marginBottom: 12 }} message="测速失败" description={status.error} />
      )}

      {/* 本次结果对比表 */}
      {nodes.length > 0 ? (
        <Card size="small" title={`本次结果${isp ? `（本机运营商：${isp}）` : ''}`} style={{ marginBottom: 16 }}>
          <Table rowKey="id" size="small" bordered pagination={false} dataSource={nodes} columns={resultColumns} scroll={{ x: 'max-content' }} />
        </Card>
      ) : (
        !running && (
          <Card size="small" style={{ marginBottom: 16 }}>
            <Empty description="点击右上角「开始测速」开始多节点线路测速" />
          </Card>
        )
      )}

      {/* 历史趋势图 + 历史表 */}
      <Card
        size="small"
        title="历史趋势"
        extra={
          history.length > 0 && (
            <Popconfirm title="确认清空全部测速历史？" onConfirm={onClearHistory}>
              <Button size="small" danger>
                清空历史
              </Button>
            </Popconfirm>
          )
        }
      >
        {trend.length > 0 ? (
          <div style={{ width: '100%', height: 240 }}>
            <ResponsiveContainer>
              <LineChart data={trend} margin={{ top: 8, right: 16, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" opacity={0.3} />
                <XAxis dataKey="time" fontSize={12} />
                <YAxis fontSize={12} unit=" Mbps" width={70} />
                <RTooltip />
                <Legend />
                <Line type="monotone" dataKey="下载" stroke="#52c41a" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="上传" stroke="#1677ff" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        ) : (
          <Empty description="暂无历史记录" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        )}
        {history.length > 0 && (
          <Table
            rowKey="time"
            size="small"
            bordered
            style={{ marginTop: 12 }}
            dataSource={history}
            columns={historyColumns}
            pagination={{ pageSize: 10, showTotal: (t) => `共 ${t} 次` }}
            scroll={{ x: 'max-content' }}
          />
        )}
      </Card>

      {/* 节点选择抽屉 */}
      <Drawer
        title="选择测速节点"
        width="min(94vw, 720px)"
        open={pickerOpen}
        onClose={() => setPickerOpen(false)}
        extra={
          <Space>
            <Text type="secondary">
              已选 {selectedIds.length} / 上限 {MAX_NODES}
            </Text>
            <Button type="primary" onClick={() => setPickerOpen(false)}>
              确定
            </Button>
          </Space>
        }
      >
        <Space style={{ marginBottom: 12 }}>
          <Input.Search allowClear placeholder="搜索 城市/运营商/ID" style={{ width: 260 }} value={kw} onChange={(e) => setKw(e.target.value)} />
          <Button size="small" onClick={() => setSelectedIds(servers.filter((s) => s.recommended).map((s) => s.id))}>
            重置为推荐
          </Button>
        </Space>
        <Table
          rowKey="id"
          size="small"
          bordered
          dataSource={filteredServers}
          columns={pickerColumns}
          pagination={{ pageSize: 12, showTotal: (t) => `共 ${t} 个节点` }}
          rowSelection={{
            selectedRowKeys: selectedIds,
            onChange: (keys) => {
              const next = keys as string[];
              if (next.length > MAX_NODES) {
                message.warning(`最多选 ${MAX_NODES} 个节点`);
                return;
              }
              setSelectedIds(next);
            },
            getCheckboxProps: (s) => ({ disabled: !s.reachable && !selectedIds.includes(s.id) }),
          }}
        />
      </Drawer>
    </PageCard>
  );
}
