import { useEffect, useState } from 'react';
import { Alert, App, Button, Card, Divider, Form, Input, InputNumber, Switch, Tooltip, Typography } from 'antd';
import { ThunderboltOutlined } from '@ant-design/icons';
import PageCard from '../../components/PageCard';
import { extractErr } from '../../hooks/useNetData';
import * as dns from '../../api/dns';

interface SettingsForm {
  enabled: boolean;
  dns_primary: string;
  dns_secondary: string;
  no_resolv: boolean;
  filter_aaaa: boolean;
  aging: number; // 老化时间(秒) → 映射 local_ttl + min_cache_ttl
  cache_size: number;
  force_proxy: boolean;
}

interface DoHForm {
  enabled: boolean;
  resolver_url: string;
  listen_port: number;
  bootstrap_dns: string;
}

export default function DnsSettingsPage() {
  const { message } = App.useApp();
  const [svc, setSvc] = useState<dns.DNSSvcInfo | null>(null);
  const [savingS, setSavingS] = useState(false);
  const [savingD, setSavingD] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [sForm] = Form.useForm<SettingsForm>();
  const [dForm] = Form.useForm<DoHForm>();

  const load = async () => {
    try {
      const [s, d, info] = await Promise.all([dns.getDNSSettings(), dns.getDNSDoH(), dns.getDNSService()]);
      setSvc(info);
      sForm.setFieldsValue({
        enabled: s.enabled,
        dns_primary: s.dns_primary,
        dns_secondary: s.dns_secondary,
        no_resolv: s.no_resolv,
        filter_aaaa: s.filter_aaaa,
        aging: s.min_cache_ttl || s.local_ttl || 0,
        cache_size: s.cache_size || 0,
        force_proxy: s.force_proxy,
      });
      dForm.setFieldsValue({
        enabled: d.enabled,
        resolver_url: d.resolver_url || 'https://dns.alidns.com/dns-query',
        listen_port: d.listen_port || 5053,
        bootstrap_dns: d.bootstrap_dns || '223.5.5.5',
      });
    } catch (e) {
      message.error(extractErr(e));
    }
  };

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onSaveSettings = async () => {
    const v = await sForm.validateFields();
    setSavingS(true);
    try {
      await dns.saveDNSSettings({
        enabled: v.enabled,
        dns_primary: v.dns_primary ?? '',
        dns_secondary: v.dns_secondary ?? '',
        no_resolv: v.no_resolv,
        filter_aaaa: v.filter_aaaa,
        cache_size: v.cache_size ?? 0,
        local_ttl: v.aging ?? 0,
        min_cache_ttl: v.aging ?? 0,
        max_cache_ttl: 0,
        force_proxy: v.force_proxy,
      });
      message.success('DNS 设置已保存');
      void load();
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setSavingS(false);
    }
  };

  const onSaveDoH = async () => {
    const v = await dForm.validateFields();
    setSavingD(true);
    try {
      await dns.saveDNSDoH({
        enabled: v.enabled,
        resolver_url: v.resolver_url ?? '',
        listen_port: v.listen_port ?? 5053,
        bootstrap_dns: v.bootstrap_dns ?? '',
      });
      message.success('DoH 设置已保存');
      void load();
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setSavingD(false);
    }
  };

  const onInstall = async () => {
    setInstalling(true);
    try {
      const r = await dns.installDoH();
      message.success('DoH 组件安装完成');
      void r;
      void load();
    } catch (e) {
      message.error('安装失败：' + extractErr(e));
    } finally {
      setInstalling(false);
    }
  };

  const aaaaUnsupported = svc !== null && !svc.filter_aaaa_supported;
  const dohUninstalled = svc !== null && !svc.doh_installed;

  return (
    <PageCard breadcrumb={['网络设置', 'DNS 设置', 'DNS 设置']} title="DNS 设置">
      <Card size="small" title="基础 DNS" style={{ maxWidth: 720 }}>
        <Form form={sForm} layout="vertical">
          <Form.Item
            label="由本工具托管 DNS 设置"
            name="enabled"
            valuePropName="checked"
            extra="关闭时不改动路由器现有 DNS（部署默认态=零改动）。开启后下面的设置才会下发。"
          >
            <Switch />
          </Form.Item>
          <Form.Item label="首选 DNS（上游）" name="dns_primary">
            <Input placeholder="如 223.5.5.5" allowClear />
          </Form.Item>
          <Form.Item label="备选 DNS（上游）" name="dns_secondary" extra="dnsmasq 多上游为「最快优先」，非严格主备顺序。">
            <Input placeholder="如 114.114.114.114" allowClear />
          </Form.Item>
          <Form.Item
            label="仅使用指定上游（不读运营商下发）"
            name="no_resolv"
            valuePropName="checked"
            extra="开启前必须填好首选/备选，否则路由器自身也会无法解析。"
          >
            <Switch />
          </Form.Item>
          <Form.Item
            label="禁止 AAAA(IPv6) 解析"
            name="filter_aaaa"
            valuePropName="checked"
            extra={aaaaUnsupported ? '本机 dnsmasq 不支持 --filter-AAAA（需 dnsmasq-full），已禁用。' : '从应答中移除 AAAA 记录。'}
          >
            <Switch disabled={aaaaUnsupported} />
          </Form.Item>
          <Form.Item label="缓存老化时间（秒）" name="aging" extra="本地记录与缓存最短 TTL；dnsmasq 默认上限 3600。0=不设。">
            <InputNumber min={0} max={3600} style={{ width: 200 }} />
          </Form.Item>
          <Form.Item label="DNS 缓存大小（条）" name="cache_size" extra="0=保持系统默认（不改）。">
            <InputNumber min={0} style={{ width: 200 }} />
          </Form.Item>
          <Form.Item
            label="强制客户端 DNS 代理"
            name="force_proxy"
            valuePropName="checked"
            extra="用防火墙把客户端 53 端口流量强制重定向到本机（仅拦 53，无法拦截客户端自带 DoH/443）。旁路由需谨慎。"
          >
            <Switch />
          </Form.Item>
          <Button type="primary" loading={savingS} onClick={onSaveSettings}>
            保存基础 DNS
          </Button>
        </Form>
      </Card>

      <Divider />

      <Card
        size="small"
        title="DNS 加速（DoH over HTTPS）"
        style={{ maxWidth: 720 }}
        extra={
          dohUninstalled ? (
            <Tooltip title="安装 https-dns-proxy 后才能启用 DoH">
              <Button size="small" icon={<ThunderboltOutlined />} loading={installing} onClick={onInstall}>
                一键安装 DoH 组件
              </Button>
            </Tooltip>
          ) : null
        }
      >
        {dohUninstalled && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 12 }}
            message="未安装 https-dns-proxy，DoH 暂不可用。点右上角「一键安装 DoH 组件」（需联网）。"
          />
        )}
        <Form form={dForm} layout="vertical">
          <Form.Item label="启用 DoH" name="enabled" valuePropName="checked">
            <Switch disabled={dohUninstalled} />
          </Form.Item>
          <Form.Item label="DoH 请求地址" name="resolver_url" rules={[{ pattern: /^https:\/\//, message: '必须以 https:// 开头' }]}>
            <Input placeholder="https://dns.alidns.com/dns-query" allowClear />
          </Form.Item>
          <Form.Item label="本机监听端口" name="listen_port" extra="必须 ≠ 53（避免与 dnsmasq 冲突），默认 5053。">
            <InputNumber min={1} max={65535} style={{ width: 200 }} />
          </Form.Item>
          <Form.Item label="引导 DNS" name="bootstrap_dns" extra="用于解析 DoH 服务器域名，如 223.5.5.5。">
            <Input placeholder="223.5.5.5" allowClear />
          </Form.Item>
          <Button type="primary" loading={savingD} onClick={onSaveDoH} disabled={dohUninstalled}>
            保存 DoH
          </Button>
        </Form>
      </Card>

      <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>
        说明：本页设置作用于本机（主路由/旁路由通用）。客户端需续租或重连后生效。
      </Typography.Paragraph>
    </PageCard>
  );
}
