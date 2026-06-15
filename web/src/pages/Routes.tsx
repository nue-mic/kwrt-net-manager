import { useState } from 'react';
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
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import PageCard from '../components/PageCard';
import { useNetData, extractErr } from '../hooks/useNetData';
import * as net from '../api/netcfg';

interface RouteForm {
  family: 'ipv4' | 'ipv6';
  interface: string;
  target: string;
  netmask: string;
  prefix: number;
  gateway: string;
  metric: number;
  remark: string;
}

const DEFAULTS: RouteForm = {
  family: 'ipv4',
  interface: 'auto',
  target: '',
  netmask: '',
  prefix: 24,
  gateway: '',
  metric: 1,
  remark: '',
};

export default function RoutesPage() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<net.Route[]>(() => net.listRoutes(), []);
  const { data: interfaces } = useNetData<net.NetInterface[]>(() => net.listInterfaces(), []);
  const [selected, setSelected] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<net.Route | null>(null);
  const [form] = Form.useForm<RouteForm>();
  const family = Form.useWatch('family', form);

  const openDrawer = (record?: net.Route) => {
    if (record) {
      setEditing(record);
      form.setFieldsValue({
        family: record.family,
        interface: record.interface || 'auto',
        target: record.target,
        netmask: record.netmask,
        prefix: record.prefix,
        gateway: record.gateway,
        metric: record.metric,
        remark: record.remark,
      });
    } else {
      setEditing(null);
      form.setFieldsValue(DEFAULTS);
    }
    setOpen(true);
  };

  const onSave = async () => {
    try {
      const v = await form.validateFields();
      const body: net.RouteInput = {
        family: v.family,
        interface: v.interface,
        target: v.target,
        gateway: v.gateway,
        metric: v.metric,
        remark: v.remark,
        enabled: editing ? editing.enabled : true,
        netmask: v.family === 'ipv4' ? v.netmask : '',
        prefix: v.family === 'ipv4' ? 0 : v.prefix,
      };
      if (editing) {
        await net.updateRoute(editing.id, body);
        message.success('已保存');
      } else {
        await net.createRoute(body);
        message.success('已添加');
      }
      setOpen(false);
      reload();
    } catch (e) {
      if (e && typeof e === 'object' && 'errorFields' in e) return;
      message.error(extractErr(e));
    }
  };

  const onToggle = async (record: net.Route) => {
    try {
      await net.toggleRoute(record.id, !record.enabled);
      message.success(record.enabled ? '已停用' : '已启用');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onDuplicate = async (record: net.Route) => {
    try {
      await net.duplicateRoute(record.id);
      message.success('已复制');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onDelete = async (record: net.Route) => {
    try {
      await net.deleteRoute(record.id);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onBatch = async (action: net.BatchAction) => {
    try {
      await net.batchRoutes(action, selected);
      message.success('操作成功');
      setSelected([]);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const columns: ColumnsType<net.Route> = [
    {
      title: '线路',
      dataIndex: 'interface',
      render: (v: string) => (v === 'auto' || v === '' ? '自动' : v),
    },
    { title: '目的地址', dataIndex: 'target' },
    {
      title: '子网掩码',
      render: (_: unknown, r: net.Route) =>
        r.family === 'ipv4' ? `${r.netmask} (/${r.prefix})` : `/${r.prefix}`,
    },
    { title: '网关', dataIndex: 'gateway', render: (v: string) => v || '-' },
    { title: '优先级', dataIndex: 'metric' },
    { title: '备注', dataIndex: 'remark', render: (v: string) => v || '-' },
    {
      title: '状态',
      dataIndex: 'enabled',
      render: (v: boolean) => (v ? <Tag color="success">已启用</Tag> : <Tag>已停用</Tag>),
    },
    {
      title: '操作',
      width: 220,
      render: (_: unknown, r: net.Route) => (
        <Space size="middle">
          <Typography.Link onClick={() => openDrawer(r)}>编辑</Typography.Link>
          <Typography.Link onClick={() => onDuplicate(r)}>复制</Typography.Link>
          <Typography.Link onClick={() => onToggle(r)}>{r.enabled ? '停用' : '启用'}</Typography.Link>
          <Popconfirm title="确认删除？" onConfirm={() => onDelete(r)}>
            <Typography.Link type="danger">删除</Typography.Link>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <PageCard
      breadcrumb={['网络设置', '静态路由', '静态路由']}
      title="静态路由"
      toolbar={
        <>
          <Space>
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
          <Button type="primary" icon={<PlusOutlined />} onClick={() => openDrawer()}>
            添加
          </Button>
        </>
      }
    >
      <Table
        rowKey="id"
        size="small"
        bordered
        loading={loading}
        dataSource={data}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: (k) => setSelected(k as string[]),
        }}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
      />

      <Drawer
        title={editing ? '编辑' : '添加'}
        width={520}
        open={open}
        onClose={() => setOpen(false)}
        extra={
          <Space>
            <Button onClick={() => setOpen(false)}>取消</Button>
            <Button type="primary" onClick={onSave}>
              保存
            </Button>
          </Space>
        }
      >
        <Form form={form} layout="vertical" initialValues={DEFAULTS}>
          <Form.Item label="协议栈" name="family" rules={[{ required: true }]}>
            <Select
              options={[
                { label: 'IPv4', value: 'ipv4' },
                { label: 'IPv6', value: 'ipv6' },
              ]}
            />
          </Form.Item>
          <Form.Item label="线路" name="interface" rules={[{ required: true }]}>
            <Select
              options={[
                { label: '自动', value: 'auto' },
                ...interfaces.map((i) => ({ label: i.name, value: i.name })),
              ]}
            />
          </Form.Item>
          <Form.Item label="目的地址" name="target" rules={[{ required: true, message: '请输入目的地址' }]}>
            <Input placeholder="如 192.168.10.0" />
          </Form.Item>
          {family === 'ipv6' ? (
            <Form.Item label="前缀长度" name="prefix" rules={[{ required: true }]}>
              <InputNumber min={0} max={128} style={{ width: '100%' }} />
            </Form.Item>
          ) : (
            <Form.Item label="子网掩码" name="netmask">
              <Input placeholder="255.255.255.0" />
            </Form.Item>
          )}
          <Form.Item label="网关" name="gateway">
            <Input placeholder="如 192.168.1.1" />
          </Form.Item>
          <Form.Item label="优先级" name="metric" extra="数值越小优先级越高">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item label="备注" name="remark">
            <Input />
          </Form.Item>
        </Form>
      </Drawer>
    </PageCard>
  );
}
