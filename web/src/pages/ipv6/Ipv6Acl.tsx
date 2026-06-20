import { useMemo, useState } from 'react';
import { Alert, App, AutoComplete, Button, Drawer, Form, Input, Popconfirm, Radio, Space, Switch, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined } from '@ant-design/icons';
import PageCard from '../../components/PageCard';
import { useNetData, extractErr } from '../../hooks/useNetData';
import * as ipv6 from '../../api/ipv6';

const { Text } = Typography;

const METHOD_LABEL: Record<ipv6.ACLv6Method, string> = {
  duid: '按DUID拒发(可靠)',
  l2mac: '按MAC L2拦截(暂未实现)',
};

interface Aclv6Form {
  method: ipv6.ACLv6Method;
  duid: string;
  mac: string;
  remark: string;
  enabled: boolean;
}

export default function Ipv6Acl() {
  const { message } = App.useApp();
  const { data, loading, reload } = useNetData<ipv6.ACLv6>(
    () => ipv6.getACLv6(),
    { mode: 'blacklist', entries: [] } as ipv6.ACLv6,
  );
  const { data: leasesV6 } = useNetData<ipv6.LeaseV6[]>(() => ipv6.listLeasesV6(), []);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<ipv6.ACLv6Entry | null>(null);
  const [form] = Form.useForm<Aclv6Form>();
  const watchMethod = Form.useWatch('method', form) as ipv6.ACLv6Method | undefined;

  // DHCPv6 终端 → DUID 选择器（仅有 DUID 的条目；保留手输）。
  const duidOptions = useMemo(
    () =>
      leasesV6
        .filter((l) => l.duid)
        .map((l) => ({
          value: l.duid,
          label: `${l.hostname || '未知设备'}${l.vendor ? `（${l.vendor}）` : ''} · ${l.duid}`,
        })),
    [leasesV6],
  );
  // 选中终端：备注为空时自动带主机名。
  const onPickDuid = (duid: string) => {
    const l = leasesV6.find((x) => x.duid === duid);
    if (l?.hostname && !form.getFieldValue('remark')) form.setFieldsValue({ remark: l.hostname });
  };

  const openDrawer = (record?: ipv6.ACLv6Entry) => {
    setEditing(record ?? null);
    if (record) {
      form.setFieldsValue({
        method: record.method,
        duid: record.duid,
        mac: record.mac,
        remark: record.remark,
        enabled: record.enabled,
      });
    } else {
      form.setFieldsValue({ method: 'duid', duid: '', mac: '', remark: '', enabled: true });
    }
    setOpen(true);
  };

  const onSave = async () => {
    let v: Aclv6Form;
    try {
      v = await form.validateFields();
    } catch {
      return;
    }
    const body: ipv6.ACLv6EntryInput = {
      method: v.method,
      duid: v.method === 'duid' ? v.duid.trim() : '',
      mac: v.method === 'l2mac' ? v.mac.trim() : '',
      remark: v.remark?.trim() ?? '',
      enabled: v.enabled,
    };
    try {
      if (editing) {
        await ipv6.updateACLv6Entry(editing.id, body);
        message.success('已保存');
      } else {
        await ipv6.addACLv6Entry(body);
        message.success('已添加');
      }
      setOpen(false);
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onChangeMode = async (mode: ipv6.ACLv6['mode']) => {
    try {
      await ipv6.setACLv6Mode(mode);
      message.success(mode === 'blacklist' ? '已切换为黑名单' : '已切换为白名单');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onToggle = async (record: ipv6.ACLv6Entry) => {
    try {
      await ipv6.toggleACLv6Entry(record.id);
      message.success(record.enabled ? '已停用' : '已启用');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const onDelete = async (id: string) => {
    try {
      await ipv6.deleteACLv6Entry(id);
      message.success('已删除');
      reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  const columns: ColumnsType<ipv6.ACLv6Entry> = [
    {
      title: '底层方式',
      dataIndex: 'method',
      key: 'method',
      width: 200,
      render: (m: ipv6.ACLv6Method) =>
        m === 'duid' ? <Tag color="green">{METHOD_LABEL.duid}</Tag> : <Tag color="orange">{METHOD_LABEL.l2mac}</Tag>,
    },
    {
      title: 'MAC',
      dataIndex: 'mac',
      key: 'mac',
      width: 200,
      render: (v: string) => v || <Text type="secondary">-</Text>,
    },
    {
      title: 'DUID',
      dataIndex: 'duid',
      key: 'duid',
      ellipsis: true,
      render: (v: string) => v || <Text type="secondary">-</Text>,
    },
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

  const modeHint =
    data.mode === 'blacklist'
      ? '黑名单：名单内的终端禁止获取 DHCPv6（拒发）'
      : '白名单：仅名单内的终端可获取 DHCPv6';

  return (
    <PageCard
      breadcrumb={['网络设置', 'IPv6', 'DHCPv6黑白名单']}
      title="DHCPv6 接入控制（实验）"
      toolbar={
        <>
          <Space size="middle">
            <Radio.Group
              value={data.mode}
              optionType="button"
              buttonStyle="solid"
              onChange={(e) => onChangeMode(e.target.value as ipv6.ACLv6['mode'])}
              options={[
                { label: '使用黑名单模式', value: 'blacklist' },
                { label: '使用白名单模式', value: 'whitelist' },
              ]}
            />
            <Text type="secondary">{modeHint}</Text>
          </Space>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => openDrawer()}>
            添加
          </Button>
        </>
      }
    >
      <Alert
        type="warning"
        showIcon
        style={{ marginBottom: 16 }}
        message="odhcpd 原生不支持按 MAC 黑白名单"
        description="可靠方式为「按 DUID 拒发」；「按 MAC L2 拦截」为实验项，SLAAC(RA) 可绕过、MAC 可伪造，强隔离请用交换机端口安全。"
      />
      <Table
        rowKey="id"
        size="small"
        bordered
        loading={loading}
        dataSource={data.entries}
        columns={columns}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
        scroll={{ x: 'max-content' }}
        locale={{ emptyText: '暂无接入控制条目' }}
      />
      <Drawer
        title={editing ? '编辑接入控制条目' : '添加接入控制条目'}
        width="min(92vw, 600px)"
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
          <Form.Item label="底层方式" name="method">
            <Radio.Group optionType="button" buttonStyle="solid">
              <Radio.Button value="duid">{METHOD_LABEL.duid}</Radio.Button>
              <Radio.Button value="l2mac" disabled>{METHOD_LABEL.l2mac}</Radio.Button>
            </Radio.Group>
          </Form.Item>
          {watchMethod === 'duid' && (
            <Form.Item
              label="DUID"
              name="duid"
              tooltip="终端的 DHCPv6 唯一标识"
              extra="可从在线 DHCPv6 终端下拉选，或手输 DUID。"
              rules={[{ required: true, message: '请输入 DUID' }]}
            >
              <AutoComplete
                options={duidOptions}
                placeholder="选择在线 DHCPv6 终端或手输 DUID"
                onSelect={onPickDuid}
                filterOption={(input, option) => String(option?.value ?? '').toLowerCase().includes(input.toLowerCase())}
                allowClear
              />
            </Form.Item>
          )}
          {watchMethod === 'l2mac' && (
            <Form.Item
              label="MAC"
              name="mac"
              tooltip="实验项：在二层尝试拦截，SLAAC 可绕过、MAC 可伪造"
              rules={[{ required: true, message: '请输入 MAC 地址' }]}
            >
              <Input placeholder="如 AA:BB:CC:DD:EE:FF" />
            </Form.Item>
          )}
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
