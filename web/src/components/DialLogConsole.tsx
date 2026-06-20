import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Empty, Space, Switch, Tag, Typography } from 'antd';
import {
  SyncOutlined, ClearOutlined, DownloadOutlined,
  CheckCircleOutlined, CloseCircleOutlined, WarningOutlined,
} from '@ant-design/icons';
import {
  dialDiagnose, dialStreamWsURL, downloadLogs,
  type DialDiagnosis, type DialLogFrame, type LogEntry, type DialSeverity,
} from '../api/logs';

const { Text } = Typography;

const MAX_LINES = 1000;
const BACKOFF_BASE = 500;
const BACKOFF_MAX = 10_000;

// 拨号阶段中文标签。
const PHASE_LABEL: Record<string, string> = {
  discovery: '发现', auth: '认证', ipcp: '获址', established: '建链', teardown: '断开', other: '—',
};

// severity → 颜色（行着色 / 横幅类型）。
function sevColor(s?: DialSeverity): string {
  switch (s) {
    case 'success': return '#52c41a';
    case 'error': return '#ff4d4f';
    case 'warning': return '#fa8c16';
    default: return '#8c8c8c';
  }
}
function sevAlertType(s?: DialSeverity): 'success' | 'error' | 'warning' | 'info' {
  if (s === 'success' || s === 'error' || s === 'warning') return s;
  return 'info';
}

function firstClause(s: string): string {
  for (const sep of ['。', '，', ',']) {
    const i = s.indexOf(sep);
    if (i > 0) return s.slice(0, i);
  }
  return s;
}

// 把一条终态日志行收敛成横幅结论（与后端 Diagnose 文案一致）。
function frameToBanner(e: LogEntry): DialDiagnosis {
  let headline = e.diagnosis || '';
  if (e.severity === 'success') headline = '拨号成功：已获取 IP，连接已建立';
  else if (e.severity === 'error') headline = '拨号失败：' + firstClause(e.diagnosis || '');
  else if (e.severity === 'warning') headline = '连接异常：' + firstClause(e.diagnosis || '');
  return {
    iface: e.iface, dial_state: (e.dial_state as DialDiagnosis['dial_state']) || 'unknown',
    phase: e.phase, severity: e.severity, headline, diagnosis: e.diagnosis, advice: e.advice,
    matched_line: e.message, updated_at: e.time,
  };
}

/**
 * DialLogConsole 实时拨号日志控制台：诊断横幅(把 pppd 报错翻成人话+建议) + 终端式滚动日志。
 * 自管 WebSocket 生命周期与指数退避重连；卸载即断开。iface 可选，用于按接口过滤。
 */
export default function DialLogConsole({ iface }: { iface?: string }) {
  const [lines, setLines] = useState<LogEntry[]>([]);
  const [banner, setBanner] = useState<DialDiagnosis | null>(null);
  const [conn, setConn] = useState<'connecting' | 'open' | 'closed'>('connecting');
  const [errorOnly, setErrorOnly] = useState(false);
  const [follow, setFollow] = useState(true);

  const wsRef = useRef<WebSocket | null>(null);
  const seqRef = useRef(0);
  const retriesRef = useRef(0);
  const timerRef = useRef<number | null>(null);
  const stoppedRef = useRef(false);
  const followRef = useRef(true);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  followRef.current = follow;

  // 新拨号周期开始(pppd started / 进入 connecting) → 横幅暂置「拨号中」，等终态覆盖。
  const onFrame = useCallback((e: LogEntry) => {
    setLines((prev) => {
      const next = prev.length >= MAX_LINES ? prev.slice(prev.length - MAX_LINES + 1) : prev;
      return [...next, e];
    });
    if (e.severity === 'success' || e.severity === 'error' || e.severity === 'warning') {
      if (e.diagnosis) setBanner(frameToBanner(e));
    } else if (e.dial_state === 'connecting' && /started by|PADI/i.test(e.message || '')) {
      setBanner({ dial_state: 'connecting', severity: 'info', headline: '拨号进行中…', iface: e.iface, updated_at: e.time });
    }
  }, []);

  const scheduleReconnect = useCallback((connect: () => void) => {
    if (stoppedRef.current || timerRef.current != null) return;
    const attempt = retriesRef.current++;
    const delay = Math.min(BACKOFF_BASE * 2 ** attempt, BACKOFF_MAX);
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      connect();
    }, delay);
  }, []);

  const connect = useCallback(() => {
    if (stoppedRef.current) return;
    setConn('connecting');
    let ws: WebSocket;
    try {
      ws = new WebSocket(dialStreamWsURL(iface, 80));
    } catch {
      scheduleReconnect(connect);
      return;
    }
    wsRef.current = ws;
    ws.onopen = () => { retriesRef.current = 0; setConn('open'); };
    ws.onmessage = (ev) => {
      try {
        const fr = JSON.parse(ev.data as string) as DialLogFrame;
        if (typeof fr.seq === 'number') {
          if (fr.seq <= seqRef.current) return; // 去重
          seqRef.current = fr.seq;
        }
        if (fr.data) onFrame(fr.data);
      } catch { /* 忽略非法帧 */ }
    };
    ws.onerror = () => setConn('closed');
    ws.onclose = () => { wsRef.current = null; setConn('closed'); scheduleReconnect(connect); };
  }, [iface, onFrame, scheduleReconnect]);

  useEffect(() => {
    stoppedRef.current = false;
    // 先取一次诊断结论，横幅即时有内容（即便此刻没有新拨号）。
    dialDiagnose(iface).then((d) => { if (!stoppedRef.current && d.dial_state !== 'unknown') setBanner(d); }).catch(() => {});
    connect();
    return () => {
      stoppedRef.current = true;
      if (timerRef.current != null) { clearTimeout(timerRef.current); timerRef.current = null; }
      if (wsRef.current) { wsRef.current.close(); wsRef.current = null; }
    };
  }, [iface, connect]);

  // 自动滚动到底（除非用户暂停跟随）。
  useEffect(() => {
    if (followRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [lines]);

  const shown = errorOnly ? lines.filter((l) => l.severity === 'error' || l.severity === 'warning') : lines;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, height: '100%' }}>
      {banner && (
        <Alert
          type={sevAlertType(banner.severity)}
          showIcon
          icon={
            banner.severity === 'success' ? <CheckCircleOutlined /> :
            banner.severity === 'error' ? <CloseCircleOutlined /> :
            banner.severity === 'warning' ? <WarningOutlined /> :
            <SyncOutlined spin />
          }
          message={<Text strong>{banner.headline}</Text>}
          description={
            (banner.diagnosis || banner.advice) && (
              <div style={{ fontSize: 13 }}>
                {banner.diagnosis && <div>诊断：{banner.diagnosis}</div>}
                {banner.advice && <div style={{ marginTop: 2 }}>建议：{banner.advice}</div>}
              </div>
            )
          }
        />
      )}

      <Space wrap size={12} style={{ fontSize: 13 }}>
        <Tag color={conn === 'open' ? 'green' : conn === 'connecting' ? 'blue' : 'red'} icon={conn === 'connecting' ? <SyncOutlined spin /> : undefined}>
          {conn === 'open' ? '● 实时' : conn === 'connecting' ? '连接中' : '已断开'}
        </Tag>
        <span><Switch size="small" checked={errorOnly} onChange={setErrorOnly} /> 仅看错误</span>
        <span><Switch size="small" checked={follow} onChange={setFollow} checkedChildren="跟随" unCheckedChildren="暂停" /> 自动滚动</span>
        <Button size="small" icon={<ClearOutlined />} onClick={() => { setLines([]); }}>清屏</Button>
        <Button size="small" icon={<DownloadOutlined />} onClick={() => downloadLogs('dialup', {})}>导出</Button>
        <Text type="secondary">{shown.length} 行</Text>
      </Space>

      <div
        ref={scrollRef}
        style={{
          flex: 1, minHeight: 280, overflowY: 'auto', background: '#0b0e14', borderRadius: 6,
          padding: '8px 12px', fontFamily: 'ui-monospace, Menlo, Consolas, monospace', fontSize: 12.5, lineHeight: 1.7,
        }}
      >
        {shown.length === 0 ? (
          <div style={{ height: 240, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={<span style={{ color: '#8c8c8c' }}>
              {conn === 'open' ? '等待拨号日志…（在面板点「重拨」可触发）' : '正在连接拨号日志流…'}
            </span>} />
          </div>
        ) : (
          shown.map((l, i) => (
            <div key={(l.seq ?? i) + '-' + i} style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
              <span style={{ color: '#5c6370' }}>{l.time?.slice(11) || ''}</span>{' '}
              <span style={{ color: '#7f848e' }}>[{PHASE_LABEL[l.phase || ''] || (l.proc ?? '')}]</span>{' '}
              <span style={{ color: sevColor(l.severity) }}>{l.message}</span>
              {l.advice && (l.severity === 'error' || l.severity === 'warning') && (
                <span style={{ color: '#e6a23c' }}>　← {firstClause(l.advice)}</span>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  );
}
