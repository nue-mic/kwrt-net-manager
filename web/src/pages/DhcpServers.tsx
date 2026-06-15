import { useEffect, useMemo, useState } from 'react';
import {
  App,
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import PageCard from '../components/PageCard';
import { useNetData, extractErr } from '../hooks/useNetData';
import * as net from '../api/netcfg';

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
  exclude_text: string;
  netmask: string;
  gateway: string;
  dns_primary: string;
  dns_secondary: string;
  lease_minutes: number;
  expired_keep_hours: number;
  check_ip: boolean;
  relay_only: boolean;
  assoc_interface: string;
  custom_options: CustomOptionRow[];
}

export default function DhcpServersPage() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<net.DHCPServer[]>(() => net.listServers(), []);

  const [status, setStatus] = useState<net.NetStatus | null>(null);
  const [interfaces, setInterfaces] = useState<net.NetInterface[]>([]);
  const [keyword, setKeyword] = useState('');
  const [selected, setSelected] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<net.DHCPServer | null>(null);
  const [saving, setSaving] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [batching, setBatching] = useState(false);
  const [form] = Form.useForm<ServerFormValues>();

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
    })();
    return () => {
      alive = false;
    };
  }, [message]);

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
        exclude_text: record.exclude.join('\n'),
        netmask: record.netmask,
        gateway: record.gateway,
        dns_primary: record.dns_primary,
        dns_secondary: record.dns_secondary,
        lease_minutes: record.lease_minutes,
        expired_keep_hours: record.expired_keep_hours,
        check_ip: record.check_ip,
        relay_only: record.relay_only,
        assoc_interface: record.assoc_interface || 'all',
        custom_options: record.custom_options.map((o) => ({ code: o.code, value: o.value })),
      });
    } else {
      form.resetFields();
      form.setFieldsValue({
        exclude_text: '',
        lease_minutes: 120,
        expired_keep_hours: 0,
        check_ip: true,
        relay_only: false,
        assoc_interface: 'all',
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
        netmask: v.netmask,
        gateway: v.gateway,
        dns_primary: v.dns_primary,
        dns_secondary: v.dns_secondary,
        lease_minutes: v.lease_minutes,
        exclude,
        expired_keep_hours: v.expired_keep_hours,
        check_ip: v.check_ip,
        relay_only: v.relay_only,
        assoc_interface: v.assoc_interface || 'all',
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

  const onRestart = async () => {
    setRestarting(true);
    try {
      await net.restartDHCP();
      message.success('DHCP 服务已重启');
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setRestarting(false);
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
      render: (v: number) => `${v} 分`,
    },
    {
      title: '过期地址保留时间',
      dataIndex: 'expired_keep_hours',
      width: 140,
      render: (v: number) => `${v} 时`,
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
              {status?.dhcp_ok ? (
                <Tag color="success">服务正常</Tag>
              ) : (
                <Tag color="warning">{status?.message || '未就绪'}</Tag>
              )}
            </Space>
            <Input.Search
              allowClear
              placeholder="搜索接口 / 网关"
              style={{ width: 220 }}
              onChange={(e) => setKeyword(e.target.value)}
              onSearch={setKeyword}
            />
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
        width={520}
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
            rules={[{ required: true, message: '请输入子网掩码' }]}
          >
            <Input placeholder="例如 255.255.255.0" />
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
          <Form.Item
            label="租期（分钟）"
            name="lease_minutes"
            rules={[{ required: true, message: '请输入租期' }]}
          >
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item label="过期地址保留时间（小时）" name="expired_keep_hours">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item label="检查接口IP有效性" name="check_ip" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item label="只应用于DHCP中继" name="relay_only" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item label="关联接口" name="assoc_interface">
            <Input placeholder="all" />
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
                <Button
                  type="dashed"
                  block
                  icon={<PlusOutlined />}
                  onClick={() => add({ code: null, value: '' })}
                >
                  添加选项
                </Button>
              </div>
            )}
          </Form.List>
        </Form>
      </Drawer>
    </PageCard>
  );
}
