import { useCallback, useEffect, useRef, useState } from 'react';
import {
  dialDiagnose, dialStreamWsURL,
  type DialDiagnosis, type DialLogFrame, type LogEntry,
} from '../../api/logs';

const MAX_LINES = 1000;
const BACKOFF_BASE = 500;
const BACKOFF_MAX = 10_000;

export type ConnState = 'connecting' | 'open' | 'closed';
export type StageStatus = 'wait' | 'process' | 'finish' | 'error';

// 拨号阶段进度：四步固定流程「发现0 → 认证1 → 获址2 → 已连接3」。
export interface DialStage {
  step: number; // 当前到达步 0..3
  status: StageStatus; // 当前步状态
}

// 拨号成功后从日志流提取的关键网络参数。
export interface DialInfo {
  localIp?: string;
  gateway?: string;
  dnsPrimary?: string;
  dnsSecondary?: string;
}

export const INITIAL_STAGE: DialStage = { step: 0, status: 'wait' };

// 后端 phase → 进度条步号。teardown 不占独立步(返回 -1)，落在当前步标红。
const STEP_OF: Record<string, number> = {
  other: 0, discovery: 0, auth: 1, established: 2, ipcp: 2, teardown: -1,
};

export function stepOf(phase?: string): number {
  if (!phase) return 0;
  const s = STEP_OF[phase];
  return s === undefined ? 0 : s;
}

// advanceStage 逐条单调推进阶段（纯函数，便于测试）：
//  - 新一轮拨号(started by) → 重置到「发现·进行中」(重拨/新建可复用同一进度条)
//  - 拿到本端 IP(connected/success) → 全部完成
//  - error → 该阶段步标红并停住；teardown(掉线) → 当前步标红
//  - 其余信息态 → 向前推进到对应步(不回退)
export function advanceStage(prev: DialStage, e: LogEntry): DialStage {
  if (e.phase === 'other' && /started by/i.test(e.message || '')) {
    return { step: 0, status: 'process' };
  }
  if (e.dial_state === 'connected' || e.severity === 'success') {
    return { step: 3, status: 'finish' };
  }
  if (e.severity === 'error') {
    const s = stepOf(e.phase);
    return { step: s >= 0 ? s : prev.step, status: 'error' };
  }
  if (e.phase === 'teardown') {
    return { step: prev.step, status: 'error' };
  }
  const s = stepOf(e.phase);
  if (s < 0) return prev;
  if (s > prev.step || prev.status === 'error' || (s === prev.step && prev.status === 'wait')) {
    return { step: s, status: 'process' };
  }
  return prev;
}

// 各分词间一律用 \s+：pppd 不同版本在 "local  IP" / "primary   DNS" 等处空格数不一。
const RE_LOCAL_IP = /local\s+IP\s+address\s+(\d{1,3}(?:\.\d{1,3}){3})/i;
const RE_REMOTE_IP = /remote\s+IP\s+address\s+(\d{1,3}(?:\.\d{1,3}){3})/i;
const RE_DNS1 = /primary\s+DNS\s+address\s+(\S+)/i;
const RE_DNS2 = /secondary\s+DNS\s+address\s+(\S+)/i;

// extractInfo 从一条日志 message 累积提取 IP/网关/DNS（纯函数）。命中才更新对应字段。
export function extractInfo(prev: DialInfo, e: LogEntry): DialInfo {
  const msg = e.message || '';
  let next = prev;
  const set = (k: keyof DialInfo, v: string) => {
    if (next[k] !== v) next = { ...next, [k]: v };
  };
  const m1 = RE_LOCAL_IP.exec(msg); if (m1) set('localIp', m1[1]);
  const m2 = RE_REMOTE_IP.exec(msg); if (m2) set('gateway', m2[1]);
  const m3 = RE_DNS1.exec(msg); if (m3) set('dnsPrimary', m3[1]);
  const m4 = RE_DNS2.exec(msg); if (m4) set('dnsSecondary', m4[1]);
  return next;
}

function firstClause(s: string): string {
  for (const sep of ['。', '，', ',']) {
    const i = s.indexOf(sep);
    if (i > 0) return s.slice(0, i);
  }
  return s;
}

// nextBanner 把一条终态/进行中日志收敛成诊断横幅结论（与后端 Diagnose 文案一致）。
function nextBanner(prev: DialDiagnosis | null, e: LogEntry): DialDiagnosis | null {
  if ((e.severity === 'success' || e.severity === 'error' || e.severity === 'warning') && e.diagnosis) {
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
  if (e.dial_state === 'connecting' && /started by|PADI/i.test(e.message || '')) {
    return { dial_state: 'connecting', severity: 'info', headline: '拨号进行中…', iface: e.iface, updated_at: e.time };
  }
  return prev;
}

/**
 * useDialStream 拨号实时数据层：自管 WebSocket 生命周期 + 指数退避重连，
 * 把任意到达速率（含连上时 replay 的一大批）经 180ms 缓冲批量 flush 成极少次渲染，
 * 同时累积出「阶段进度 / 诊断横幅 / 关键信息 / 滚动日志」。iface 可选，用于按线路过滤。
 */
export function useDialStream(iface?: string) {
  const [lines, setLines] = useState<LogEntry[]>([]);
  const [banner, setBanner] = useState<DialDiagnosis | null>(null);
  const [conn, setConn] = useState<ConnState>('connecting');
  const [stage, setStage] = useState<DialStage>(INITIAL_STAGE);
  const [info, setInfo] = useState<DialInfo>({});

  const wsRef = useRef<WebSocket | null>(null);
  const seqRef = useRef(0);
  const retriesRef = useRef(0);
  const timerRef = useRef<number | null>(null);
  const stoppedRef = useRef(false);
  const pendingRef = useRef<LogEntry[]>([]); // onmessage 只入队，由定时器批量 flush
  const bannerRef = useRef<DialDiagnosis | null>(null);
  const stageRef = useRef<DialStage>(INITIAL_STAGE);
  const infoRef = useRef<DialInfo>({});

  // onmessage 必须 O(1)、不触发渲染——仅入缓冲，渲染交给 flush 定时器。
  const onFrame = useCallback((e: LogEntry) => { pendingRef.current.push(e); }, []);

  // flush：每 ~180ms 把缓冲帧一次性并入并重算 banner/stage/info，封顶 ~6 次渲染/秒。
  useEffect(() => {
    const flush = () => {
      const buf = pendingRef.current;
      if (buf.length === 0) return;
      pendingRef.current = [];
      let b = bannerRef.current, st = stageRef.current, nf = infoRef.current;
      for (const e of buf) {
        b = nextBanner(b, e);
        st = advanceStage(st, e);
        nf = extractInfo(nf, e);
      }
      if (b !== bannerRef.current) { bannerRef.current = b; setBanner(b); }
      if (st !== stageRef.current) { stageRef.current = st; setStage(st); }
      if (nf !== infoRef.current) { infoRef.current = nf; setInfo(nf); }
      setLines((prev) => {
        const merged = prev.concat(buf);
        return merged.length > MAX_LINES ? merged.slice(merged.length - MAX_LINES) : merged;
      });
    };
    const t = window.setInterval(flush, 180);
    return () => window.clearInterval(t);
  }, []);

  const clear = useCallback(() => { pendingRef.current = []; setLines([]); }, []);

  const scheduleReconnect = useCallback((connect: () => void) => {
    if (stoppedRef.current || timerRef.current != null) return;
    const attempt = retriesRef.current++;
    const delay = Math.min(BACKOFF_BASE * 2 ** attempt, BACKOFF_MAX);
    timerRef.current = window.setTimeout(() => { timerRef.current = null; connect(); }, delay);
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
    dialDiagnose(iface).then((d) => {
      if (!stoppedRef.current && d.dial_state !== 'unknown') { bannerRef.current = d; setBanner(d); }
    }).catch(() => {});
    connect();
    return () => {
      stoppedRef.current = true;
      if (timerRef.current != null) { clearTimeout(timerRef.current); timerRef.current = null; }
      if (wsRef.current) { wsRef.current.close(); wsRef.current = null; }
    };
  }, [iface, connect]);

  return { lines, banner, conn, stage, info, clear };
}
