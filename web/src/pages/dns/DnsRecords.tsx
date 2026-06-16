import { useMemo, useState } from 'react';
import { App, Button, Drawer, Form, Input, Popconfirm, Space, Switch, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import PageCard from '../../components/PageCard';
import { useNetData, extractErr } from '../../hooks/useNetData';
import type { BatchAction } from '../../api/netcfg';
import * as dns from '../../api/dns';

interface RecordForm {
  domain: string;
  address: string;
  wildcard: boolean;
  remark: string;
}

export default function DnsRecordsPage() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<dns.DNSRecord[]>(() => dns.listDNSRecords(), []);
  const [selected, setSelected] = useState<string[]>([]);
  const [keyword, setKeyword] = useState('');
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<dns.DNSRecord | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<RecordForm>();

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    if (!kw) return data;
    return data.filter((r) => [r.domain, r.address, r.remark].some((f) => (f ?? '').toLowerCase().includes(kw)));
  }, [data, keyword]);

  const openDrawer = (record?: dns.DNSRecord) => {
    setEditing(record ?? null);
    if (record) {
      form.setFieldsValue({ domain: record.domain, address: record.address, wildcard: record.wildcard, remark: record.remark });
    } else {
      form.resetFields();
      form.setFieldsValue({ wildcard: false });
    }
    setOpen(true);
  };

  const onSave = async () => {
    let v: RecordForm;
    try {
      v = await form.validateFields();
    } catch {
      return;
    }
    const body: dns.DNSRecordInput = {
      domain: v.domain,
      address: v.address,
      wildcard: !!v.wildcard || v.domain.startsWith('*.'),
      record_type: 'A', // 后端按地址族自动判定
      src_ip_scope: '',
      remark: v.remark ?? '',
      enabled: editing ? editing.enabled : true,
    };
    setSaving(true);
    try {
      if (editing) await dns.updateDNSRecord(editing.id, body);
      else await dns.createDNSRecord(body);
      message.success(editing ? '已更新' : '已添加');
      setOpen(false);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setSaving(false);
    }
  };

  const onToggle = async (r: dns.DNSRecord) => {
    try {
      await dns.toggleDNSRecord(r.id, !r.enabled);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };
  const onDelete = async (r: dns.DNSRecord) => {
    try {
      await dns.deleteDNSRecord(r.id);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };
  const onBatch = async (action: BatchAction) => {
    try {
      await dns.batchDNSRecords(action, selected);
      setSelected([]);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const columns: ColumnsType<dns.DNSRecord> = [
    { title: '域名', dataIndex: 'domain' },
    { title: '解析类型', dataIndex: 'record_type', width: 100, render: (v: string) => <Tag>{v}</Tag> },
    { title: '解析地址', dataIndex: 'address' },
    { title: '通配', dataIndex: 'wildcard', width: 80, render: (v: boolean) => (v ? <Tag color="blue">是</Tag> : <Tag>-</Tag>) },
    { title: '备注', dataIndex: 'remark', ellipsis: true, render: (v: string) => v || '-' },
    {
      title: '状态',
      dataIndex: 'enabled',
      width: 90,
      render: (v: boolean) => (v ? <Tag color="success">已启用</Tag> : <Tag>已停用</Tag>),
    },
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

  return (
    <PageCard
      breadcrumb={['网络设置', 'DNS 设置', '自定义解析']}
      title="自定义解析（DNS 反向代理）"
      toolbar={
        <>
          <Space>
            <Input.Search allowClear placeholder="搜索 域名/IP/备注" style={{ width: 240 }} value={keyword} onChange={(e) => setKeyword(e.target.value)} />
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
            <Popconfirm title="确认删除选中项？" disabled={selected.length === 0} onConfirm={() => onBatch('delete')}>
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
        rowSelection={{ selectedRowKeys: selected, onChange: (k) => setSelected(k as string[]) }}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
      />
      <Drawer
        title={editing ? '编辑自定义解析' : '添加自定义解析'}
        width={480}
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
          <Form.Item name="domain" label="域名" rules={[{ required: true, message: '请输入域名' }]} extra="以 *. 开头表示通配（含所有子域）。">
            <Input placeholder="如 nas.lan 或 *.demo.lan" allowClear />
          </Form.Item>
          <Form.Item name="address" label="解析地址" rules={[{ required: true, message: '请输入解析地址' }]} extra="IPv4 → A 记录；IPv6 → AAAA 记录（自动判定）。">
            <Input placeholder="如 192.168.1.50" allowClear />
          </Form.Item>
          <Form.Item name="wildcard" label="通配（含子域）" valuePropName="checked" extra="勾选或域名以 *. 开头均视为通配。">
            <Switch />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input placeholder="可空" allowClear />
          </Form.Item>
          <Typography.Text type="secondary">
            说明：OpenWrt 单 dnsmasq 无法按「来源 IP 段」分应答，故爱快的「作用IP段」此处全局生效。
          </Typography.Text>
        </Form>
      </Drawer>
    </PageCard>
  );
}
