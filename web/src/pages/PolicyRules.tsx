import { useMemo, useState } from 'react';
import { Alert, App, Button, Drawer, Form, Input, InputNumber, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import PageCard from '../components/PageCard';
import { useNetData, extractErr } from '../hooks/useNetData';
import type { BatchAction } from '../api/netcfg';
import * as net from '../api/netcfg';

interface RuleForm {
  family: 'ipv4' | 'ipv6';
  priority?: number;
  src: string;
  dest: string;
  in_iface: string;
  lookup: string;
  remark: string;
}

export default function PolicyRulesPage() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<net.PolicyRule[]>(() => net.listPolicyRules(), []);
  const { data: ifaces } = useNetData<net.NetInterface[]>(() => net.listInterfaces(), []);
  const [selected, setSelected] = useState<string[]>([]);
  const [keyword, setKeyword] = useState('');
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<net.PolicyRule | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<RuleForm>();

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    if (!kw) return data;
    return data.filter((r) => [r.src, r.dest, r.lookup, r.in_iface, r.remark].some((f) => (f ?? '').toLowerCase().includes(kw)));
  }, [data, keyword]);

  const openDrawer = (record?: net.PolicyRule) => {
    setEditing(record ?? null);
    if (record) {
      form.setFieldsValue({
        family: record.family,
        priority: record.priority || undefined,
        src: record.src,
        dest: record.dest,
        in_iface: record.in_iface,
        lookup: record.lookup,
        remark: record.remark,
      });
    } else {
      form.resetFields();
      form.setFieldsValue({ family: 'ipv4', src: '', dest: '', in_iface: '', lookup: '', remark: '' });
    }
    setOpen(true);
  };

  const onSave = async () => {
    let v: RuleForm;
    try {
      v = await form.validateFields();
    } catch {
      return;
    }
    const body: net.PolicyRuleInput = {
      family: v.family,
      enabled: editing ? editing.enabled : true,
      priority: v.priority ?? 0,
      src: v.src ?? '',
      dest: v.dest ?? '',
      in_iface: v.in_iface ?? '',
      lookup: (v.lookup ?? '').trim(),
      remark: v.remark ?? '',
    };
    setSaving(true);
    try {
      if (editing) await net.updatePolicyRule(editing.id, body);
      else await net.createPolicyRule(body);
      message.success(editing ? '已更新' : '已添加');
      setOpen(false);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setSaving(false);
    }
  };

  const onToggle = async (r: net.PolicyRule) => {
    try {
      await net.togglePolicyRule(r.id, !r.enabled);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };
  const onDelete = async (r: net.PolicyRule) => {
    try {
      await net.deletePolicyRule(r.id);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };
  const onBatch = async (action: BatchAction) => {
    try {
      await net.batchPolicyRules(action, selected);
      setSelected([]);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const columns: ColumnsType<net.PolicyRule> = [
    { title: '协议', dataIndex: 'family', width: 80, render: (v: string) => <Tag>{v === 'ipv6' ? 'IPv6' : 'IPv4'}</Tag> },
    { title: '源地址', dataIndex: 'src', render: (v: string) => v || <Typography.Text type="secondary">任意</Typography.Text> },
    { title: '目标地址', dataIndex: 'dest', render: (v: string) => v || <Typography.Text type="secondary">任意</Typography.Text> },
    { title: '入接口', dataIndex: 'in_iface', width: 100, render: (v: string) => v || '-' },
    { title: '查询表', dataIndex: 'lookup', width: 90, render: (v: string) => <Tag color="purple">表 {v}</Tag> },
    { title: '优先级', dataIndex: 'priority', width: 90, render: (v: number) => v || <Typography.Text type="secondary">自动</Typography.Text> },
    { title: '备注', dataIndex: 'remark', ellipsis: true, render: (v: string) => v || '-' },
    { title: '状态', dataIndex: 'enabled', width: 90, render: (v: boolean) => (v ? <Tag color="success">已启用</Tag> : <Tag>已停用</Tag>) },
    {
      title: '操作',
      width: 150,
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

  return (
    <PageCard
      breadcrumb={['网络设置', '静态路由', '策略路由']}
      title="策略路由"
      toolbar={
        <>
          <Space>
            <Input.Search allowClear placeholder="搜索 源/目标/表/备注" style={{ width: 240 }} value={keyword} onChange={(e) => setKeyword(e.target.value)} />
          </Space>
          <Space>
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
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="策略路由 = 按「源地址 / 入接口」把流量导向指定路由表，实现多线路分流。用法两步："
        description={
          <>
            ① 在「静态路由」页建一条路由、填上「路由表」号（如 100）、网关指向目标线路；
            ② 在这里建一条规则：匹配源地址（如 192.168.1.100），查询表填同一个表号（100）。
            这样该源的流量就走那条线路。
          </>
        }
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
        title={editing ? '编辑策略路由' : '添加策略路由'}
        width="min(92vw, 600px)"
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
          <Form.Item label="协议栈" name="family" rules={[{ required: true }]}>
            <Select
              options={[
                { label: 'IPv4', value: 'ipv4' },
                { label: 'IPv6', value: 'ipv6' },
              ]}
            />
          </Form.Item>
          <Form.Item label="源地址 / 网段（可选）" name="src" extra="匹配「来自」这些地址的流量，如 192.168.1.100 或 192.168.1.0/24。留空=任意源。">
            <Input placeholder="如 192.168.1.0/24" allowClear />
          </Form.Item>
          <Form.Item label="目标地址 / 网段（可选）" name="dest" extra="只对发往这些目标的流量生效。留空=任意目标。">
            <Input placeholder="如 8.8.8.8/32" allowClear />
          </Form.Item>
          <Form.Item label="入接口（可选）" name="in_iface">
            <Select allowClear placeholder="任意" options={ifaces.map((i) => ({ label: i.name, value: i.name }))} />
          </Form.Item>
          <Form.Item label="查询路由表号" name="lookup" rules={[{ required: true, message: '请填路由表号' }]} extra="匹配的流量去查这个表（1-255）。需先在「静态路由」里建一条带同号「路由表」的路由。">
            <Input placeholder="如 100" allowClear />
          </Form.Item>
          <Form.Item label="优先级（可选）" name="priority" extra="数值越小越先匹配；留空由内核分配。">
            <InputNumber min={0} max={65535} style={{ width: '100%' }} placeholder="留空=自动" />
          </Form.Item>
          <Form.Item label="备注" name="remark">
            <Input placeholder="可空" allowClear />
          </Form.Item>
        </Form>
      </Drawer>
    </PageCard>
  );
}
