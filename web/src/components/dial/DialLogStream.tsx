import { useEffect, useRef, useState } from 'react';
import { Button, Empty, Space, Switch, Tag, Typography } from 'antd';
import { SyncOutlined, ClearOutlined, DownloadOutlined } from '@ant-design/icons';
import { downloadLogs, type LogEntry, type DialSeverity } from '../../api/logs';
import type { ConnState } from './useDialStream';

const { Text } = Typography;

// 拨号阶段中文标签（终端行内 [阶段] 前缀）。
const PHASE_LABEL: Record<string, string> = {
  discovery: '发现', auth: '认证', ipcp: '获址', established: '建链', teardown: '断开', other: '—',
};

// severity → 行着色。
function sevColor(s?: DialSeverity): string {
  switch (s) {
    case 'success': return '#52c41a';
    case 'error': return '#ff4d4f';
    case 'warning': return '#fa8c16';
    default: return '#8c8c8c';
  }
}

function firstClause(s: string): string {
  for (const sep of ['。', '，', ',']) {
    const i = s.indexOf(sep);
    if (i > 0) return s.slice(0, i);
  }
  return s;
}

/**
 * DialLogStream 终端式滚动日志区 + 工具条（仅看错误 / 自动滚动 / 清屏 / 导出）。
 * 纯展示：数据(lines/conn)由上层 useDialStream 提供；自管错误过滤与跟随滚动两个本地视图状态。
 */
export default function DialLogStream({ lines, conn, onClear, iface }: {
  lines: LogEntry[]; conn: ConnState; onClear: () => void; iface?: string;
}) {
  const [errorOnly, setErrorOnly] = useState(false);
  const [follow, setFollow] = useState(true);
  const followRef = useRef(true);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  followRef.current = follow;

  // 自动滚动到底（除非用户暂停跟随）。
  useEffect(() => {
    if (followRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [lines]);

  const shown = errorOnly ? lines.filter((l) => l.severity === 'error' || l.severity === 'warning') : lines;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, height: '100%', minHeight: 0 }}>
      <Space wrap size={12} style={{ fontSize: 13 }}>
        <Tag color={conn === 'open' ? 'green' : conn === 'connecting' ? 'blue' : 'red'} icon={conn === 'connecting' ? <SyncOutlined spin /> : undefined}>
          {conn === 'open' ? '● 实时' : conn === 'connecting' ? '连接中' : '已断开'}
        </Tag>
        <span><Switch size="small" checked={errorOnly} onChange={setErrorOnly} /> 仅看错误</span>
        <span><Switch size="small" checked={follow} onChange={setFollow} checkedChildren="跟随" unCheckedChildren="暂停" /> 自动滚动</span>
        <Button size="small" icon={<ClearOutlined />} onClick={onClear}>清屏</Button>
        <Button size="small" icon={<DownloadOutlined />} onClick={() => downloadLogs('dialup', iface ? { iface } : {})}>导出</Button>
        <Text type="secondary">{shown.length} 行</Text>
      </Space>

      <div
        ref={scrollRef}
        style={{
          flex: 1, minHeight: 200, overflowY: 'auto', background: '#0b0e14', borderRadius: 6,
          padding: '8px 12px', fontFamily: 'ui-monospace, Menlo, Consolas, monospace', fontSize: 12.5, lineHeight: 1.7,
        }}
      >
        {shown.length === 0 ? (
          <div style={{ height: 200, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={<span style={{ color: '#8c8c8c' }}>
              {conn === 'open' ? '等待拨号日志…（点「重拨」可触发）' : '正在连接拨号日志流…'}
            </span>} />
          </div>
        ) : (
          shown.map((l, i) => (
            <div key={l.seq ?? `${l.ts}-${i}`} style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
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
