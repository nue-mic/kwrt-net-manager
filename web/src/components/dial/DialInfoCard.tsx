import { Descriptions } from 'antd';
import type { DialInfo } from './useDialStream';

/**
 * DialInfoCard 拨号成功后的关键网络参数卡（本端 IP / 网关 / 主备 DNS）。
 * 数据由 useDialStream 从日志流正则提取；尚未拿到本端 IP 时不渲染（拨号中/失败态隐藏）。
 */
export default function DialInfoCard({ info }: { info: DialInfo }) {
  if (!info.localIp) return null;
  return (
    <Descriptions size="small" column={2} bordered
      items={[
        { key: 'ip', label: '本端 IP', children: info.localIp },
        { key: 'gw', label: '网关', children: info.gateway || '—' },
        { key: 'd1', label: '主 DNS', children: info.dnsPrimary || '—' },
        { key: 'd2', label: '备 DNS', children: info.dnsSecondary || '—' },
      ]}
    />
  );
}
