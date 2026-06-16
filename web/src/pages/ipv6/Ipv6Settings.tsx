import { useEffect, useState } from 'react';
import {
  Alert, App, Button, Col, Drawer, Form, Input, InputNumber, Popconfirm, Radio,
  Row, Select, Space, Switch, Table, Tag, Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import PageCard from '../../components/PageCard';
import { useNetData, extractErr } from '../../hooks/useNetData';
import * as ipv6 from '../../api/ipv6';
import * as net from '../../api/netcfg';

const { Text } = Typography;

const PROTO_LABEL: Record<string, string> = {
  dhcpv6: 'DHCPv6 客户端（动态获取）', static6: '静态 IPv6', '6in4': '6in4 隧道', '6to4': '6to4 隧道', '6rd': '6rd 隧道',
};
const MODE_LABEL: Record<string, string> = { stateless: '无状态', stateful: '有状态', stateful_only: '纯有状态' };
const REQ_PREFIX_OPTS = ['64', '60', '56', '48', 'auto', 'no'];

export default function Ipv6Settings() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData(async () => {
    const [wans, lans] = await Promise.all([ipv6.listWANv6(), ipv6.listLANv6()]);
    return { wans, lans };
  }, { wans: [] as ipv6.WANv6[], lans: [] as ipv6.LANv6[] });

  const [ifaces, setIfaces] = useState<net.NetInterface[]>([]);
  const [svc, setSvc] = useState<ipv6.DHCPv6SvcInfo | null>(null);
  const [drawer, setDrawer] = useState<{ open: boolean; role: 'wan' | 'lan'; editing: ipv6.WANv6 | ipv6.LANv6 | null }>({ open: false, role: 'wan', editing: null });

  useEffect(() => {
    net.listInterfaces().then(setIfaces).catch(() => {});
    ipv6.getDHCPv6Service().then(setSvc).catch(() => {});
  }, [data]);

  const openEdit = (role: 'wan' | 'lan', editing: ipv6.WANv6 | ipv6.LANv6 | null) => setDrawer({ open: true, role, editing });

  const onToggleWAN = async (r: ipv6.WANv6) => {
    try { await ipv6.toggleWANv6(r.id, !r.enabled); reload(); } catch (e) { message.error(extractErr(e)); }
  };
  const onDeleteWAN = async (r: ipv6.WANv6) => {
    try { await ipv6.deleteWANv6(r.id); message.success('已删除'); reload(); } catch (e) { message.error(extractErr(e)); }
  };
  const onToggleLAN = async (r: ipv6.LANv6) => {
    try { await ipv6.toggleLANv6(r.id, !r.enabled); reload(); } catch (e) { message.error(extractErr(e)); }
  };
  const onDeleteLAN = async (r: ipv6.LANv6) => {
    try { await ipv6.deleteLANv6(r.id); message.success('已删除'); reload(); } catch (e) { message.error(extractErr(e)); }
  };

  const wanCols: ColumnsType<ipv6.WANv6> = [
    { title: '外网接口', dataIndex: 'name', render: (v, r) => <Space><Text strong>{v}</Text>{!r.managed && <Tag>已导入</Tag>}</Space> },
    { title: '接入方式', dataIndex: 'proto', render: (p: string) => PROTO_LABEL[p] ?? p },
    { title: '本地链接IPv6', dataIndex: 'local_link', render: (v) => v || '—' },
    { title: 'IPv6前缀', dataIndex: 'ip6_prefix', render: (v) => v || '—' },
    { title: 'IPv6地址', dataIndex: 'ip6_address', render: (v) => v || '—' },
    { title: 'IPv6网关', dataIndex: 'ip6_gateway', render: (v) => v || '—' },
    { title: '状态', dataIndex: 'enabled', render: (v: boolean) => (v ? <Tag color="success">已启用</Tag> : <Tag>已停用</Tag>) },
    {
      title: '操作', key: 'a', fixed: 'right', render: (_, r) => (
        <Space size="middle">
          <Typography.Link onClick={() => openEdit('wan', r)}>编辑</Typography.Link>
          <Typography.Link onClick={() => onToggleWAN(r)}>{r.enabled ? '停用' : '启用'}</Typography.Link>
          <Popconfirm title="确认删除？" onConfirm={() => onDeleteWAN(r)}><Typography.Link type="danger">删除</Typography.Link></Popconfirm>
        </Space>
      ),
    },
  ];

  const lanCols: ColumnsType<ipv6.LANv6> = [
    { title: '内网接口', dataIndex: 'interface', render: (v, r) => <Space><Text strong>{v}</Text>{!r.managed && <Tag>已导入</Tag>}</Space> },
    { title: '配置类型', dataIndex: 'config_type', render: (v: string) => (v === 'static' ? '静态' : '自动获取') },
    { title: '绑定外网线路', dataIndex: 'bind_wan', render: (v) => v || '自动' },
    { title: '本地链接IPv6', dataIndex: 'local_link', render: (v) => v || '—' },
    { title: 'IPv6地址', dataIndex: 'ip6_address', render: (v) => v || '—' },
    { title: 'DHCPv6', dataIndex: 'dhcpv6_enabled', render: (v: boolean) => (v ? <Tag color="blue">开启</Tag> : <Tag>关闭</Tag>) },
    { title: '模式', dataIndex: 'dhcpv6_mode', render: (v: string) => MODE_LABEL[v] ?? v },
    { title: '租期', dataIndex: 'lease_minutes', render: (v: number) => `${v} 分` },
    { title: '状态', dataIndex: 'enabled', render: (v: boolean) => (v ? <Tag color="success">已启用</Tag> : <Tag>已停用</Tag>) },
    {
      title: '操作', key: 'a', fixed: 'right', render: (_, r) => (
        <Space size="middle">
          <Typography.Link onClick={() => openEdit('lan', r)}>编辑</Typography.Link>
          <Typography.Link onClick={() => onToggleLAN(r)}>{r.enabled ? '停用' : '启用'}</Typography.Link>
          <Popconfirm title="确认删除？" onConfirm={() => onDeleteLAN(r)}><Typography.Link type="danger">删除</Typography.Link></Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <PageCard
      breadcrumb={['网络设置', 'IPv6', 'IPv6设置']}
      title="IPv6"
      extra={<Button icon={<ReloadOutlined />} onClick={() => reload()} loading={loading}>刷新</Button>}
    >
      {svc && !svc.odhcpd_installed && (
        <Alert type="warning" showIcon style={{ marginBottom: 12 }}
          message="未检测到 odhcpd（OpenWrt 的 IPv6 RA / DHCPv6 服务）"
          description="内网 IPv6 服务端依赖 odhcpd。请先通过包管理器安装 odhcpd 后再配置内网 DHCPv6。" />
      )}

      <Space style={{ width: '100%', justifyContent: 'space-between', marginBottom: 8 }}>
        <Text strong>外网配置</Text>
        <Button type="primary" size="small" icon={<PlusOutlined />} onClick={() => openEdit('wan', null)}>添加</Button>
      </Space>
      <Table rowKey="id" size="small" bordered loading={loading} dataSource={data.wans} columns={wanCols}
        pagination={false} scroll={{ x: 'max-content' }} style={{ marginBottom: 24 }}
        locale={{ emptyText: '暂无 IPv6 外网配置' }} />

      <Space style={{ width: '100%', justifyContent: 'space-between', marginBottom: 8 }}>
        <Text strong>内网配置</Text>
        <Button type="primary" size="small" icon={<PlusOutlined />} onClick={() => openEdit('lan', null)}>添加</Button>
      </Space>
      <Table rowKey="id" size="small" bordered loading={loading} dataSource={data.lans} columns={lanCols}
        pagination={false} scroll={{ x: 'max-content' }}
        locale={{ emptyText: '暂无 IPv6 内网配置' }} />

      {drawer.role === 'wan' ? (
        <WANv6Drawer open={drawer.open} editing={drawer.editing as ipv6.WANv6 | null} ifaces={ifaces}
          onClose={() => setDrawer((d) => ({ ...d, open: false }))}
          onSaved={() => { setDrawer((d) => ({ ...d, open: false })); reload(); }} />
      ) : (
        <LANv6Drawer open={drawer.open} editing={drawer.editing as ipv6.LANv6 | null} ifaces={ifaces} wans={data.wans}
          onClose={() => setDrawer((d) => ({ ...d, open: false }))}
          onSaved={() => { setDrawer((d) => ({ ...d, open: false })); reload(); }} />
      )}
    </PageCard>
  );
}

// ---------------- 外网编辑抽屉 ----------------

function WANv6Drawer({ open, editing, ifaces, onClose, onSaved }: {
  open: boolean; editing: ipv6.WANv6 | null; ifaces: net.NetInterface[]; onClose: () => void; onSaved: () => void;
}) {
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [saving, setSaving] = useState(false);
  const proto = Form.useWatch('proto', form) as string | undefined;
  const peerDNS = Form.useWatch('peer_dns', form) as boolean | undefined;
  const useFixed = Form.useWatch('use_fixed', form) as boolean | undefined;

  useEffect(() => {
    if (!open) return;
    if (editing) {
      form.setFieldsValue({ ...editing, use_fixed: !!editing.fixed_prefix });
    } else {
      form.setFieldsValue({
        id: '', name: '', wan_iface: 'wan', device: '', proto: 'dhcpv6', enabled: true,
        req_prefix: '64', fixed_prefix: '', use_fixed: false, force_prefix: true, client_id: '',
        no_release: false, peer_dns: true, dns_primary: '', dns_secondary: '',
        static_ip6: '', static_gateway: '', peer_addr: '', tun_prefix: '', mtu: 1492, remark: '',
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing]);

  const onRegenDUID = async () => {
    if (editing) {
      try { const u = await ipv6.regenWANv6DUID(editing.id); form.setFieldValue('client_id', u.client_id); message.success('已生成新 DUID'); onSaved(); } catch (e) { message.error(extractErr(e)); }
    } else {
      // 新建态本地生成一个随机 DUID-UUID（前端预览，后端保存时按实际值写入）
      const hex = Array.from({ length: 16 }, () => Math.floor(Math.random() * 256).toString(16).padStart(2, '0')).join('');
      form.setFieldValue('client_id', '0004' + hex);
    }
  };

  const onCheckPkg = async (p: string) => {
    if (p === 'dhcpv6' || p === 'static6') return;
    try {
      const r = await ipv6.transitionPkg(p);
      if (!r.installed) message.warning(`接入方式 ${p} 需要安装协议包「${r.pkg}」，当前未检测到，请先安装`);
    } catch { /* ignore */ }
  };

  const onSave = async () => {
    let v;
    try { v = await form.validateFields(); } catch { return; }
    const body: ipv6.WANv6Input = {
      id: editing?.id || v.id || '', name: editing?.name || v.name || '', wan_iface: v.wan_iface || 'wan',
      device: v.device ?? editing?.device ?? '', proto: v.proto, enabled: editing ? editing.enabled : true,
      req_prefix: v.use_fixed ? '' : (v.req_prefix || ''), fixed_prefix: v.use_fixed ? (v.fixed_prefix || '') : '',
      force_prefix: !!v.force_prefix, client_id: v.client_id || '', no_release: !!v.no_release,
      peer_dns: !!v.peer_dns, dns_primary: v.dns_primary || '', dns_secondary: v.dns_secondary || '',
      static_ip6: v.static_ip6 || '', static_gateway: v.static_gateway || '',
      peer_addr: v.peer_addr || '', tun_prefix: v.tun_prefix || '', mtu: v.mtu || 0, remark: v.remark || '',
    };
    setSaving(true);
    try {
      if (editing) await ipv6.updateWANv6(editing.id, body); else await ipv6.createWANv6(body);
      message.success('已保存（已下发并重载网络）');
      onSaved();
    } catch (e) { message.error('保存失败：' + extractErr(e)); } finally { setSaving(false); }
  };

  return (
    <Drawer title={`IPv6 外网设置${editing ? ' · ' + editing.name : '（新增）'}`} width={560} open={open} onClose={onClose} destroyOnClose
      footer={<Space><Button type="primary" loading={saving} onClick={onSave}>保存</Button><Button onClick={onClose}>取消</Button></Space>}>
      <Form form={form} layout="vertical">
        {!editing && (
          <Form.Item label="接口名称" name="id" tooltip="留空自动取 wan6 / wan6_2"><Input placeholder="wan6" /></Form.Item>
        )}
        <Form.Item label="绑定外网接口" name="wan_iface" tooltip="跟随该 IPv4 外网接口的物理设备（device @wan）">
          <Select showSearch options={ifaces.map((i) => ({ label: i.name, value: i.name }))} placeholder="wan" allowClear />
        </Form.Item>
        <Form.Item label="接入方式" name="proto">
          <Select onChange={(v) => onCheckPkg(v)} options={Object.entries(PROTO_LABEL).map(([value, label]) => ({ value, label }))} />
        </Form.Item>

        {proto === 'dhcpv6' && (
          <>
            <Form.Item label="尝试固定前缀" name="use_fixed" valuePropName="checked" tooltip="开启后向上游请求指定的固定前缀">
              <Switch />
            </Form.Item>
            {useFixed ? (
              <Form.Item label="固定前缀" name="fixed_prefix" rules={[{ required: true, message: '请输入固定前缀（如 2001:db8::/60）' }]}>
                <Input placeholder="2001:db8::/60" />
              </Form.Item>
            ) : (
              <Form.Item label="请求前缀长度" name="req_prefix">
                <Select options={REQ_PREFIX_OPTS.map((v) => ({ value: v, label: v }))} />
              </Form.Item>
            )}
            <Form.Item label="强行获取前缀" name="force_prefix" valuePropName="checked" tooltip="reqaddress=force，强制向上游请求地址">
              <Switch />
            </Form.Item>
            <Form.Item label="客户端 DUID 标识" name="client_id" tooltip="留空使用 OpenWrt 默认（基于 MAC）；可点「重新生成」用随机 DUID">
              <Input placeholder="留空=默认" addonAfter={<Typography.Link onClick={onRegenDUID}>重新生成</Typography.Link>} />
            </Form.Item>
            <Form.Item label="断开不释放前缀" name="no_release" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item label="使用上游下发的 DNS" name="peer_dns" valuePropName="checked"><Switch /></Form.Item>
            {!peerDNS && (
              <Row gutter={12}>
                <Col span={12}><Form.Item label="首选 DNS" name="dns_primary"><Input placeholder="2606:4700:4700::1111" /></Form.Item></Col>
                <Col span={12}><Form.Item label="备选 DNS" name="dns_secondary"><Input placeholder="2001:4860:4860::8888" /></Form.Item></Col>
              </Row>
            )}
          </>
        )}

        {proto === 'static6' && (
          <>
            <Form.Item label="IPv6 地址" name="static_ip6" rules={[{ required: true, message: '请输入带前缀的 IPv6（如 2001:db8::1/64）' }]}>
              <Input placeholder="2001:db8::1/64" />
            </Form.Item>
            <Form.Item label="IPv6 网关" name="static_gateway"><Input placeholder="2001:db8::1" /></Form.Item>
          </>
        )}

        {(proto === '6in4' || proto === '6rd') && (
          <>
            <Form.Item label="隧道对端 IPv4" name="peer_addr"><Input placeholder="隧道服务器 IPv4" /></Form.Item>
            <Form.Item label="隧道前缀" name="tun_prefix"><Input placeholder="2001:db8::/48" /></Form.Item>
          </>
        )}

        {editing && (
          <Form.Item label="运行状态">
            <Space direction="vertical" size={2}>
              {editing.up ? <Tag color="success">已连接</Tag> : <Tag>未连接</Tag>}
              {editing.ip6_address && <Text type="secondary" style={{ fontSize: 12 }}>地址：{editing.ip6_address}</Text>}
              {editing.ip6_prefix && <Text type="secondary" style={{ fontSize: 12 }}>前缀：{editing.ip6_prefix}</Text>}
              {editing.ip6_gateway && <Text type="secondary" style={{ fontSize: 12 }}>网关：{editing.ip6_gateway}</Text>}
            </Space>
          </Form.Item>
        )}
        <Form.Item label="MTU" name="mtu"><InputNumber min={1280} max={9200} style={{ width: 160 }} /></Form.Item>
        <Form.Item label="备注" name="remark"><Input /></Form.Item>
      </Form>
    </Drawer>
  );
}

// ---------------- 内网编辑抽屉 ----------------

function LANv6Drawer({ open, editing, ifaces, wans, onClose, onSaved }: {
  open: boolean; editing: ipv6.LANv6 | null; ifaces: net.NetInterface[]; wans: ipv6.WANv6[]; onClose: () => void; onSaved: () => void;
}) {
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [saving, setSaving] = useState(false);
  const dhcpOn = Form.useWatch('dhcpv6_enabled', form) as boolean | undefined;
  const dnsOn = Form.useWatch('ipv6_dns_enabled', form) as boolean | undefined;
  const raMtuOn = Form.useWatch('ra_mtu_enabled', form) as boolean | undefined;
  const cfgType = Form.useWatch('config_type', form) as string | undefined;

  useEffect(() => {
    if (!open) return;
    if (editing) {
      form.setFieldsValue(editing);
    } else {
      form.setFieldsValue({
        interface: 'lan', config_type: 'auto', bind_wan: '', prefix_assign_len: 64, prefix_hint: '',
        static_ip6: '', dhcpv6_enabled: true, dhcpv6_mode: 'stateful', ipv6_dns_enabled: false,
        dns_servers: [], lease_minutes: 120, ra_mtu_enabled: false, ra_mtu: 1492, enabled: true, remark: '',
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing]);

  const onSave = async () => {
    let v;
    try { v = await form.validateFields(); } catch { return; }
    const body: ipv6.LANv6Input = {
      id: editing?.id || '', interface: v.interface, config_type: v.config_type, bind_wan: v.bind_wan || '',
      prefix_assign_len: v.prefix_assign_len || 0, prefix_hint: v.prefix_hint ?? editing?.prefix_hint ?? '', static_ip6: v.static_ip6 || '',
      dhcpv6_enabled: !!v.dhcpv6_enabled, dhcpv6_mode: v.dhcpv6_mode, ipv6_dns_enabled: !!v.ipv6_dns_enabled,
      dns_servers: v.dns_servers || [], lease_minutes: v.lease_minutes || 0, ra_mtu_enabled: !!v.ra_mtu_enabled,
      ra_mtu: v.ra_mtu || 0, enabled: editing ? editing.enabled : true, remark: v.remark || '',
    };
    setSaving(true);
    try {
      if (editing) await ipv6.updateLANv6(editing.id, body); else await ipv6.createLANv6(body);
      message.success('已保存（已下发并重载 odhcpd）');
      onSaved();
    } catch (e) { message.error('保存失败：' + extractErr(e)); } finally { setSaving(false); }
  };

  return (
    <Drawer title={`IPv6 内网设置${editing ? ' · ' + editing.interface : '（新增）'}`} width={560} open={open} onClose={onClose} destroyOnClose
      footer={<Space><Button type="primary" loading={saving} onClick={onSave}>保存</Button><Button onClick={onClose}>取消</Button></Space>}>
      <Alert type="info" showIcon style={{ marginBottom: 16 }}
        message="若本机为上游的下游设备（其 IPv6 来自上游 DHCPv6），在 LAN 再开 DHCPv6 服务端可能与上游冲突，请按拓扑使用。" />
      <Form form={form} layout="vertical">
        <Form.Item label="内网接口" name="interface" rules={[{ required: true, message: '请选择内网接口' }]}>
          <Select disabled={!!editing} showSearch options={ifaces.map((i) => ({ label: i.name, value: i.name }))} placeholder="lan" />
        </Form.Item>
        <Form.Item label="配置类型" name="config_type">
          <Radio.Group optionType="button" buttonStyle="solid">
            <Radio.Button value="auto">自动获取</Radio.Button>
            <Radio.Button value="static">静态</Radio.Button>
          </Radio.Group>
        </Form.Item>
        {cfgType === 'static' && (
          <Form.Item label="静态 IPv6" name="static_ip6" rules={[{ required: true, message: '请输入带前缀的内网 IPv6（如 fd00::1/64）' }]}>
            <Input placeholder="fd00::1/64" />
          </Form.Item>
        )}
        <Form.Item label="绑定外网线路" name="bind_wan" tooltip="多 WAN 时从哪条外网线路的委派前缀分配；留空=自动">
          <Select allowClear placeholder="自动" options={wans.map((w) => ({ label: w.name, value: w.id }))} />
        </Form.Item>
        <Form.Item label="前缀分配长度" name="prefix_assign_len" tooltip="从委派前缀切分给本内网的子网长度（ip6assign）">
          <Select options={[60, 62, 64].map((v) => ({ value: v, label: String(v) }))} />
        </Form.Item>
        <Form.Item label="DHCPv6 服务端" name="dhcpv6_enabled" valuePropName="checked" tooltip="开启后本机向内网下发 RA + DHCPv6"><Switch /></Form.Item>
        {dhcpOn && (
          <Form.Item label="DHCPv6 模式" name="dhcpv6_mode">
            <Radio.Group optionType="button" buttonStyle="solid">
              <Radio.Button value="stateless">无状态</Radio.Button>
              <Radio.Button value="stateful">有状态</Radio.Button>
              <Radio.Button value="stateful_only">纯有状态</Radio.Button>
            </Radio.Group>
          </Form.Item>
        )}
        <Form.Item label="租期（分钟）" name="lease_minutes"><InputNumber min={0} style={{ width: 160 }} /></Form.Item>
        <Form.Item label="下发 IPv6 DNS" name="ipv6_dns_enabled" valuePropName="checked"><Switch /></Form.Item>
        {dnsOn && (
          <Form.Item label="IPv6 DNS 列表" name="dns_servers" tooltip="回车添加多个">
            <Select mode="tags" placeholder="2606:4700:4700::1111" tokenSeparators={[',', ' ']} />
          </Form.Item>
        )}
        <Form.Item label="下发 RA MTU" name="ra_mtu_enabled" valuePropName="checked"><Switch /></Form.Item>
        {raMtuOn && (
          <Form.Item label="RA MTU" name="ra_mtu"><InputNumber min={1280} max={9200} style={{ width: 160 }} /></Form.Item>
        )}
        <Form.Item label="备注" name="remark"><Input /></Form.Item>
      </Form>
    </Drawer>
  );
}
