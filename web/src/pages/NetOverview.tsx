import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Alert, App, Button, Card, Checkbox, Col, Collapse, Drawer, Form, Input, InputNumber,
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

function maskToPrefix(mask: string): number {
  const m = mask.split('.').map(Number);
  if (m.length !== 4 || m.some((x) => isNaN(x))) return 24;
  return m.reduce((acc, o) => acc + (o.toString(2).match(/1/g)?.length || 0), 0);
}
function prefixToMask(p: number): string {
  const full = Math.floor(p / 8), rem = p % 8;
  const o = [0, 0, 0, 0].map((_, i) => (i < full ? 255 : i === full ? 256 - 2 ** (8 - rem) : 0));
  return o.join('.');
}

// IPv4 子网掩码：完整列出 /8–/32（点分十进制，覆盖所有常用网段），再附 /1–/7 超大网段，求全面。
const PREFIXES = [
  ...Array.from({ length: 25 }, (_, i) => i + 8), // /8 ~ /32
  ...Array.from({ length: 7 }, (_, i) => i + 1),  // /1 ~ /7
].map((v) => ({ v, t: `/${v} (${prefixToMask(v)})` }));

// IPv6 前缀长度：覆盖常用子网边界（/64 为标准），尽量全面。
const PREFIXES6 = [128, 127, 126, 125, 124, 120, 116, 112, 104, 96, 88, 80, 72, 68, 64, 60, 58, 56, 52, 48, 44, 40, 36, 32, 28, 24, 20, 16, 12, 8];

export default function NetOverview() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<net.NetOverview>(() => net.getNetOverview(), {
    wan_count: 0, wan_up: 0, connections: 0, lan_count: 0, lan_up: 0, dhcp_on: 0, terminals: 0, free_ports: 0, wans: [], lans: [],
  }, { pollMs: 6000 });
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
            ips={ifaceIps(w, nics)}
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
            sub={l.up ? '已连接' : '未连接'}
            ips={ifaceIps(l, nics)}
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

// ifaceIps 汇总一个接口要显示的全部 IP：优先取该 device 网卡的真实内核地址（ip addr，
// 含静态/DHCP/PD/SLAAC 等全部），过滤链路本地(fe80)/回环；运行态拿不到时（dev/store）
// 回退到配置态（主 IP + 附加 IP）。IPv4 在前、IPv6 在后，便于阅读。
function ifaceIps(iface: net.NetIface, nics: net.NIC[]): string[] {
  const skip = (cidr: string) => {
    const ip = cidr.split('/')[0].toLowerCase();
    return ip.startsWith('fe80') || ip === '::1' || ip.startsWith('127.');
  };
  const nic = nics.find((n) => n.name === iface.device);
  let ips = (nic?.ip_addrs || []).filter((a) => !skip(a));
  if (ips.length === 0) {
    const cfg: string[] = [];
    if (iface.ipaddr) cfg.push(iface.netmask ? `${iface.ipaddr}/${maskToPrefix(iface.netmask)}` : iface.ipaddr);
    (iface.extra_addrs || []).forEach((a) => { if (a.address) cfg.push(`${a.address}/${a.prefix}`); });
    if (cfg.length === 0 && iface.runtime_ip) cfg.push(iface.runtime_ip);
    ips = cfg;
  }
  return [...ips].sort((a, b) => Number(a.includes(':')) - Number(b.includes(':')));
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

function PortCard({ title, sub, color, icon, onClick, ips }: { title: string; sub: string; color: string; icon: React.ReactNode; onClick?: () => void; ips?: string[] }) {
  return (
    <Card
      size="small"
      hoverable={!!onClick}
      onClick={onClick}
      style={{ width: 200, textAlign: 'center', cursor: onClick ? 'pointer' : 'default', borderColor: color }}
      styles={{ body: { padding: '12px 10px' } }}
    >
      <div style={{ fontSize: 26, color }}>{icon}</div>
      <div style={{ fontWeight: 600, marginTop: 4 }}>{title}</div>
      {ips && ips.length > 0 && (
        <div style={{ margin: '4px 0' }}>
          {ips.map((ip) => (
            <Text key={ip} style={{ display: 'block', fontSize: 11, lineHeight: 1.5, wordBreak: 'break-all' }}>{ip}</Text>
          ))}
        </div>
      )}
      <Text type="secondary" style={{ fontSize: 11 }}>{sub}</Text>
    </Card>
  );
}

interface DrawerProps {
  open: boolean;
  role: 'lan' | 'wan';
  editing: net.NetIface | null;
  nics: net.NIC[];
  onClose: () => void;
  onSaved: () => void;
  onAction: (id: string, action: 'connect' | 'disconnect' | 'restart') => void;
  onDelete: (id: string) => void;
}

function IfaceDrawer({ open, role, editing, nics, onClose, onSaved, onAction, onDelete }: DrawerProps) {
  const { message } = App.useApp();
  const navigate = useNavigate();
  const [form] = Form.useForm();
  const [saving, setSaving] = useState(false);
  const proto = Form.useWatch('proto', form) as string | undefined;

  // 可绑定的物理网卡：空闲 + 当前接口已绑定的
  const phys = useMemo(() => nics.filter((n) => n.kind === 'physical'), [nics]);

  useEffect(() => {
    if (!open) return;
    if (editing) {
      form.setFieldsValue({
        id: editing.id, proto: editing.proto, ipaddr: editing.ipaddr,
        prefix: editing.netmask ? maskToPrefix(editing.netmask) : 24,
        gateway: editing.gateway, dns_primary: editing.dns_primary, dns_secondary: editing.dns_secondary,
        username: editing.username, password: editing.password, service: editing.service, ac: editing.ac,
        mtu: editing.mtu || 1500, default_gw: editing.default_gw, remark: editing.remark,
        ports: editing.ports, device: editing.device, clone_mac: editing.clone_mac,
        extra_addrs: editing.extra_addrs || [],
        metric: editing.metric || 0, peerdns: editing.peerdns ?? undefined,
        broadcast: editing.broadcast || '', force_link: editing.force_link ?? undefined,
        auto: editing.auto ?? undefined, ip6assign: editing.ip6assign || 0,
        ip6hint: editing.ip6hint || '', ip6gw: editing.ip6gw || '',
        ip6prefix: editing.ip6prefix || '', ip6ifaceid: editing.ip6ifaceid || '',
        keepalive: editing.keepalive || '', pppoe_ipv6: editing.pppoe_ipv6 ?? undefined,
      });
    } else {
      form.setFieldsValue({
        id: '', proto: role === 'wan' ? 'dhcp' : 'static', prefix: 24,
        mtu: 1500, default_gw: true, ports: [], device: '', extra_addrs: [],
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
    // G3：编辑「当前内网」且改了主 IP/子网时二次确认（保存后可能短暂断连）
    const ipChanged =
      editing != null && role === 'lan' &&
      (v.ipaddr !== editing.ipaddr || (v.ipaddr ? prefixToMask(v.prefix || 24) : '') !== editing.netmask);
    if (ipChanged) {
      const go = await new Promise<boolean>((res) => {
        Modal.confirm({
          title: '更改内网管理地址',
          content: `将把 ${editing!.ipaddr} 改为 ${v.ipaddr}/${v.prefix}。保存后本机网络会重载，当前页面可能短暂断连，请用新地址 ${v.ipaddr} 重新访问。确定继续？`,
          okText: '确定更改', cancelText: '取消',
          onOk: () => res(true), onCancel: () => res(false),
        });
      });
      if (!go) return;
    }
    const ports: string[] = v.ports || [];
    const extra: net.IfaceAddr[] = (v.extra_addrs || [])
      .filter((a: any) => a && a.address)
      .map((a: any) => {
        const family: 'ipv4' | 'ipv6' = a.family === 'ipv6' ? 'ipv6' : 'ipv4';
        return {
          address: a.address,
          prefix: a.prefix || (family === 'ipv6' ? 64 : 24),
          family,
          remark: a.remark || '',
          enabled: true,
        };
      });
    const body: net.NetIfaceInput = {
      id: editing?.id || v.id || '',
      name: editing?.name || v.id || '',
      role,
      proto: role === 'lan' ? 'static' : v.proto,
      device: role === 'wan' ? (ports[0] || v.device || '') : (editing?.device || ''),
      ports,
      ipaddr: v.ipaddr || '',
      netmask: v.ipaddr ? prefixToMask(v.prefix || 24) : '',
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
      extra_addrs: extra,
      metric: v.metric || 0,
      peerdns: v.peerdns,
      broadcast: v.broadcast || '',
      force_link: v.force_link,
      auto: v.auto,
      ip6assign: v.ip6assign || 0,
      ip6hint: v.ip6hint || '',
      ip6gw: v.ip6gw || '',
      ip6prefix: v.ip6prefix || '',
      ip6ifaceid: v.ip6ifaceid || '',
      keepalive: v.keepalive || '',
      pppoe_ipv6: v.pppoe_ipv6,
    };
    setSaving(true);
    try {
      if (editing) await net.updateNetIface(editing.id, body);
      else await net.createNetIface(body);
      if (ipChanged) message.success(`已保存，若无法访问请用新地址 ${v.ipaddr} 打开`);
      else message.success('已保存（已下发并重载网络）');
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
      width="min(92vw, 680px)"
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
            <Form.Item label="保活间隔 (keepalive)" name="keepalive" tooltip='格式"失败次数 间隔秒"，如 "5 25"；留空用默认'>
              <Input placeholder="5 25" style={{ width: 200 }} />
            </Form.Item>
            <Form.Item label="PPPoE 启用 IPv6" name="pppoe_ipv6" tooltip="在 PPPoE 链路上获取/分配 IPv6；留空=默认">
              <Select allowClear placeholder="默认" options={[{ value: true, label: '是' }, { value: false, label: '否' }]} style={{ width: 160 }} />
            </Form.Item>
          </>
        )}

        {/* 静态 / 内网 地址 */}
        {(role === 'lan' || (role === 'wan' && proto === 'static')) && (
          <>
            <Form.Item label="IP 地址" name="ipaddr" rules={[{ required: true, message: '请输入 IP 地址' }]}>
              <Input placeholder="192.168.2.1" />
            </Form.Item>
            <Form.Item label="子网掩码" name="prefix">
              <Select showSearch optionFilterProp="label"
                options={PREFIXES.map((p) => ({ value: p.v, label: p.t }))} />
            </Form.Item>
            <Form.Item label="附加 IP" tooltip="同接口的次地址，可同/异子网，仅作管理/路由，不发 DHCP；需发地址请新建内网口">
              <Text type="secondary" style={{ display: 'block', fontSize: 12, marginBottom: 8 }}>
                支持 IPv4 / IPv6 附加地址（次地址），可同/异子网，仅作管理/路由，均不发 DHCP。
              </Text>
              <Form.List name="extra_addrs">
                {(fields, { add, remove }) => (
                  <Space direction="vertical" style={{ width: '100%' }} size={6}>
                    {fields.map(({ key, name, ...rest }) => (
                      <Space key={key} align="baseline" wrap>
                        <Form.Item {...rest} name={[name, 'family']} noStyle initialValue="ipv4">
                          <Select
                            style={{ width: 84 }}
                            options={[{ value: 'ipv4', label: 'IPv4' }, { value: 'ipv6', label: 'IPv6' }]}
                            onChange={(v) => form.setFieldValue(['extra_addrs', name, 'prefix'], v === 'ipv6' ? 64 : 24)}
                          />
                        </Form.Item>
                        <Form.Item {...rest} name={[name, 'address']} noStyle rules={[{ required: true, message: 'IP' }]}>
                          <Input placeholder="10.0.0.1" style={{ width: 170 }} />
                        </Form.Item>
                        <Form.Item noStyle shouldUpdate>
                          {() => {
                            const fam = form.getFieldValue(['extra_addrs', name, 'family']) || 'ipv4';
                            const opts = fam === 'ipv6'
                              ? PREFIXES6.map((p) => ({ value: p, label: '/' + p }))
                              : PREFIXES.map((p) => ({ value: p.v, label: '/' + p.v }));
                            return (
                              <Form.Item {...rest} name={[name, 'prefix']} noStyle initialValue={24}>
                                <Select showSearch optionFilterProp="label" style={{ width: 110 }} options={opts} />
                              </Form.Item>
                            );
                          }}
                        </Form.Item>
                        <Form.Item {...rest} name={[name, 'remark']} noStyle>
                          <Input placeholder="备注" style={{ width: 120 }} />
                        </Form.Item>
                        <DeleteOutlined onClick={() => remove(name)} />
                      </Space>
                    ))}
                    <Button type="dashed" onClick={() => add({ family: 'ipv4', prefix: 24 })} icon={<PlusOutlined />} block>新增附加 IP</Button>
                  </Space>
                )}
              </Form.List>
            </Form.Item>
            {role === 'wan' && (
              <Form.Item label="网关" name="gateway">
                <Input placeholder="119.x.x.1" />
              </Form.Item>
            )}
            {editing && role === 'lan' && (
              <Form.Item>
                <Button type="link" style={{ paddingLeft: 0 }} icon={<ThunderboltOutlined />}
                  onClick={() => navigate(
                    '/dhcp/servers?iface=' + encodeURIComponent(editing.id) +
                    '&ip=' + encodeURIComponent(editing.ipaddr || '') +
                    '&mask=' + encodeURIComponent(editing.netmask || ''),
                  )}>
                  为该内网启用 DHCP（去 DHCP 服务端配置）
                </Button>
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

        <Collapse ghost items={[{
          key: 'adv', label: '高级设置',
          children: (
            <>
              {role === 'wan' && (
                <Form.Item label="线路优先级 (metric)" name="metric" tooltip="多 WAN 时数值越小越优先">
                  <InputNumber min={0} max={9999} style={{ width: 160 }} addonAfter={
                    <Space size={4}>
                      <a onClick={() => form.setFieldValue('metric', 0)}>主</a>
                      <a onClick={() => form.setFieldValue('metric', 100)}>备</a>
                    </Space>} />
                </Form.Item>
              )}
              <Form.Item label="使用上游下发 DNS (peerdns)" name="peerdns" tooltip="留空=默认；关闭则只用手填 DNS">
                <Select allowClear placeholder="默认" options={[{ value: true, label: '是' }, { value: false, label: '否' }]} style={{ width: 160 }} />
              </Form.Item>
              <Form.Item label="开机自启 (auto)" name="auto">
                <Select allowClear placeholder="默认(是)" options={[{ value: true, label: '是' }, { value: false, label: '否' }]} style={{ width: 160 }} />
              </Form.Item>
              <Form.Item label="无链路也配置 (force_link)" name="force_link">
                <Select allowClear placeholder="默认" options={[{ value: true, label: '是' }, { value: false, label: '否' }]} style={{ width: 160 }} />
              </Form.Item>
              <Form.Item label="广播地址" name="broadcast"><Input placeholder="192.168.1.255" /></Form.Item>
              <Form.Item label="MTU" name="mtu"><InputNumber min={576} max={9200} style={{ width: 160 }} /></Form.Item>
              <Form.Item label="克隆 MAC" name="clone_mac" tooltip="留空使用网卡原 MAC"><Input placeholder="AA:BB:CC:DD:EE:FF" /></Form.Item>
              <Form.Item label="IPv6 委派前缀 (ip6assign)" name="ip6assign" tooltip="LAN 常用 60；0=不设"><InputNumber min={0} max={64} style={{ width: 160 }} /></Form.Item>
              <Form.Item label="IPv6 子前缀提示 (ip6hint)" name="ip6hint"><Input placeholder="hex，如 10" /></Form.Item>
              <Form.Item label="IPv6 网关 (ip6gw)" name="ip6gw"><Input placeholder="2001:db8::1" /></Form.Item>
              <Form.Item label="IPv6 委派前缀 (ip6prefix)" name="ip6prefix" tooltip="向下游分发的前缀 CIDR"><Input placeholder="2001:db8:1::/48" /></Form.Item>
              <Form.Item label="IPv6 接口 ID (ip6ifaceid)" name="ip6ifaceid" tooltip="接口 ID 后缀"><Input placeholder="::1" /></Form.Item>
              <Form.Item label="备注" name="remark"><Input /></Form.Item>
            </>
          ),
        }]} />
        <Paragraph type="secondary" style={{ fontSize: 12 }}>
          <Tooltip title="保存即写入 /etc/config/network 并 reload，修改内网 IP / 绑定网卡可能短暂断网">
            提示：保存后会下发到 OpenWrt 并重载网络。
          </Tooltip>
        </Paragraph>
      </Form>
    </Drawer>
  );
}
