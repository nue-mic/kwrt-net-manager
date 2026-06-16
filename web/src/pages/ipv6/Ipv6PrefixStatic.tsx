import { useEffect, useState } from 'react';
import {
  Alert, App, Button, Drawer, Form, Input, Popconfirm, Space, Switch, Table, Tag, Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import PageCard from '../../components/PageCard';
import { useNetData, extractErr } from '../../hooks/useNetData';
import * as ipv6 from '../../api/ipv6';

const { Text } = Typography;

export default function Ipv6PrefixStatic() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData(() => ipv6.listPrefixStaticsV6(), [] as ipv6.PrefixStaticV6[]);

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<ipv6.PrefixStaticV6 | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<ipv6.PrefixStaticV6Input>();

  useEffect(() => {
    if (!open) return;
    if (editing) {
      form.setFieldsValue({
        local_link: editing.local_link,
        lan_interface: editing.lan_interface,
        wan_line: editing.wan_line,
        duid: editing.duid,
        host_id: editing.host_id,
        mac: editing.mac,
        remark: editing.remark,
        enabled: editing.enabled,
      });
    } else {
      form.setFieldsValue({
        local_link: '',
        lan_interface: '',
        wan_line: '',
        duid: '',
        host_id: '',
        mac: '',
        remark: '',
        enabled: true,
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, editing]);

  const openDrawer = (record?: ipv6.PrefixStaticV6) => {
    setEditing(record ?? null);
    setOpen(true);
  };

  const onSave = async () => {
    let v: ipv6.PrefixStaticV6Input;
    try {
      v = await form.validateFields();
    } catch {
      return;
    }
    const body: ipv6.PrefixStaticV6Input = {
      local_link: v.local_link?.trim() ?? '',
      lan_interface: v.lan_interface?.trim() ?? '',
      wan_line: v.wan_line?.trim() ?? '',
      duid: v.duid?.trim() ?? '',
      host_id: v.host_id?.trim() ?? '',
      mac: v.mac?.trim() ?? '',
      remark: v.remark?.trim() ?? '',
      enabled: !!v.enabled,
    };
    setSaving(true);
    try {
      if (editing) {
        await ipv6.updatePrefixStaticV6(editing.id, body);
        message.success('已保存');
      } else {
        await ipv6.createPrefixStaticV6(body);
        message.success('已添加');
      }
      setOpen(false);
      reload();
    } catch (e) {
      message.error('保存失败：' + extractErr(e));
    } finally {
      setSaving(false);
    }
  };

  const onToggle = async (record: ipv6.PrefixStaticV6) => {
    try {
      await ipv6.togglePrefixStaticV6(record.id, !record.enabled);
      message.success(record.enabled ? '已停用' : '已启用');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onDelete = async (id: string) => {
    try {
      await ipv6.deletePrefixStaticV6(id);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const columns: ColumnsType<ipv6.PrefixStaticV6> = [
    { title: '终端本地链接IPv6地址', dataIndex: 'local_link', key: 'local_link', render: (v: string) => v || '—' },
    { title: '内网接口', dataIndex: 'lan_interface', key: 'lan_interface', render: (v: string) => v || '—' },
    { title: '外网线路', dataIndex: 'wan_line', key: 'wan_line', render: (v: string) => v || '—' },
    {
      title: 'DUID',
      dataIndex: 'duid',
      key: 'duid',
      render: (v: string) => (v ? <Text copyable style={{ fontSize: 12 }}>{v}</Text> : '—'),
    },
    { title: '接口ID', dataIndex: 'host_id', key: 'host_id', render: (v: string) => v || '—' },
    { title: 'MAC', dataIndex: 'mac', key: 'mac', render: (v: string) => v || '—' },
    {
      title: '备注',
      dataIndex: 'remark',
      key: 'remark',
      render: (v: string) => v || <Text type="secondary">-</Text>,
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 110,
      render: (enabled: boolean) => (enabled ? <Tag color="success">已启用</Tag> : <Tag>已停用</Tag>),
    },
    {
      title: '操作',
      key: 'action',
      width: 200,
      fixed: 'right',
      render: (_, record) => (
        <Space size="middle">
          <Typography.Link onClick={() => openDrawer(record)}>编辑</Typography.Link>
          <Typography.Link onClick={() => onToggle(record)}>{record.enabled ? '停用' : '启用'}</Typography.Link>
          <Popconfirm title="确认删除？" onConfirm={() => onDelete(record.id)}>
            <Typography.Link type="danger">删除</Typography.Link>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <PageCard
      breadcrumb={['网络设置', 'IPv6', '前缀静态分配']}
      title="前缀静态分配"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => openDrawer()}>
          添加
        </Button>
      }
    >
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="仅固定终端的接口 ID（IID），非整段委派前缀；需先在「IPv6 内网设置」开启 DHCPv6 服务端方可生效。"
      />
      <Table
        rowKey="id"
        size="small"
        bordered
        loading={loading}
        dataSource={data}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
        scroll={{ x: 'max-content' }}
        locale={{ emptyText: '暂无前缀静态分配' }}
      />
      <Drawer
        title={editing ? '编辑前缀静态分配' : '添加前缀静态分配'}
        width="min(92vw, 600px)"
        open={open}
        onClose={() => setOpen(false)}
        destroyOnClose
        footer={
          <Space>
            <Button type="primary" loading={saving} onClick={onSave}>
              保存
            </Button>
            <Button onClick={() => setOpen(false)}>取消</Button>
          </Space>
        }
      >
        <Form form={form} layout="vertical">
          <Form.Item
            label="终端本地链接IPv6地址"
            name="local_link"
            tooltip="终端的 fe80:: 本地链接地址"
          >
            <Input placeholder="fe80::1234" />
          </Form.Item>
          <Form.Item
            label="内网接口"
            name="lan_interface"
            rules={[{ required: true, message: '请输入内网接口' }]}
          >
            <Input placeholder="lan" />
          </Form.Item>
          <Form.Item label="外网线路" name="wan_line">
            <Input placeholder="wan6" />
          </Form.Item>
          <Form.Item
            label="DUID"
            name="duid"
            tooltip="DUID 与 MAC 二选一：填 DUID 时按 DUID 匹配终端"
          >
            <Input placeholder="00:04:..." />
          </Form.Item>
          <Form.Item
            label="接口ID"
            name="host_id"
            rules={[{ required: true, message: '请输入接口 ID（IID）' }]}
            tooltip="固定分配给该终端的接口 ID（IID）"
          >
            <Input placeholder="::1234" />
          </Form.Item>
          <Form.Item label="MAC（选填）" name="mac" tooltip="DUID 与 MAC 二选一：未填 DUID 时按 MAC 匹配终端">
            <Input placeholder="AA:BB:CC:DD:EE:FF" />
          </Form.Item>
          <Form.Item label="备注" name="remark">
            <Input placeholder="可选" />
          </Form.Item>
          <Form.Item label="启用" name="enabled" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Drawer>
    </PageCard>
  );
}
