import { useEffect, useMemo, useState } from 'react';
import {
  Alert, App, Button, Card, Checkbox, Col, Drawer, Form, Input, InputNumber,
  Modal, Popconfirm, Radio, Row, Select, Space, Statistic, Switch, Tag, Tooltip, Typography,
} from 'antd';
import {
  GlobalOutlined, ApartmentOutlined, PlusOutlined, ReloadOutlined,
  PoweroffOutlined, ThunderboltOutlined, DeleteOutlined, WifiOutlined,
} from '@ant-design/icons';
import PageCard from '../components/PageCard';
import { useNetData, extractErr } from '../hooks/useNetData';
import * as net from '../api/netcfg';

const { Text, Paragraph } = Typography;

const MASKS = ['255.255.255.0', '255.255.254.0', '255.255.252.0', '255.255.248.0', '255.255.240.0', '255.255.224.0', '255.255.192.0', '255.255.128.0', '255.255.0.0', '255.0.0.0'];

export default function NetOverview() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<net.NetOverview>(() => net.getNetOverview(), {
    wan_count: 0, wan_up: 0, connections: 0, lan_count: 0, lan_up: 0, dhcp_on: 0, terminals: 0, free_ports: 0, wans: [], lans: [],
  });
  const [nics, setNics] = useState<net.NIC[]>([]);
  const [svc, setSvc] = useState<net.DHCPSvcInfo | null>(null);
  const [installing, setInstalling] = useState(false);

  const [drawer, setDrawer] = useState<{ open: boolean; role: 'lan' | 'wan'; editing: net.NetIface | null }>({ open: false, role: 'lan', editing: null });

  const loadAux = () => {
    net.listNICs().then(setNics).catch(() => {});
    net.getDHCPService().then(setSvc).catch(() => {});
  };
  useEffect(() => { loadAux(); }, [data]);

  const onInstall = async () => {
    setInstalling(true);
    try {
      const r = await net.installDHCP();
      Modal.success({ title: 'dnsmasq 安装完成', width: 640, content: <pre style={{ maxHeight: 320, overflow: 'auto', fontSize: 12 }}>{r.output || '已安装'}</pre> });
      loadAux();
      reload();
    } catch (e) {
      message.error('安装失败：' + extractErr(e));
    } finally {
      setInstalling(false);
    }
  };

  const openEdit = (role: 'lan' | 'wan', editing: net.NetIface | null) => setDrawer({ open: true, role, editing });

  const onAction = async (id: string, action: 'connect' | 'disconnect' | 'restart') => {
    try {
      await net.ifaceAction(id, action);
      message.success('已执行');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onDelete = async (id: string) => {
    try {
      await net.deleteNetIface(id);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const needInstall = svc && !svc.dnsmasq_installed && svc.can_install;

  return (
    <PageCard
      breadcrumb={['网络设置', '内外网设置']}
      title="内外网设置"
      extra={<Button icon={<ReloadOutlined />} onClick={() => reload()} loading={loading}>刷新</Button>}
    >
      {needInstall && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message="未安装 dnsmasq（OpenWrt 标准 DHCP + DNS 服务）"
          description={`当前 DHCP 守护：${svc?.daemon || '无'}。dnsmasq 是最常用的 DHCP/DNS 实现，建议安装后由本面板统一管理。`}
          action={<Button type="primary" loading={installing} icon={<ThunderboltOutlined />} onClick={onInstall}>一键安装 dnsmasq</Button>}
        />
      )}

      {/* 状态卡片 */}
      <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
        <Col xs={24} md={12}>
          <Card style={{ borderLeft: '4px solid #1f6fb2' }}>
            <Space size="large" align="center" wrap>
              <Space><GlobalOutlined style={{ fontSize: 26, color: '#1f6fb2' }} /><Text strong style={{ fontSize: 15 }}>外网状态</Text></Space>
              <Statistic title="WAN 已连接" value={data.wan_up} suffix={`/ ${data.wan_count}`} />
              <Statistic title="连接数" value={data.connections} />
            </Space>
          </Card>
        </Col>
        <Col xs={24} md={12}>
          <Card style={{ borderLeft: '4px solid #52c41a' }}>
            <Space size="large" align="center" wrap>
              <Space><ApartmentOutlined style={{ fontSize: 26, color: '#52c41a' }} /><Text strong style={{ fontSize: 15 }}>内网状态</Text></Space>
              <Statistic title="LAN 已连接" value={data.lan_up} suffix={`/ ${data.lan_count}`} />
              <Statistic title="DHCP 已启用" value={data.dhcp_on} />
              <Statistic title="终端连接" value={data.terminals} />
            </Space>
          </Card>
        </Col>
      </Row>

      {/* 接口状态 */}
      <Text strong style={{ display: 'block', margin: '8px 0 12px' }}>接口状态</Text>

      <PortGroup
        title="空闲网口"
        count={data.free_ports}
        extra={
          <Space>
            <Button size="small" type="primary" icon={<PlusOutlined />} onClick={() => openEdit('wan', null)}>新增外网</Button>
            <Button size="small" icon={<PlusOutlined />} onClick={() => openEdit('lan', null)}>新增内网</Button>
          </Space>
        }
      >
        {nics.filter((n) => n.kind === 'physical' && !n.bound).map((n) => (
          <PortCard key={n.name} title={n.name} sub={n.up ? '已连接' : '未连接'} color="#9ca3af" icon={<WifiOutlined />} />
        ))}
      </PortGroup>

      <PortGroup title="外网网口" count={data.wan_count}>
        {data.wans.map((w) => (
          <PortCard
            key={w.id}
            title={w.name}
            sub={`${protoLabel(w.proto)}${w.up ? ' · 已连接' : ' · 未连接'}`}
            color={w.up ? '#1f6fb2' : '#9ca3af'}
            icon={<GlobalOutlined />}
            onClick={() => openEdit('wan', w)}
          />
        ))}
      </PortGroup>

      <PortGroup title="内网网口" count={data.lan_count}>
        {data.lans.map((l) => (
          <PortCard
            key={l.id}
            title={l.name}
            sub={`${l.ipaddr || '—'}${l.up ? ' · 已连接' : ''}`}
            color={l.up ? '#52c41a' : '#9ca3af'}
            icon={<ApartmentOutlined />}
            onClick={() => openEdit('lan', l)}
          />
        ))}
      </PortGroup>

      <IfaceDrawer
        open={drawer.open}
        role={drawer.role}
        editing={drawer.editing}
        nics={nics}
        masks={MASKS}
        onClose={() => setDrawer((d) => ({ ...d, open: false }))}
        onSaved={() => { setDrawer((d) => ({ ...d, open: false })); reload(); }}
        onAction={onAction}
        onDelete={onDelete}
      />
    </PageCard>
  );
}

function protoLabel(p: string) {
  return p === 'pppoe' ? 'PPPoE 拨号' : p === 'static' ? '静态 IP' : 'DHCP 动态';
}

function PortGroup({ title, count, extra, children }: { title: string; count: number; extra?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 18 }}>
      <Space style={{ marginBottom: 8 }}>
        <Tag color="blue">{count}</Tag>
        <Text type="secondary">{title}</Text>
        {extra}
      </Space>
      <Space wrap size={[12, 12]}>
        {children}
        {count === 0 && !extra && <Text type="secondary" style={{ fontSize: 12 }}>暂无</Text>}
      </Space>
    </div>
  );
}

function PortCard({ title, sub, color, icon, onClick }: { title: string; sub: string; color: string; icon: React.ReactNode; onClick?: () => void }) {
  return (
    <Card
      size="small"
      hoverable={!!onClick}
      onClick={onClick}
      style={{ width: 140, textAlign: 'center', cursor: onClick ? 'pointer' : 'default', borderColor: color }}
      styles={{ body: { padding: '12px 8px' } }}
    >
      <div style={{ fontSize: 26, color }}>{icon}</div>
      <div style={{ fontWeight: 600, marginTop: 4 }}>{title}</div>
      <Text type="secondary" style={{ fontSize: 11 }}>{sub}</Text>
    </Card>
  );
}

interface DrawerProps {
  open: boolean;
  role: 'lan' | 'wan';
  editing: net.NetIface | null;
  nics: net.NIC[];
  masks: string[];
  onClose: () => void;
  onSaved: () => void;
  onAction: (id: string, action: 'connect' | 'disconnect' | 'restart') => void;
  onDelete: (id: string) => void;
}

function IfaceDrawer({ open, role, editing, nics, masks, onClose, onSaved, onAction, onDelete }: DrawerProps) {
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [saving, setSaving] = useState(false);
  const proto = Form.useWatch('proto', form) as string | undefined;

  // 可绑定的物理网卡：空闲 + 当前接口已绑定的
  const phys = useMemo(() => nics.filter((n) => n.kind === 'physical'), [nics]);

  useEffect(() => {
    if (!open) return;
    if (editing) {
      form.setFieldsValue({
        id: editing.id, proto: editing.proto, ipaddr: editing.ipaddr, netmask: editing.netmask || '255.255.255.0',
        gateway: editing.gateway, dns_primary: editing.dns_primary, dns_secondary: editing.dns_secondary,
        username: editing.username, password: editing.password, service: editing.service, ac: editing.ac,
        mtu: editing.mtu || 1500, default_gw: editing.default_gw, remark: editing.remark,
        ports: editing.ports, device: editing.device, clone_mac: editing.clone_mac,
      });
    } else {
      form.setFieldsValue({
        id: '', proto: role === 'wan' ? 'dhcp' : 'static', netmask: '255.255.255.0',
        mtu: 1500, default_gw: true, ports: [], device: '',
        ipaddr: role === 'lan' ? '192.168.2.1' : '',
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing, role]);

  const onSave = async () => {
    let v;
    try {
      v = await form.validateFields();
    } catch {
      return;
    }
    const ports: string[] = v.ports || [];
    const body: net.NetIfaceInput = {
      id: editing?.id || v.id || '',
      name: editing?.name || v.id || '',
      role,
      proto: role === 'lan' ? 'static' : v.proto,
      device: role === 'wan' ? (ports[0] || v.device || '') : (editing?.device || ''),
      ports,
      ipaddr: v.ipaddr || '',
      netmask: v.netmask || '',
      gateway: v.gateway || '',
      dns_primary: v.dns_primary || '',
      dns_secondary: v.dns_secondary || '',
      username: v.username || '',
      password: v.password || '',
      service: v.service || '',
      ac: v.ac || '',
      mtu: v.mtu || 0,
      default_gw: !!v.default_gw,
      clone_mac: v.clone_mac || '',
      remark: v.remark || '',
    };
    setSaving(true);
    try {
      if (editing) await net.updateNetIface(editing.id, body);
      else await net.createNetIface(body);
      message.success('已保存（已下发并重载网络）');
      onSaved();
    } catch (e) {
      message.error('保存失败：' + extractErr(e));
    } finally {
      setSaving(false);
    }
  };

  const title = `${role === 'wan' ? '外网设置' : '内网设置'}${editing ? ' · ' + editing.name : '（新增）'}`;

  return (
    <Drawer
      title={title}
      width={560}
      open={open}
      onClose={onClose}
      destroyOnClose
      extra={
        <Space>
          {editing && role === 'wan' && (
            <>
              <Button size="small" onClick={() => onAction(editing.id, 'disconnect')}>断开</Button>
              <Button size="small" type="primary" icon={<PoweroffOutlined />} onClick={() => onAction(editing.id, 'restart')}>重拨</Button>
            </>
          )}
          {editing && (
            <Popconfirm title={`删除接口 ${editing.name}？`} onConfirm={() => { onDelete(editing.id); onClose(); }}>
              <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
            </Popconfirm>
          )}
        </Space>
      }
      footer={
        <Space>
          <Button type="primary" loading={saving} onClick={onSave}>保存</Button>
          <Button onClick={onClose}>取消</Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical">
        {!editing && (
          <Form.Item label="接口名称" name="id" tooltip="留空自动取 lan/lan2/wan/wan2" extra="OpenWrt 的逻辑接口名（小写字母+数字）">
            <Input placeholder={role === 'wan' ? 'wan / wan2' : 'lan / lan2'} />
          </Form.Item>
        )}

        {/* 绑定网卡 */}
        {role === 'lan' ? (
          <Form.Item label="绑定网卡（可多选组成网桥）" name="ports" tooltip="勾选的物理网卡将桥接为此内网口">
            <Checkbox.Group style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {phys.map((n) => (
                <Checkbox key={n.name} value={n.name}>
                  <Space size={6}>
                    <Text strong>{n.name}</Text>
                    <Text type="secondary" style={{ fontSize: 12 }}>{n.mac}</Text>
                    {n.up ? <Tag color="success">链路</Tag> : <Tag>断开</Tag>}
                    {n.bound && n.bound !== editing?.id && <Tag color="warning">占用:{n.bound}</Tag>}
                  </Space>
                </Checkbox>
              ))}
            </Checkbox.Group>
          </Form.Item>
        ) : (
          <Form.Item label="绑定网卡" name="ports" rules={[{ required: !editing, message: '请选择一个网卡' }]} getValueFromEvent={(v) => (v ? [v] : [])} getValueProps={(v) => ({ value: Array.isArray(v) ? v[0] : v })}>
            <Select placeholder="选择一个物理网卡作为外网口" allowClear>
              {phys.map((n) => (
                <Select.Option key={n.name} value={n.name}>
                  {n.name} · {n.mac} {n.bound && n.bound !== editing?.id ? `（占用:${n.bound}）` : ''}
                </Select.Option>
              ))}
            </Select>
          </Form.Item>
        )}

        {role === 'wan' && (
          <Form.Item label="接入方式" name="proto">
            <Radio.Group optionType="button" buttonStyle="solid">
              <Radio.Button value="dhcp">DHCP 动态</Radio.Button>
              <Radio.Button value="pppoe">PPPoE 拨号</Radio.Button>
              <Radio.Button value="static">静态 IP</Radio.Button>
            </Radio.Group>
          </Form.Item>
        )}

        {/* PPPoE */}
        {role === 'wan' && proto === 'pppoe' && (
          <>
            <Form.Item label="账号" name="username" rules={[{ required: true, message: '请输入 PPPoE 账号' }]}>
              <Input placeholder="宽带账号" />
            </Form.Item>
            <Form.Item label="密码" name="password">
              <Input.Password placeholder="宽带密码" />
            </Form.Item>
            <Form.Item label="服务器名称" name="service" tooltip="一般不填">
              <Input />
            </Form.Item>
            <Form.Item label="AC 名称" name="ac" tooltip="一般不填">
              <Input />
            </Form.Item>
          </>
        )}

        {/* 静态 / 内网 地址 */}
        {(role === 'lan' || (role === 'wan' && proto === 'static')) && (
          <>
            <Form.Item label="IP 地址" name="ipaddr" rules={[{ required: true, message: '请输入 IP 地址' }]}>
              <Input placeholder="192.168.2.1" />
            </Form.Item>
            <Form.Item label="子网掩码" name="netmask">
              <Select showSearch>
                {masks.map((m) => <Select.Option key={m} value={m}>{m}</Select.Option>)}
              </Select>
            </Form.Item>
            {role === 'wan' && (
              <Form.Item label="网关" name="gateway">
                <Input placeholder="119.x.x.1" />
              </Form.Item>
            )}
          </>
        )}

        {/* DNS（静态 WAN） */}
        {role === 'wan' && proto === 'static' && (
          <Row gutter={12}>
            <Col span={12}><Form.Item label="首选 DNS" name="dns_primary"><Input placeholder="223.5.5.5" /></Form.Item></Col>
            <Col span={12}><Form.Item label="备选 DNS" name="dns_secondary"><Input placeholder="114.114.114.114" /></Form.Item></Col>
          </Row>
        )}

        {role === 'wan' && (
          <Form.Item label="设为默认网关" name="default_gw" valuePropName="checked" tooltip="多 WAN 时只选一条作为默认出口">
            <Switch />
          </Form.Item>
        )}

        {editing && role === 'wan' && (
          <Form.Item label="运行状态">
            <Space>
              {editing.up ? <Tag color="success">已连接</Tag> : <Tag>未连接</Tag>}
              {editing.runtime_ip && <Text>当前 IP：{editing.runtime_ip}</Text>}
            </Space>
          </Form.Item>
        )}

        <Form.Item label="MTU" name="mtu">
          <InputNumber min={576} max={9200} style={{ width: 160 }} />
        </Form.Item>
        <Form.Item label="克隆 MAC" name="clone_mac" tooltip="留空使用网卡原 MAC">
          <Input placeholder="留空不克隆" />
        </Form.Item>
        <Form.Item label="备注" name="remark">
          <Input />
        </Form.Item>
        <Paragraph type="secondary" style={{ fontSize: 12 }}>
          <Tooltip title="保存即写入 /etc/config/network 并 reload，修改内网 IP / 绑定网卡可能短暂断网">
            提示：保存后会下发到 OpenWrt 并重载网络。
          </Tooltip>
        </Paragraph>
      </Form>
    </Drawer>
  );
}
