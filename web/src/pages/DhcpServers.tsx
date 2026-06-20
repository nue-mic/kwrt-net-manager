import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Alert,
  App,
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined, ThunderboltOutlined, ReloadOutlined } from '@ant-design/icons';
import PageCard from '../components/PageCard';
import { useNetData, extractErr } from '../hooks/useNetData';
import * as net from '../api/netcfg';

// 由接口 IP + 掩码推默认 DHCP 地址池起止。
// /24（255.255.255.0）时把末段换成 .100~.200；其余掩码退化为留空（让用户自填，避免给出错误范围）。
function defaultPool(ip: string, mask: string): { start: string; end: string } {
  const parts = (ip || '').split('.');
  if (parts.length === 4 && parts.every((p) => p !== '' && !isNaN(Number(p))) && mask === '255.255.255.0') {
    const prefix = parts.slice(0, 3).join('.');
    return { start: `${prefix}.100`, end: `${prefix}.200` };
  }
  return { start: '', end: '' };
}

// Drawer 表单内自定义 DHCP 选项的行结构（code 允许暂为空，保存时再过滤/转换）。
interface CustomOptionRow {
  code: number | null;
  value: string;
}

// Drawer 表单值类型，避免裸 any。
interface ServerFormValues {
  interface: string;
  ip_start: string;
  ip_end: string;
  force: boolean;
  exclude_text: string;
  netmask: string;
  gateway: string;
  dns_primary: string;
  dns_secondary: string;
  lease_value: number;
  lease_unit: 'm' | 'h' | 'd' | 'inf';
  custom_options: CustomOptionRow[];
}

// 租期分钟 ↔ 数值+单位 互转（让用户填「2 小时」而非「120 分钟」；0=永久）。
function minutesToUnit(min: number): { lease_value: number; lease_unit: 'm' | 'h' | 'd' | 'inf' } {
  if (min <= 0) return { lease_value: 0, lease_unit: 'inf' };
  if (min % 1440 === 0) return { lease_value: min / 1440, lease_unit: 'd' };
  if (min % 60 === 0) return { lease_value: min / 60, lease_unit: 'h' };
  return { lease_value: min, lease_unit: 'm' };
}
function unitToMinutes(value: number, unit: string): number {
  if (unit === 'inf') return 0; // 永久
  const f = unit === 'h' ? 60 : unit === 'd' ? 1440 : 1;
  return Math.max(0, Math.round((value || 0) * f));
}

// 常用自定义 DHCP 选项预设：选了自动带出选项号，免得记数字。
const COMMON_OPTIONS = [
  { code: 42, name: 'NTP 时间服务器' },
  { code: 15, name: '域名后缀 (Domain)' },
  { code: 119, name: '搜索域 (Search Domain)' },
  { code: 44, name: 'WINS 服务器' },
  { code: 66, name: 'TFTP 服务器 (PXE)' },
  { code: 67, name: '启动文件名 (PXE)' },
  { code: 252, name: 'WPAD 自动代理' },
  { code: 26, name: '接口 MTU' },
];

export default function DhcpServersPage() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<net.DHCPServer[]>(() => net.listServers(), []);

  const [status, setStatus] = useState<net.NetStatus | null>(null);
  const [svc, setSvc] = useState<net.DHCPSvcInfo | null>(null);
  const [installing, setInstalling] = useState(false);
  const [interfaces, setInterfaces] = useState<net.NetInterface[]>([]);
  const [keyword, setKeyword] = useState('');
  const [selected, setSelected] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<net.DHCPServer | null>(null);
  const [saving, setSaving] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [batching, setBatching] = useState(false);
  const [routePush, setRoutePush] = useState<net.RoutePushMode>('off');
  const [form] = Form.useForm<ServerFormValues>();
  const [searchParams, setSearchParams] = useSearchParams();

  useEffect(() => {
    let alive = true;
    void (async () => {
      try {
        const [s, ifs] = await Promise.all([net.getStatus(), net.listInterfaces()]);
        if (!alive) return;
        setStatus(s);
        setInterfaces(ifs);
      } catch (e) {
        if (alive) message.error(extractErr(e));
      }
      try {
        const m = await net.getRoutePushMode();
        if (alive) setRoutePush(m);
      } catch {
        /* 可选，失败静默 */
      }
      try {
        const info = await net.getDHCPService();
        if (alive) setSvc(info);
      } catch {
        /* 服务信息可选，失败静默 */
      }
    })();
    return () => {
      alive = false;
    };
  }, [message]);

  // 服务端列表变化（启用/停用/新增/删除/批量后 reload）即同步刷新「服务端状态」标签，
  // 避免状态与实际不一致（页面本身不轮询，重启后可点状态旁的刷新按钮）。
  useEffect(() => {
    let alive = true;
    net
      .getStatus()
      .then((s) => {
        if (alive) setStatus(s);
      })
      .catch(() => {
        /* 失败静默，保留上次状态 */
      });
    return () => {
      alive = false;
    };
  }, [data]);

  // 从「内外网设置」一键跳转过来时，URL 带 iface/ip/mask：自动打开新建抽屉并预填，
  // 让用户在已填好接口/网关/掩码/默认池的基础上确认保存（不自动保存）。读完即清 query 防重复触发。
  useEffect(() => {
    const iface = searchParams.get('iface');
    if (!iface) return;
    const ip = searchParams.get('ip') || '';
    const mask = searchParams.get('mask') || '';
    const pool = defaultPool(ip, mask);
    setEditing(null);
    form.resetFields();
    form.setFieldsValue({
      force: true,
      exclude_text: '',
      lease_value: 2,
      lease_unit: 'h',
      custom_options: [],
      interface: iface,
      netmask: mask,
      gateway: ip,
      ip_start: pool.start,
      ip_end: pool.end,
    });
    setOpen(true);
    setSearchParams({}, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams]);

  // 手动刷新：重新拉取服务端状态 + 列表 + 路由下发模式（页面不轮询，状态变更后点此即可）。
  const onRefresh = async () => {
    setRefreshing(true);
    try {
      const s = await net.getStatus();
      setStatus(s);
      reload();
      try {
        setRoutePush(await net.getRoutePushMode());
      } catch {
        /* 可选，失败静默 */
      }
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setRefreshing(false);
    }
  };

  // 客户端按服务接口 / 网关过滤。
  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    if (!kw) return data;
    return data.filter(
      (s) => s.interface.toLowerCase().includes(kw) || s.gateway.toLowerCase().includes(kw),
    );
  }, [data, keyword]);

  const openDrawer = (record?: net.DHCPServer) => {
    setEditing(record ?? null);
    if (record) {
      form.setFieldsValue({
        interface: record.interface,
        ip_start: record.ip_start,
        ip_end: record.ip_end,
        force: record.force,
        exclude_text: record.exclude.join('\n'),
        netmask: record.netmask,
        gateway: record.gateway,
        dns_primary: record.dns_primary,
        dns_secondary: record.dns_secondary,
        ...minutesToUnit(record.lease_minutes),
        custom_options: record.custom_options.map((o) => ({ code: o.code, value: o.value })),
      });
    } else {
      form.resetFields();
      form.setFieldsValue({
        force: true, // 默认强制下发：旁路由/同网段多 DHCP 时也能稳定分配
        exclude_text: '',
        lease_value: 2,
        lease_unit: 'h', // 默认 2 小时
        custom_options: [],
      });
    }
    setOpen(true);
  };

  const onSave = async () => {
    const v = await form.validateFields();
    setSaving(true);
    try {
      const exclude = v.exclude_text
        .split('\n')
        .map((line) => line.trim())
        .filter((line) => line.length > 0);
      const custom_options: net.CustomOption[] = (v.custom_options ?? [])
        .filter((o) => o.code !== null && o.code !== undefined && o.value.trim() !== '')
        .map((o) => ({ code: Number(o.code), value: o.value.trim() }));
      const body: net.DHCPServerInput = {
        interface: v.interface,
        enabled: editing ? editing.enabled : true,
        ip_start: v.ip_start,
        ip_end: v.ip_end,
        force: v.force ?? true,
        netmask: v.netmask,
        gateway: v.gateway,
        dns_primary: v.dns_primary,
        dns_secondary: v.dns_secondary,
        lease_minutes: unitToMinutes(v.lease_value, v.lease_unit),
        exclude,
        custom_options,
      };
      if (editing) {
        await net.updateServer(editing.id, body);
        message.success('已保存');
      } else {
        await net.createServer(body);
        message.success('已添加');
      }
      setOpen(false);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setSaving(false);
    }
  };

  const onToggle = async (record: net.DHCPServer) => {
    try {
      await net.toggleServer(record.id, !record.enabled);
      message.success(record.enabled ? '已停用' : '已启用');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onDelete = async (record: net.DHCPServer) => {
    try {
      await net.deleteServer(record.id);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onBatch = async (action: net.BatchAction) => {
    setBatching(true);
    try {
      await net.batchServers(action, selected);
      message.success('操作成功');
      setSelected([]);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setBatching(false);
    }
  };

  const onChangeRoutePush = async (mode: net.RoutePushMode) => {
    const prev = routePush;
    setRoutePush(mode);
    try {
      await net.setRoutePushMode(mode);
      message.success('路由下发模式已保存');
    } catch (e) {
      setRoutePush(prev);
      message.error(extractErr(e));
    }
  };

  const onRestart = async () => {
    setRestarting(true);
    try {
      await net.restartDHCP();
      message.success('DHCP 服务已重启');
      try {
        setStatus(await net.getStatus());
      } catch {
        /* 状态刷新失败静默 */
      }
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setRestarting(false);
    }
  };

  const onInstallDnsmasq = async () => {
    setInstalling(true);
    try {
      const r = await net.installDHCP();
      Modal.success({
        title: 'dnsmasq 安装完成',
        width: 640,
        content: <pre style={{ maxHeight: 320, overflow: 'auto', fontSize: 12 }}>{r.output || '已安装'}</pre>,
      });
      try {
        setSvc(await net.getDHCPService());
      } catch {
        /* ignore */
      }
      reload();
    } catch (e) {
      message.error('安装失败：' + extractErr(e));
    } finally {
      setInstalling(false);
    }
  };

  const columns: ColumnsType<net.DHCPServer> = [
    { title: '服务接口', dataIndex: 'interface', width: 120 },
    {
      title: '客户端地址',
      key: 'range',
      width: 220,
      render: (_, r) => `${r.ip_start} - ${r.ip_end}`,
    },
    { title: '子网掩码', dataIndex: 'netmask', width: 130 },
    { title: '网关', dataIndex: 'gateway', width: 130 },
    { title: '首选DNS', dataIndex: 'dns_primary', width: 130 },
    { title: '备选DNS', dataIndex: 'dns_secondary', width: 130 },
    {
      title: '租期',
      dataIndex: 'lease_minutes',
      width: 90,
      render: (v: number) => {
        if (v <= 0) return '永久';
        if (v % 1440 === 0) return `${v / 1440} 天`;
        if (v % 60 === 0) return `${v / 60} 小时`;
        return `${v} 分`;
      },
    },
    { title: '剩余地址', dataIndex: 'remaining', width: 90 },
    {
      title: '状态',
      dataIndex: 'enabled',
      width: 90,
      render: (v: boolean) =>
        v ? <Tag color="success">已启用</Tag> : <Tag>已停用</Tag>,
    },
    {
      title: '操作',
      key: 'action',
      width: 160,
      fixed: 'right',
      render: (_, r) => (
        <Space size="middle">
          <Typography.Link onClick={() => openDrawer(r)}>编辑</Typography.Link>
          <Typography.Link onClick={() => onToggle(r)}>
            {r.enabled ? '停用' : '启用'}
          </Typography.Link>
          <Popconfirm title="确认删除？" onConfirm={() => onDelete(r)}>
            <Typography.Link type="danger">删除</Typography.Link>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const noneSelected = selected.length === 0;

  return (
    <PageCard
      breadcrumb={['网络设置', 'DHCP设置', 'DHCP服务端']}
      title="DHCP 服务端"
      toolbar={
        <>
          <Space size="middle" wrap>
            <Space size={6}>
              <Typography.Text type="secondary">服务端状态:</Typography.Text>
              {!status?.dhcp_ok ? (
                <Tooltip title={status?.message || ''}>
                  <Tag color="error">{status?.message ? '服务异常' : '未就绪'}</Tag>
                </Tooltip>
              ) : (status?.enabled_servers ?? 0) > 0 ? (
                <Tag color="success">服务正常</Tag>
              ) : (
                <Tooltip title="DHCP/DNS 进程在运行，但没有任何已启用的服务端池，当前不会给客户端下发地址。">
                  <Tag color="warning">未下发（无启用服务端）</Tag>
                </Tooltip>
              )}
              <Tooltip title="刷新服务端状态与列表">
                <Button size="small" type="text" icon={<ReloadOutlined />} loading={refreshing} onClick={onRefresh} />
              </Tooltip>
            </Space>
            <Input.Search
              allowClear
              placeholder="搜索接口 / 网关"
              style={{ width: 220 }}
              onChange={(e) => setKeyword(e.target.value)}
              onSearch={setKeyword}
            />
            <Space size={6}>
              <Typography.Text type="secondary">路由下发:</Typography.Text>
              <Select
                size="small"
                style={{ width: 124 }}
                value={routePush}
                onChange={onChangeRoutePush}
                options={[
                  { label: '关闭', value: 'off' },
                  { label: '全部客户端', value: 'all' },
                  { label: '仅指定设备', value: 'tagged' },
                ]}
              />
            </Space>
          </Space>
          <Space size="middle" wrap>
            <Popconfirm title="确认重启 DHCP 服务？" onConfirm={onRestart}>
              <Button danger loading={restarting}>
                重启DHCP服务
              </Button>
            </Popconfirm>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openDrawer()}>
              添加
            </Button>
            <Button disabled={noneSelected} loading={batching} onClick={() => onBatch('enable')}>
              启用
            </Button>
            <Button disabled={noneSelected} loading={batching} onClick={() => onBatch('disable')}>
              停用
            </Button>
            <Popconfirm
              title={`确认删除选中的 ${selected.length} 项？`}
              disabled={noneSelected}
              onConfirm={() => onBatch('delete')}
            >
              <Button danger disabled={noneSelected} loading={batching}>
                删除
              </Button>
            </Popconfirm>
          </Space>
        </>
      }
    >
      {svc && !svc.dnsmasq_installed && svc.can_install && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          message="未安装 dnsmasq（OpenWrt 标准 DHCP + DNS 服务）"
          description={`当前 DHCP 守护：${svc.daemon || '无'}。建议安装 dnsmasq 后由本面板统一下发 DHCP / DNS 配置。`}
          action={
            <Button type="primary" size="small" loading={installing} icon={<ThunderboltOutlined />} onClick={onInstallDnsmasq}>
              一键安装 dnsmasq
            </Button>
          }
        />
      )}
      <Table
        rowKey="id"
        size="small"
        bordered
        loading={loading}
        dataSource={filtered}
        scroll={{ x: 1500 }}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: (keys) => setSelected(keys as string[]),
        }}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
      />

      <Drawer
        title={editing ? '编辑' : '添加'}
        width="min(92vw, 680px)"
        open={open}
        onClose={() => setOpen(false)}
        destroyOnClose
        extra={
          <Space>
            <Button onClick={() => setOpen(false)}>取消</Button>
            <Button type="primary" loading={saving} onClick={onSave}>
              保存
            </Button>
          </Space>
        }
      >
        <Form form={form} layout="vertical">
          <Form.Item
            label="服务接口"
            name="interface"
            rules={[{ required: true, message: '请选择服务接口' }]}
          >
            <Select
              placeholder="请选择接口"
              options={interfaces.map((i) => ({ label: i.name, value: i.name }))}
              showSearch
              onChange={(name: string) => {
                const it = interfaces.find((x) => x.name === name);
                if (it) form.setFieldValue('netmask', it.netmask);
              }}
            />
          </Form.Item>
          <Form.Item
            label="客户端地址（起）"
            name="ip_start"
            rules={[{ required: true, message: '请输入起始地址' }]}
          >
            <Input placeholder="例如 192.168.1.100" />
          </Form.Item>
          <Form.Item
            label="客户端地址（止）"
            name="ip_end"
            rules={[{ required: true, message: '请输入结束地址' }]}
          >
            <Input placeholder="例如 192.168.1.200" />
          </Form.Item>
          <Form.Item label="排除地址" name="exclude_text">
            <Input.TextArea
              rows={3}
              placeholder="格式:192.168.1.1 或 192.168.1.1-192.168.1.10，一行一条"
            />
          </Form.Item>
          <Form.Item
            label="子网掩码"
            name="netmask"
            extra="由所选接口自动带出：dnsmasq 的 DHCP 池掩码即接口掩码，无法独立设置。如需更换网段，请到「内外网设置」修改接口 IP/掩码。"
          >
            <Input placeholder="选择接口后自动带出" readOnly />
          </Form.Item>
          <Form.Item
            label="网关"
            name="gateway"
            rules={[{ required: true, message: '请输入网关' }]}
          >
            <Input placeholder="例如 192.168.1.1" />
          </Form.Item>
          <Form.Item label="首选DNS" name="dns_primary">
            <Input placeholder="例如 192.168.1.1" />
          </Form.Item>
          <Form.Item label="备选DNS" name="dns_secondary">
            <Input placeholder="例如 8.8.8.8" />
          </Form.Item>
          <Form.Item label="租期" tooltip="设备超过此时长未续租，地址会被回收重分配；选「永久」则不过期。">
            <Form.Item noStyle shouldUpdate={(p, c) => p.lease_unit !== c.lease_unit}>
              {({ getFieldValue }) => {
                const inf = getFieldValue('lease_unit') === 'inf';
                return (
                  <Space.Compact style={{ width: '100%' }}>
                    <Form.Item name="lease_value" noStyle rules={inf ? [] : [{ required: true, message: '请输入租期数值' }]}>
                      <InputNumber min={1} disabled={inf} style={{ width: '100%' }} placeholder={inf ? '永久不过期' : '数值'} />
                    </Form.Item>
                    <Form.Item name="lease_unit" noStyle>
                      <Select
                        style={{ width: 110 }}
                        options={[
                          { value: 'm', label: '分钟' },
                          { value: 'h', label: '小时' },
                          { value: 'd', label: '天' },
                          { value: 'inf', label: '永久' },
                        ]}
                      />
                    </Form.Item>
                  </Space.Compact>
                );
              }}
            </Form.Item>
          </Form.Item>
          <Form.Item
            label="强制下发 DHCP"
            name="force"
            valuePropName="checked"
            extra="即便本网段已存在其它 DHCP 服务器，也强制由本机下发（跳过 dnsmasq 的探测礼让）。旁路由/主路由同段时务必开启，否则设备可能拿不到地址。默认开启。"
          >
            <Switch />
          </Form.Item>
          <Typography.Text strong>自定义DHCP选项</Typography.Text>
          <Form.List name="custom_options">
            {(fields, { add, remove }) => (
              <div style={{ marginTop: 8 }}>
                {fields.map((field) => (
                  <Space key={field.key} align="baseline" style={{ display: 'flex', marginBottom: 8 }}>
                    <Form.Item
                      name={[field.name, 'code']}
                      rules={[{ required: true, message: '编号' }]}
                      noStyle
                    >
                      <InputNumber min={0} placeholder="编号" style={{ width: 110 }} />
                    </Form.Item>
                    <Form.Item
                      name={[field.name, 'value']}
                      rules={[{ required: true, message: '值' }]}
                      noStyle
                    >
                      <Input placeholder="值" style={{ width: 260 }} />
                    </Form.Item>
                    <Typography.Link type="danger" onClick={() => remove(field.name)}>
                      删除
                    </Typography.Link>
                  </Space>
                ))}
                <Space wrap>
                  <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({ code: null, value: '' })}>
                    手动添加
                  </Button>
                  <Select
                    placeholder="+ 常用选项（自动带选项号）"
                    style={{ width: 240 }}
                    value={null}
                    onSelect={(code) => add({ code: Number(code), value: '' })}
                    options={COMMON_OPTIONS.map((o) => ({ value: o.code, label: `${o.code} · ${o.name}` }))}
                  />
                </Space>
              </div>
            )}
          </Form.List>
        </Form>
      </Drawer>
    </PageCard>
  );
}
