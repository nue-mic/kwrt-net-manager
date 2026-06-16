import { useMemo, useState } from 'react';
import {
  App,
  Button,
  Checkbox,
  Drawer,
  Form,
  Input,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { CheckboxChangeEvent } from 'antd/es/checkbox';
import { PlusOutlined } from '@ant-design/icons';
import { useSearchParams } from 'react-router-dom';
import PageCard from '../components/PageCard';
import { useNetData, extractErr } from '../hooks/useNetData';
import * as net from '../api/netcfg';

interface StaticForm {
  hostname: string;
  ip: string;
  mac: string;
  gateway: string;
  interface: string;
  dns_primary: string;
  dns_secondary: string;
  remark: string;
  route_push: boolean;
}

export default function DhcpStaticsPage() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<{ items: net.StaticLease[]; arp_bind: boolean }>(
    () => net.listStatics(),
    { items: [], arp_bind: false },
  );
  const { data: ifaces } = useNetData<net.NetInterface[]>(() => net.listInterfaces(), []);

  const [searchParams] = useSearchParams();
  const [selected, setSelected] = useState<string[]>([]);
  // 从终端列表「查看」跳转过来时，用 ?q=<ip> 预填搜索，定位到该条静态分配。
  const [keyword, setKeyword] = useState(searchParams.get('q') ?? '');
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<net.StaticLease | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<StaticForm>();

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    if (!kw) return data.items;
    return data.items.filter((r) =>
      [r.ip, r.mac, r.remark, r.hostname].some((f) => (f ?? '').toLowerCase().includes(kw)),
    );
  }, [data.items, keyword]);

  const ifaceOptions = useMemo(
    () => ifaces.map((i) => ({ label: i.name, value: i.name })),
    [ifaces],
  );

  const onToggleArpBind = async (e: CheckboxChangeEvent) => {
    try {
      await net.setARPBind(e.target.checked);
      message.success('已保存');
      reload();
    } catch (err) {
      message.error(extractErr(err));
    }
  };

  const openDrawer = (record?: net.StaticLease) => {
    setEditing(record ?? null);
    if (record) {
      form.setFieldsValue({
        hostname: record.hostname,
        ip: record.ip,
        mac: record.mac,
        gateway: record.gateway,
        interface: record.interface,
        dns_primary: record.dns_primary,
        dns_secondary: record.dns_secondary,
        remark: record.remark,
        route_push: record.route_push,
      });
    } else {
      form.resetFields();
    }
    setOpen(true);
  };

  const onSave = async () => {
    let v: StaticForm;
    try {
      v = await form.validateFields();
    } catch {
      return;
    }
    const body: net.StaticLeaseInput = {
      hostname: v.hostname ?? '',
      ip: v.ip,
      mac: v.mac,
      gateway: v.gateway ?? '',
      interface: v.interface ?? '',
      dns_primary: v.dns_primary ?? '',
      dns_secondary: v.dns_secondary ?? '',
      remark: v.remark ?? '',
      route_push: !!v.route_push,
      enabled: editing ? editing.enabled : true,
    };
    setSaving(true);
    try {
      if (editing) {
        await net.updateStatic(editing.id, body);
      } else {
        await net.createStatic(body);
      }
      message.success(editing ? '已更新' : '已添加');
      setOpen(false);
      reload();
    } catch (err) {
      message.error(extractErr(err));
    } finally {
      setSaving(false);
    }
  };

  const onToggle = async (record: net.StaticLease) => {
    try {
      await net.toggleStatic(record.id, !record.enabled);
      message.success(record.enabled ? '已停用' : '已启用');
      reload();
    } catch (err) {
      message.error(extractErr(err));
    }
  };

  const onDelete = async (record: net.StaticLease) => {
    try {
      await net.deleteStatic(record.id);
      message.success('已删除');
      reload();
    } catch (err) {
      message.error(extractErr(err));
    }
  };

  const onBatch = async (action: net.BatchAction) => {
    try {
      await net.batchStatics(action, selected);
      message.success('操作成功');
      setSelected([]);
      reload();
    } catch (err) {
      message.error(extractErr(err));
    }
  };

  const columns: ColumnsType<net.StaticLease> = [
    { title: '主机名称', dataIndex: 'hostname', ellipsis: true, render: (v: string) => v || '-' },
    { title: '绑定IP', dataIndex: 'ip' },
    { title: '绑定MAC', dataIndex: 'mac' },
    { title: '网关', dataIndex: 'gateway', render: (v: string) => v || '-' },
    { title: '绑定接口', dataIndex: 'interface', render: (v: string) => v || '-' },
    { title: '首选DNS', dataIndex: 'dns_primary', render: (v: string) => v || '-' },
    { title: '备选DNS', dataIndex: 'dns_secondary', render: (v: string) => v || '-' },
    { title: '备注', dataIndex: 'remark', ellipsis: true, render: (v: string) => v || '-' },
    {
      title: '跟随路由推送',
      dataIndex: 'route_push',
      width: 110,
      render: (v: boolean) => (v ? <Tag color="blue">是</Tag> : <Tag>-</Tag>),
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      width: 90,
      render: (enabled: boolean) =>
        enabled ? <Tag color="success">已启用</Tag> : <Tag>已停用</Tag>,
    },
    {
      title: '操作',
      key: 'action',
      width: 170,
      fixed: 'right',
      render: (_, record) => (
        <Space size="middle">
          <Typography.Link onClick={() => openDrawer(record)}>编辑</Typography.Link>
          <Typography.Link onClick={() => onToggle(record)}>
            {record.enabled ? '停用' : '启用'}
          </Typography.Link>
          <Popconfirm title="确认删除？" onConfirm={() => onDelete(record)}>
            <Typography.Link type="danger">删除</Typography.Link>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <PageCard
      breadcrumb={['网络设置', 'DHCP设置', 'DHCP静态分配']}
      title="DHCP 静态分配"
      toolbar={
        <>
          <Space size="middle" wrap>
            <Checkbox checked={data.arp_bind} onChange={onToggleArpBind}>
              兼容ARP绑定到静态分配
            </Checkbox>
            <Input.Search
              allowClear
              placeholder="搜索 主机名/IP/MAC/备注"
              style={{ width: 240 }}
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
            />
          </Space>
          <Space>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openDrawer()}>
              添加
            </Button>
            <Button disabled={selected.length === 0} onClick={() => onBatch('enable')}>
              启用
            </Button>
            <Button disabled={selected.length === 0} onClick={() => onBatch('disable')}>
              停用
            </Button>
            <Popconfirm
              title="确认删除选中项？"
              disabled={selected.length === 0}
              onConfirm={() => onBatch('delete')}
            >
              <Button danger disabled={selected.length === 0}>
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
        scroll={{ x: 'max-content' }}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: (keys) => setSelected(keys as string[]),
        }}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
      />
      <Drawer
        title={editing ? '编辑静态分配' : '添加静态分配'}
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
          <Form.Item name="hostname" label="主机名称">
            <Input placeholder="可空，留作终端识别" allowClear />
          </Form.Item>
          <Form.Item name="ip" label="绑定IP" rules={[{ required: true, message: '请输入绑定IP' }]}>
            <Input placeholder="192.168.1.100" allowClear />
          </Form.Item>
          <Form.Item
            name="mac"
            label="绑定MAC"
            rules={[{ required: true, message: '请输入绑定MAC' }]}
          >
            <Input placeholder="AA:BB:CC:DD:EE:FF" allowClear />
          </Form.Item>
          <Form.Item name="gateway" label="网关">
            <Input placeholder="可空" allowClear />
          </Form.Item>
          <Form.Item name="interface" label="绑定接口">
            <Select allowClear placeholder="选择接口" options={ifaceOptions} />
          </Form.Item>
          <Form.Item name="dns_primary" label="首选DNS">
            <Input placeholder="可空" allowClear />
          </Form.Item>
          <Form.Item name="dns_secondary" label="备选DNS">
            <Input placeholder="可空" allowClear />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input placeholder="可空" allowClear />
          </Form.Item>
          <Form.Item
            name="route_push"
            label="跟随路由推送"
            valuePropName="checked"
            extra="仅当「DHCP 服务端」页的『路由下发』总开关为『仅指定设备』时生效：勾选后，已标记推送的静态路由只下发给这台设备。"
          >
            <Switch />
          </Form.Item>
        </Form>
      </Drawer>
    </PageCard>
  );
}
