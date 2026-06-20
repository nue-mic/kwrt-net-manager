import { useEffect, useMemo, useState } from 'react';
import { Alert, App, AutoComplete, Button, Drawer, Form, Input, Popconfirm, Select, Space, Table, Tag, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined, ThunderboltOutlined } from '@ant-design/icons';
import PageCard from '../../components/PageCard';
import { useNetData, extractErr } from '../../hooks/useNetData';
import type { BatchAction } from '../../api/netcfg';
import * as ddns from '../../api/ddns';

interface DdnsForm {
  provider: string;
  domain: string;
  auth_mode: 'token' | 'userpass';
  username: string;
  password: string;
  ip_source: 'web' | 'network' | 'device';
  interface: string;
  mac: string;
  record_type: 'A' | 'AAAA';
  remark: string;
}

const resultTag = (v?: string) => {
  if (!v) return <Tag>-</Tag>;
  if (v === '成功') return <Tag color="success">{v}</Tag>;
  if (v === '失败') return <Tag color="error">{v}</Tag>;
  if (v === '已停用') return <Tag>{v}</Tag>;
  return <Tag color="processing">{v}</Tag>;
};

export default function DdnsListPage() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<ddns.DDNSEntry[]>(() => ddns.listDDNS(), []);
  const [svc, setSvc] = useState<ddns.DDNSSvcInfo | null>(null);
  const [devices, setDevices] = useState<ddns.DDNSDevice[]>([]);
  const [installing, setInstalling] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [keyword, setKeyword] = useState('');
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<ddns.DDNSEntry | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<DdnsForm>();

  const loadSvc = async () => {
    try {
      setSvc(await ddns.getDDNSService());
    } catch {
      /* 忽略 */
    }
  };
  const loadDevices = async () => {
    try {
      setDevices(await ddns.listDDNSDevices());
    } catch {
      /* 忽略：非 OpenWrt 环境无设备 */
    }
  };
  useEffect(() => {
    void loadSvc();
  }, []);

  // 设备下拉：主机名 / MAC / 当前 GUA 供搜索，value 取 MAC。
  const deviceOptions = useMemo(
    () =>
      devices.map((d) => ({
        value: d.mac,
        label: `${d.hostname || '未知设备'}${d.vendor ? `（${d.vendor}）` : ''} · ${d.mac}${d.ipv6 ? ` · ${d.ipv6}` : '（暂无 GUA）'}`,
      })),
    [devices],
  );

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    if (!kw) return data;
    return data.filter((r) => [r.provider, r.domain, r.remark, r.current_ip].some((f) => (f ?? '').toLowerCase().includes(kw)));
  }, [data, keyword]);

  const openDrawer = (record?: ddns.DDNSEntry) => {
    setEditing(record ?? null);
    void loadDevices();
    if (record) {
      form.setFieldsValue({ ...record });
    } else {
      form.resetFields();
      form.setFieldsValue({ auth_mode: 'token', ip_source: 'web', record_type: 'A', interface: 'wan', mac: '', provider: svc?.providers?.[0] });
    }
    setOpen(true);
  };

  const onSave = async () => {
    let v: DdnsForm;
    try {
      v = await form.validateFields();
    } catch {
      return;
    }
    const isDevice = v.ip_source === 'device';
    const body: ddns.DDNSInput = {
      provider: v.provider,
      domain: v.domain,
      auth_mode: v.auth_mode,
      username: v.username ?? '',
      password: v.password ?? '',
      ip_source: v.ip_source,
      interface: isDevice ? 'lan' : v.interface ?? 'wan',
      mac: isDevice ? (v.mac ?? '').trim() : '',
      record_type: isDevice ? 'AAAA' : v.record_type, // device 仅支持 IPv6
      enabled: editing ? editing.enabled : true,
      remark: v.remark ?? '',
    };
    setSaving(true);
    try {
      if (editing) await ddns.updateDDNS(editing.id, body);
      else await ddns.createDDNS(body);
      message.success(editing ? '已更新' : '已添加');
      setOpen(false);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setSaving(false);
    }
  };

  const onToggle = async (r: ddns.DDNSEntry) => {
    try {
      await ddns.toggleDDNS(r.id, !r.enabled);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };
  const onDelete = async (r: ddns.DDNSEntry) => {
    try {
      await ddns.deleteDDNS(r.id);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };
  const onBatch = async (action: BatchAction) => {
    try {
      await ddns.batchDDNS(action, selected);
      setSelected([]);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };
  const onInstall = async () => {
    setInstalling(true);
    try {
      await ddns.installDDNS();
      message.success('DDNS 组件安装完成');
      void loadSvc();
      reload();
    } catch (e) {
      message.error('安装失败：' + extractErr(e));
    } finally {
      setInstalling(false);
    }
  };

  const columns: ColumnsType<ddns.DDNSEntry> = [
    { title: '服务商', dataIndex: 'provider', width: 140 },
    { title: '域名', dataIndex: 'domain' },
    {
      title: '解析方式',
      dataIndex: 'ip_source',
      width: 130,
      render: (v: string, r) =>
        v === 'device' ? (
          <Tooltip title={`目标终端 ${r.mac || ''}`}>
            <Tag color="purple">终端解析</Tag>
          </Tooltip>
        ) : v === 'network' ? (
          '接口IP'
        ) : (
          '互联网出口IP'
        ),
    },
    { title: '记录类型', dataIndex: 'record_type', width: 100, render: (v: string) => <Tag>{v}</Tag> },
    { title: '更新结果', dataIndex: 'last_result', width: 100, render: (v: string) => resultTag(v) },
    { title: 'IP地址', dataIndex: 'current_ip', width: 200, render: (v: string) => v || '--' },
    { title: '更新时间', dataIndex: 'last_update', width: 170, render: (v: string) => v || '--' },
    { title: '状态', dataIndex: 'enabled', width: 90, render: (v: boolean) => (v ? <Tag color="success">已启用</Tag> : <Tag>已停用</Tag>) },
    {
      title: '操作',
      width: 160,
      fixed: 'right',
      render: (_, r) => (
        <Space size="middle">
          <Typography.Link onClick={() => openDrawer(r)}>编辑</Typography.Link>
          <Typography.Link onClick={() => onToggle(r)}>{r.enabled ? '停用' : '启用'}</Typography.Link>
          <Popconfirm title="确认删除？" onConfirm={() => onDelete(r)}>
            <Typography.Link type="danger">删除</Typography.Link>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const uninstalled = svc !== null && !svc.installed;

  return (
    <PageCard
      breadcrumb={['高级应用', '动态域名']}
      title="动态域名 DDNS"
      toolbar={
        <>
          <Space>
            <Input.Search allowClear placeholder="搜索 服务商/域名/IP" style={{ width: 240 }} value={keyword} onChange={(e) => setKeyword(e.target.value)} />
          </Space>
          <Space>
            {uninstalled && (
              <Tooltip title="安装 ddns-scripts 后才能下发到 OpenWrt">
                <Button icon={<ThunderboltOutlined />} loading={installing} onClick={onInstall}>
                  一键安装 DDNS 组件
                </Button>
              </Tooltip>
            )}
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openDrawer()}>
              添加
            </Button>
            <Button disabled={selected.length === 0} onClick={() => onBatch('enable')}>启用</Button>
            <Button disabled={selected.length === 0} onClick={() => onBatch('disable')}>停用</Button>
            <Popconfirm title="确认删除选中项？" disabled={selected.length === 0} onConfirm={() => onBatch('delete')}>
              <Button danger disabled={selected.length === 0}>删除</Button>
            </Popconfirm>
          </Space>
        </>
      }
    >
      {uninstalled && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="未安装 ddns-scripts，配置可先保存，安装后自动下发。点右上角「一键安装 DDNS 组件」（需联网）。"
        />
      )}
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="解析方式：互联网出口 IP / 接口 IP 走 OpenWrt 原生 ddns-scripts；「按终端解析」由 kwrtmgrd 把某 LAN 设备当前的稳定全球 IPv6（GUA）解析出来再推送，仅支持 AAAA（IPv6），隐私/临时地址设备可能无法稳定锁定。"
      />
      <Table
        rowKey="id"
        size="small"
        bordered
        loading={loading}
        dataSource={filtered}
        scroll={{ x: 'max-content' }}
        rowSelection={{ selectedRowKeys: selected, onChange: (k) => setSelected(k as string[]) }}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
      />
      <Drawer
        title={editing ? '编辑动态域名' : '添加动态域名'}
        width="min(92vw, 640px)"
        open={open}
        onClose={() => setOpen(false)}
        destroyOnClose
        extra={
          <Space>
            <Button onClick={() => setOpen(false)}>取消</Button>
            <Button type="primary" loading={saving} onClick={onSave}>保存</Button>
          </Space>
        }
      >
        <Form form={form} layout="vertical">
          <Form.Item name="provider" label="服务商" rules={[{ required: true, message: '请选择服务商' }]}>
            <Select
              showSearch
              placeholder="如 cloudflare.com"
              options={(svc?.providers ?? []).map((p) => ({ label: p, value: p }))}
            />
          </Form.Item>
          <Form.Item name="domain" label="域名（要更新的记录）" rules={[{ required: true, message: '请输入域名' }]}>
            <Input placeholder="如 home.example.com" allowClear />
          </Form.Item>
          <Form.Item name="auth_mode" label="配置方式" rules={[{ required: true }]}>
            <Select
              options={[
                { label: 'API Token / 密钥', value: 'token' },
                { label: '账号 + 密码', value: 'userpass' },
              ]}
            />
          </Form.Item>
          <Form.Item name="username" label="Zone / 账号（可空）" extra="Cloudflare 等用 Token 时可留空；部分服务商需填 zone 或账号。">
            <Input placeholder="可空" allowClear />
          </Form.Item>
          <Form.Item name="password" label="API Token / 密钥 / 密码" rules={[{ required: true, message: '请输入凭据' }]}>
            <Input.Password placeholder="服务商签发的 Token 或密码" allowClear />
          </Form.Item>
          <Form.Item name="ip_source" label="解析方式" rules={[{ required: true }]}>
            <Select
              onChange={(val) => {
                if (val === 'device') form.setFieldsValue({ record_type: 'AAAA' });
              }}
              options={[
                { label: '互联网出口 IP（探测公网 IP）', value: 'web' },
                { label: '接口 IP（取指定网卡地址）', value: 'network' },
                { label: '按终端解析（某 LAN 设备当前 IPv6）', value: 'device' },
              ]}
            />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(p, c) => p.ip_source !== c.ip_source || p.mac !== c.mac}>
            {({ getFieldValue }) => {
              const src = getFieldValue('ip_source');
              if (src === 'network') {
                return (
                  <Form.Item name="interface" label="解析网卡" rules={[{ required: true, message: '请输入网卡名' }]} extra="如 wan。">
                    <Input placeholder="wan" allowClear />
                  </Form.Item>
                );
              }
              if (src === 'device') {
                const mac = (getFieldValue('mac') || '').toUpperCase();
                const sel = devices.find((d) => d.mac.toUpperCase() === mac);
                return (
                  <>
                    <Alert
                      type="info"
                      showIcon
                      style={{ marginBottom: 12 }}
                      message="按终端解析仅支持 IPv6（AAAA）。kwrtmgrd 会持续把该终端当前的稳定全球 IPv6（GUA）解析出来更新到域名；ddns-scripts 负责推送。"
                    />
                    <Form.Item
                      name="mac"
                      label="目标终端"
                      rules={[{ required: true, message: '请选择或输入终端 MAC' }]}
                      extra="可从下拉选当前在线设备，或手动输入 MAC（如 00:11:22:aa:bb:cc）。隐私/临时地址设备可能无法稳定锁定。"
                    >
                      <AutoComplete
                        options={deviceOptions}
                        placeholder="选择设备或输入 MAC"
                        filterOption={(input, option) => (option?.value ?? '').toLowerCase().includes(input.toLowerCase())}
                        allowClear
                      />
                    </Form.Item>
                    {sel &&
                      (sel.ipv6 ? (
                        <Alert type="success" showIcon style={{ marginBottom: 12 }} message={`当前解析到：${sel.ipv6}（来源：${sel.source ?? '-'}）`} />
                      ) : (
                        <Alert
                          type="warning"
                          showIcon
                          style={{ marginBottom: 12 }}
                          message="该设备当前未发现可用全球 IPv6（GUA）：可能离线、未启用 IPv6、或仅有临时/隐私地址。"
                        />
                      ))}
                  </>
                );
              }
              return null;
            }}
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(p, c) => p.ip_source !== c.ip_source}>
            {({ getFieldValue }) => {
              const isDevice = getFieldValue('ip_source') === 'device';
              return (
                <Form.Item name="record_type" label="记录类型" rules={[{ required: true }]} extra={isDevice ? '按终端解析仅支持 AAAA（IPv6）。' : undefined}>
                  <Select
                    disabled={isDevice}
                    options={[
                      { label: 'A 记录（IPv4）', value: 'A' },
                      { label: 'AAAA 记录（IPv6）', value: 'AAAA' },
                    ]}
                  />
                </Form.Item>
              );
            }}
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input placeholder="可空" allowClear />
          </Form.Item>
        </Form>
      </Drawer>
    </PageCard>
  );
}
