import { useMemo, useState } from 'react';
import { App, AutoComplete, Button, Drawer, Form, Input, Modal, Popconfirm, Radio, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import PageCard from '../components/PageCard';
import { useNetData, extractErr } from '../hooks/useNetData';
import * as net from '../api/netcfg';

interface AclForm {
  mac: string;
  remark: string;
}

export default function DhcpAclPage() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<net.ACL>(() => net.getACL(), { mode: 'blacklist', entries: [] });
  const { data: leases } = useNetData<net.Lease[]>(() => net.listLeases(), []);
  const [selected, setSelected] = useState<string[]>([]);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<net.ACLEntry | null>(null);
  const [form] = Form.useForm<AclForm>();

  // 在线终端 → MAC 选择器（保留手输任意 MAC）。
  const deviceOptions = useMemo(
    () =>
      leases.map((l) => ({
        value: l.mac,
        label: `${l.hostname || '未知设备'}${l.vendor ? `（${l.vendor}）` : ''} · ${l.ip} · ${l.mac}`,
      })),
    [leases],
  );
  // 选中终端：备注为空时自动带主机名。
  const onPickMac = (mac: string) => {
    const l = leases.find((x) => x.mac === mac);
    if (l?.hostname && !form.getFieldValue('remark')) form.setFieldsValue({ remark: l.hostname });
  };

  const openDrawer = (record?: net.ACLEntry) => {
    setEditing(record ?? null);
    if (record) {
      form.setFieldsValue({ mac: record.mac, remark: record.remark });
    } else {
      form.resetFields();
    }
    setOpen(true);
  };

  const onSave = async () => {
    try {
      const v = await form.validateFields();
      const body: net.ACLEntryInput = {
        mac: v.mac.trim(),
        remark: v.remark?.trim() ?? '',
        enabled: editing ? editing.enabled : true,
      };
      if (editing) {
        await net.updateACLEntry(editing.id, body);
        message.success('已保存');
      } else {
        await net.addACLEntry(body);
        message.success('已添加');
      }
      setOpen(false);
      reload();
    } catch (e) {
      if (e && typeof e === 'object' && 'errorFields' in e) return;
      message.error(extractErr(e));
    }
  };

  const applyMode = async (mode: net.ACL['mode']) => {
    try {
      await net.setACLMode(mode);
      message.success(mode === 'blacklist' ? '已切换为黑名单' : '已切换为白名单');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onChangeMode = (mode: net.ACL['mode']) => {
    // 切到白名单且名单内无启用项 = 所有设备都将拿不到 DHCP，先确认，避免误锁全网。
    if (mode === 'whitelist' && data.entries.filter((e) => e.enabled).length === 0) {
      Modal.confirm({
        title: '切换为白名单？',
        content: '白名单模式下「仅名单内 MAC 可获取 DHCP」。当前名单为空，切换后所有设备都将无法获取地址。确认继续？',
        okText: '确认切换',
        cancelText: '取消',
        okButtonProps: { danger: true },
        onOk: () => applyMode(mode),
      });
      return;
    }
    void applyMode(mode);
  };

  const onToggle = async (record: net.ACLEntry) => {
    try {
      await net.toggleACLEntry(record.id);
      message.success(record.enabled ? '已停用' : '已启用');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onDelete = async (id: string) => {
    try {
      await net.deleteACLEntry(id);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onBatchDelete = async () => {
    try {
      for (const id of selected) {
        await net.deleteACLEntry(id);
      }
      message.success('已删除所选');
      setSelected([]);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const columns: ColumnsType<net.ACLEntry> = [
    { title: 'MAC', dataIndex: 'mac', key: 'mac', width: 220 },
    {
      title: '备注',
      dataIndex: 'remark',
      key: 'remark',
      render: (v: string) => v || <Typography.Text type="secondary">-</Typography.Text>,
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 120,
      render: (enabled: boolean) =>
        enabled ? <Tag color="success">已启用</Tag> : <Tag>已停用</Tag>,
    },
    {
      title: '操作',
      key: 'action',
      width: 200,
      render: (_, record) => (
        <Space size="middle">
          <Typography.Link onClick={() => openDrawer(record)}>编辑</Typography.Link>
          <Typography.Link onClick={() => onToggle(record)}>
            {record.enabled ? '停用' : '启用'}
          </Typography.Link>
          <Popconfirm title="确认删除？" onConfirm={() => onDelete(record.id)}>
            <Typography.Link type="danger">删除</Typography.Link>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const modeHint =
    data.mode === 'blacklist'
      ? '黑名单：名单内的 MAC 禁止获取 DHCP'
      : '白名单：仅名单内的 MAC 可获取 DHCP';

  return (
    <PageCard
      breadcrumb={['网络设置', 'DHCP设置', 'DHCP黑白名单']}
      title="DHCP 黑白名单"
      toolbar={
        <>
          <Space size="middle">
            <Radio.Group
              value={data.mode}
              optionType="button"
              buttonStyle="solid"
              onChange={(e) => onChangeMode(e.target.value as net.ACL['mode'])}
              options={[
                { label: '黑名单', value: 'blacklist' },
                { label: '白名单', value: 'whitelist' },
              ]}
            />
            <Typography.Text type="secondary">{modeHint}</Typography.Text>
          </Space>
          <Space>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openDrawer()}>
              添加
            </Button>
            <Popconfirm
              title={`确认删除所选 ${selected.length} 项？`}
              disabled={selected.length === 0}
              onConfirm={onBatchDelete}
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
        dataSource={data.entries}
        rowSelection={{ selectedRowKeys: selected, onChange: (k) => setSelected(k as string[]) }}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
      />
      <Drawer
        title={editing ? '编辑' : '添加'}
        width="min(92vw, 560px)"
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
        <Form form={form} layout="vertical">
          <Form.Item
            label="MAC"
            name="mac"
            extra="可从在线终端下拉选，或手输任意 MAC。"
            rules={[
              { required: true, message: '请输入 MAC 地址' },
              { pattern: /^([0-9a-fA-F]{2}[:-]){5}[0-9a-fA-F]{2}$/, message: 'MAC 格式不正确，如 AA:BB:CC:DD:EE:FF' },
            ]}
          >
            <AutoComplete
              options={deviceOptions}
              placeholder="选择在线终端或手输 MAC"
              onSelect={onPickMac}
              filterOption={(input, option) => String(option?.value ?? '').toLowerCase().includes(input.toLowerCase())}
              allowClear
            />
          </Form.Item>
          <Form.Item label="备注" name="remark">
            <Input placeholder="可选" />
          </Form.Item>
        </Form>
      </Drawer>
    </PageCard>
  );
}
