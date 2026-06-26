import { Alert, Button, Drawer, Space, Typography } from 'antd';
import {
  CheckCircleOutlined, CloseCircleOutlined, WarningOutlined, SyncOutlined,
  PoweroffOutlined, FileSearchOutlined,
} from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import type { DialSeverity } from '../../api/logs';
import { useDialStream } from './useDialStream';
import DialStageSteps from './DialStageSteps';
import DialInfoCard from './DialInfoCard';
import DialLogStream from './DialLogStream';

const { Text } = Typography;

function sevAlertType(s?: DialSeverity): 'success' | 'error' | 'warning' | 'info' {
  if (s === 'success' || s === 'error' || s === 'warning') return s;
  return 'info';
}

function bannerIcon(s?: DialSeverity) {
  if (s === 'success') return <CheckCircleOutlined />;
  if (s === 'error') return <CloseCircleOutlined />;
  if (s === 'warning') return <WarningOutlined />;
  return <SyncOutlined spin />;
}

// DialBody 仅在 Drawer 打开时挂载，使 WS 订阅随面板开关精确建立/断开。
function DialBody({ iface, onRedial, onClose }: { iface?: string; onRedial?: () => void; onClose: () => void }) {
  const navigate = useNavigate();
  const { lines, banner, conn, stage, info, clear } = useDialStream(iface);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14, height: '100%', minHeight: 0 }}>
      <DialStageSteps stage={stage} />

      {banner && (
        <Alert
          type={sevAlertType(banner.severity)}
          showIcon
          icon={bannerIcon(banner.severity)}
          message={<Text strong>{banner.headline}</Text>}
          description={(banner.diagnosis || banner.advice) ? (
            <div style={{ fontSize: 13 }}>
              {banner.diagnosis && <div>诊断：{banner.diagnosis}</div>}
              {banner.advice && <div style={{ marginTop: 2 }}>建议：{banner.advice}</div>}
            </div>
          ) : undefined}
        />
      )}

      <DialInfoCard info={info} />

      <div style={{ flex: 1, minHeight: 220 }}>
        <DialLogStream lines={lines} conn={conn} onClear={clear} iface={iface} />
      </div>

      <Space>
        {onRedial && <Button type="primary" icon={<PoweroffOutlined />} onClick={onRedial}>重拨</Button>}
        <Button
          icon={<FileSearchOutlined />}
          onClick={() => { onClose(); navigate(`/logs/dialup${iface ? `?iface=${encodeURIComponent(iface)}` : ''}`); }}
        >
          查看完整历史
        </Button>
        <Button onClick={onClose}>关闭</Button>
      </Space>
    </div>
  );
}

/**
 * DialDiagnosticDrawer 右侧拨号诊断面板：阶段进度条 + 诊断横幅(人话+建议) + 成功信息卡 + 实时日志流。
 * 由 NetOverview 顶层管理开关，保存 PPPoE / 点重拨后弹出；提升到顶层后不再随编辑抽屉关闭而销毁。
 */
export default function DialDiagnosticDrawer({ open, iface, name, onClose, onRedial }: {
  open: boolean; iface?: string; name?: string; onClose: () => void; onRedial?: () => void;
}) {
  return (
    <Drawer
      title={`拨号诊断${name ? ' · ' + name : ''}`}
      open={open}
      onClose={onClose}
      width="min(92vw, 720px)"
      styles={{ body: { display: 'flex', flexDirection: 'column' } }}
    >
      {open && <DialBody iface={iface} onRedial={onRedial} onClose={onClose} />}
    </Drawer>
  );
}
