import { useState } from 'react';
import {
  Alert,
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

interface RouteForm {
  family: 'ipv4' | 'ipv6';
  interface: string;
  target: string;
  netmask: string;
  prefix: number;
  gateway: string;
  metric: number;
  remark: string;
  push_to_clients: boolean;
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
  push_to_clients: false,
};

export default function RoutesPage() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<net.Route[]>(() => net.listRoutes(), []);
  const { data: interfaces } = useNetData<net.NetInterface[]>(() => net.listInterfaces(), []);
  const { data: pushMode } = useNetData<net.RoutePushMode>(() => net.getRoutePushMode(), 'off');
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
        push_to_clients: record.push_to_clients,
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
        push_to_clients: v.family === 'ipv4' ? !!v.push_to_clients : false,
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
    {
      title: '下发客户端',
      dataIndex: 'push_to_clients',
      render: (_v: boolean, r: net.Route) => {
        if (r.family !== 'ipv4' || !r.push_to_clients) return <Tag>-</Tag>;
        if (pushMode === 'all') return <Tag color="blue">已推送·全部</Tag>;
        if (pushMode === 'tagged') return <Tag color="cyan">已推送·指定</Tag>;
        return <Tag color="warning">待开启</Tag>; // 已勾选但总开关关闭
      },
    },
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
      {pushMode === 'off' ? (
        data.some((r) => r.family === 'ipv4' && r.push_to_clients) && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            message="有路由已勾选「下发给客户端」，但 DHCP 服务端的「路由下发」总开关当前为「关闭」，需到 DHCP 服务端页把它改成「全部客户端」或「仅指定设备」后才会真正下发。"
          />
        )
      ) : (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message={
            pushMode === 'all'
              ? '路由下发：全部客户端 —— 已勾选的路由经 DHCP option 121 下发给本 LAN 所有客户端（设备续租/重连后生效）。'
              : '路由下发：仅指定设备 —— 已勾选的路由只下发给「DHCP 静态分配」中开启了『跟随路由推送』的设备。'
          }
        />
      )}
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
        width="min(92vw, 640px)"
        open={open}
        onClose={() => setOpen(false)}
        destroyOnClose
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
          {family !== 'ipv6' && (
            <Form.Item
              label="下发给客户端 (DHCP 选项 121)"
              name="push_to_clients"
              valuePropName="checked"
              extra="勾选后，本路由会经 DHCP 推送给客户端，让网关指向主路由的设备也把该网段流量引到本旁路由。需在「DHCP 服务端」页把『路由下发』总开关设为 全部客户端 / 仅指定设备 才生效。"
            >
              <Switch />
            </Form.Item>
          )}
        </Form>
      </Drawer>
    </PageCard>
  );
}
