import { useMemo, useState } from 'react';
import { App, Button, Input, Select, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useNavigate } from 'react-router-dom';
import PageCard from '../components/PageCard';
import { useNetData, extractErr } from '../hooks/useNetData';
import * as net from '../api/netcfg';

const ALL = '__all__';

/** 把秒数格式化为 HH:MM:SS。 */
function formatRemaining(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(h)}:${pad(m)}:${pad(s)}`;
}

export default function DhcpLeasesPage() {
  const { message } = App.useApp();
  const navigate = useNavigate();
  const { data, loading, reload } = useNetData<net.Lease[]>(() => net.listLeases(), []);
  const { data: ifaces } = useNetData<net.NetInterface[]>(() => net.listInterfaces(), []);

  const [iface, setIface] = useState<string>(ALL);
  const [status, setStatus] = useState<'static' | 'dynamic' | typeof ALL>(ALL);
  const [keyword, setKeyword] = useState('');
  const [selected, setSelected] = useState<string[]>([]);

  // 接口下拉：优先用 listInterfaces 的网卡名，回退到租约里去重的 interface。
  const ifaceOptions = useMemo(() => {
    const names = new Set<string>();
    ifaces.forEach((i) => i.name && names.add(i.name));
    data.forEach((l) => l.interface && names.add(l.interface));
    return Array.from(names).sort();
  }, [ifaces, data]);

  // 客户端过滤（接口 / 状态 / 搜索）。
  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    return data.filter((l) => {
      if (iface !== ALL && l.interface !== iface) return false;
      if (status === 'static' && !l.static) return false;
      if (status === 'dynamic' && l.static) return false;
      if (kw) {
        const hay = `${l.ip} ${l.mac} ${l.hostname} ${l.remark}`.toLowerCase();
        if (!hay.includes(kw)) return false;
      }
      return true;
    });
  }, [data, iface, status, keyword]);

  const selectedLeases = useMemo(
    () => filtered.filter((l) => selected.includes(l.ip)),
    [filtered, selected],
  );

  async function reserveOne(l: net.Lease) {
    await net.reserveLease({ ip: l.ip, mac: l.mac, hostname: l.hostname, interface: l.interface });
  }
  async function blacklistOne(l: net.Lease) {
    await net.blacklistLease({ mac: l.mac, remark: l.hostname });
  }

  // 行内：加入静态分配。
  async function onReserveRow(l: net.Lease) {
    try {
      await reserveOne(l);
      message.success(`已将 ${l.ip} 加入静态分配`);
      await reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  }

  // 行内：加入 MAC 黑名单。
  async function onBlacklistRow(l: net.Lease) {
    try {
      await blacklistOne(l);
      message.success(`已将 ${l.mac} 加入 MAC 黑名单`);
      await reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  }

  // 批量：对选中的动态租约逐条加入静态分配（静态项跳过）。
  async function onBatchReserve() {
    const targets = selectedLeases.filter((l) => !l.static);
    if (targets.length === 0) {
      message.warning('请选择动态租约');
      return;
    }
    let ok = 0;
    let fail = 0;
    for (const l of targets) {
      try {
        await reserveOne(l);
        ok += 1;
      } catch {
        fail += 1;
      }
    }
    if (ok > 0) message.success(`成功加入静态分配 ${ok} 条${fail > 0 ? `，失败 ${fail} 条` : ''}`);
    else message.error(`加入静态分配失败 ${fail} 条`);
    setSelected([]);
    await reload();
  }

  // 批量：对选中租约逐条加入 MAC 黑名单。
  async function onBatchBlacklist() {
    if (selectedLeases.length === 0) {
      message.warning('请先选择租约');
      return;
    }
    let ok = 0;
    let fail = 0;
    for (const l of selectedLeases) {
      try {
        await blacklistOne(l);
        ok += 1;
      } catch {
        fail += 1;
      }
    }
    if (ok > 0) message.success(`成功加入 MAC 黑名单 ${ok} 条${fail > 0 ? `，失败 ${fail} 条` : ''}`);
    else message.error(`加入 MAC 黑名单失败 ${fail} 条`);
    setSelected([]);
    await reload();
  }

  // 一键固定同网段。
  async function onFixSubnet() {
    try {
      const added = await net.fixSubnet(iface === ALL ? '' : iface);
      message.success(`已固定同网段，新增 ${added} 条`);
      await reload();
    } catch (e) {
      message.error(extractErr(e));
    }
  }

  const columns: ColumnsType<net.Lease> = [
    { title: '主机名称', dataIndex: 'hostname', key: 'hostname', render: (v: string) => v || '-' },
    { title: '终端 IP', dataIndex: 'ip', key: 'ip' },
    { title: '终端 MAC', dataIndex: 'mac', key: 'mac' },
    {
      title: '有效时间',
      key: 'remaining',
      render: (_, r) =>
        r.static || r.remaining_seconds <= 0 ? '静态/永久' : formatRemaining(r.remaining_seconds),
    },
    { title: '绑定接口', dataIndex: 'interface', key: 'interface', render: (v: string) => v || '-' },
    {
      title: '状态',
      key: 'status',
      render: (_, r) =>
        r.static ? <Tag color="success">静态分配</Tag> : <Tag color="processing">动态分配</Tag>,
    },
    { title: '备注', dataIndex: 'remark', key: 'remark', render: (v: string) => v || '-' },
    {
      title: '操作',
      key: 'actions',
      width: 220,
      fixed: 'right',
      render: (_, r) => (
        <Space size="middle" style={{ whiteSpace: 'nowrap' }}>
          {/* 已是静态分配的终端：跳到「DHCP 静态分配」查看；动态的才显示「加入静态分配」 */}
          {r.static ? (
            <Typography.Link onClick={() => navigate(`/dhcp/statics?q=${encodeURIComponent(r.ip)}`)}>查看</Typography.Link>
          ) : (
            <Typography.Link onClick={() => onReserveRow(r)}>加入静态分配</Typography.Link>
          )}
          <Typography.Link onClick={() => onBlacklistRow(r)}>加入 MAC 黑名单</Typography.Link>
        </Space>
      ),
    },
  ];

  return (
    <PageCard
      breadcrumb={['网络设置', 'DHCP设置', 'DHCP终端列表']}
      title="DHCP 终端列表"
      toolbar={
        <>
          <Space size="middle" wrap>
            <Select<string>
              value={iface}
              onChange={setIface}
              style={{ width: 160 }}
              options={[
                { label: '全部接口', value: ALL },
                ...ifaceOptions.map((n) => ({ label: n, value: n })),
              ]}
            />
            <Select<'static' | 'dynamic' | typeof ALL>
              value={status}
              onChange={setStatus}
              style={{ width: 140 }}
              options={[
                { label: '全部状态', value: ALL },
                { label: '静态分配', value: 'static' },
                { label: '动态分配', value: 'dynamic' },
              ]}
            />
            <Input.Search
              allowClear
              placeholder="搜索 IP / MAC / 主机名 / 备注"
              style={{ width: 240 }}
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
            />
          </Space>
          <Space size="middle" wrap>
            <Button disabled={selected.length === 0} onClick={onBatchReserve}>
              加入静态分配
            </Button>
            <Button disabled={selected.length === 0} onClick={onBatchBlacklist}>
              加入 MAC 黑名单
            </Button>
            <Button type="primary" onClick={onFixSubnet}>
              一键固定同网段
            </Button>
          </Space>
        </>
      }
    >
      <Table
        rowKey={(r) => r.ip}
        size="small"
        bordered
        loading={loading}
        dataSource={filtered}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: (k) => setSelected(k as string[]),
        }}
        columns={columns}
        scroll={{ x: 'max-content' }}
        pagination={{ pageSize: 20, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
      />
    </PageCard>
  );
}
