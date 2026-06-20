// 与后端 internal/eventbus/types.go 对齐
export type EventType =
  | 'instance.state'
  | 'instance.error'
  | 'proxy.status'
  | 'proxy.connections'
  | 'config.changed'
  | 'config.deleted'
  | 'log.line'
  | 'backup.run'
  // 网络配置领域事件（用于各页按领域订阅自动刷新；useNetData 也据 lastSeq 全量刷新）
  | 'dhcp.changed'
  | 'static.changed'
  | 'lease.changed'
  | 'acl.changed'
  | 'route.changed'
  | 'iface.changed'
  | 'ipv6.changed'
  | 'dns.changed';

export interface BusEvent<T = unknown> {
  seq: number;
  type: EventType;
  config_id?: string;
  ts: string;
  data?: T;
}

export interface InstanceStateData {
  state: string;
  prev_state?: string;
}

export interface InstanceErrorData {
  message: string;
}

export interface ProxyStatusData {
  name: string;
  type: string;
  status: string;
  remote_addr?: string;
  error?: string;
}

export interface ProxyConnectionsData {
  name: string;
  type: string;
  cur_conns: number;
}

export interface LogLineData {
  line: string;
}

export type ConnState = 'idle' | 'connecting' | 'open' | 'closed' | 'error';
