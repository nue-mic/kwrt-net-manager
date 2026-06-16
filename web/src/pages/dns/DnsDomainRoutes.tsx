import { useMemo, useState } from 'react';
import { Alert, App, Button, Drawer, Form, Input, Popconfirm, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import PageCard from '../../components/PageCard';
import { useNetData, extractErr } from '../../hooks/useNetData';
import type { BatchAction } from '../../api/netcfg';
import * as dns from '../../api/dns';

interface RouteForm {
  domain: string;
  server: string;
  out_iface: string;
  remark: string;
}

export default function DnsDomainRoutesPage() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<dns.DNSDomainRoute[]>(() => dns.listDNSDomainRoutes(), []);
  const [selected, setSelected] = useState<string[]>([]);
  const [keyword, setKeyword] = useState('');
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<dns.DNSDomainRoute | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<RouteForm>();

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    if (!kw) return data;
    return data.filter((r) => [r.domain, r.server, r.remark].some((f) => (f ?? '').toLowerCase().includes(kw)));
  }, [data, keyword]);

  const openDrawer = (record?: dns.DNSDomainRoute) => {
    setEditing(record ?? null);
    if (record) form.setFieldsValue({ domain: record.domain, server: record.server, out_iface: record.out_iface, remark: record.remark });
    else form.resetFields();
    setOpen(true);
  };

  const onSave = async () => {
    let v: RouteForm;
    try {
      v = await form.validateFields();
    } catch {
      return;
    }
    const body: dns.DNSDomainRouteInput = {
      domain: v.domain,
      server: v.server,
      out_iface: v.out_iface ?? '',
      remark: v.remark ?? '',
      enabled: editing ? editing.enabled : true,
    };
    setSaving(true);
    try {
      if (editing) await dns.updateDNSDomainRoute(editing.id, body);
      else await dns.createDNSDomainRoute(body);
      message.success(editing ? '已更新' : '已添加');
      setOpen(false);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setSaving(false);
    }
  };

  const onToggle = async (r: dns.DNSDomainRoute) => {
    try {
      await dns.toggleDNSDomainRoute(r.id, !r.enabled);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };
  const onDelete = async (r: dns.DNSDomainRoute) => {
    try {
      await dns.deleteDNSDomainRoute(r.id);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };
  const onBatch = async (action: BatchAction) => {
    try {
      await dns.batchDNSDomainRoutes(action, selected);
      setSelected([]);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const columns: ColumnsType<dns.DNSDomainRoute> = [
    { title: '域名', dataIndex: 'domain' },
    { title: '上游 DNS', dataIndex: 'server' },
    { title: '出接口', dataIndex: 'out_iface', render: (v: string) => v || '-' },
    { title: '备注', dataIndex: 'remark', ellipsis: true, render: (v: string) => v || '-' },
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

  return (
    <PageCard
      breadcrumb={['网络设置', 'DNS 设置', '域名分流 DNS']}
      title="域名分流 DNS"
      toolbar={
        <>
          <Space>
            <Input.Search allowClear placeholder="搜索 域名/上游/备注" style={{ width: 240 }} value={keyword} onChange={(e) => setKeyword(e.target.value)} />
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
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        message="指定域名走指定上游 DNS（dnsmasq server=/域名/上游）。OpenWrt 单 dnsmasq 无法按 WAN 线路自动分流，故此处为「按域名分流」——爱快「多线路DNS」的等效可行实现。"
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
        title={editing ? '编辑域名分流' : '添加域名分流'}
        width="min(92vw, 580px)"
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
          <Form.Item name="domain" label="域名" rules={[{ required: true, message: '请输入域名' }]}>
            <Input placeholder="如 example.com" allowClear />
          </Form.Item>
          <Form.Item name="server" label="上游 DNS" rules={[{ required: true, message: '请输入上游 DNS' }]} extra="可带端口，如 8.8.8.8 或 8.8.8.8#5353。">
            <Input placeholder="如 8.8.8.8" allowClear />
          </Form.Item>
          <Form.Item name="out_iface" label="出接口（可选）" extra="强制该域解析从指定接口发出（@iface）。一般留空。">
            <Input placeholder="可空，如 wan" allowClear />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input placeholder="可空" allowClear />
          </Form.Item>
        </Form>
      </Drawer>
    </PageCard>
  );
}
