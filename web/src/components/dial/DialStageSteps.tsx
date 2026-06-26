import type { ReactNode } from 'react';
import { Steps } from 'antd';
import { LoadingOutlined } from '@ant-design/icons';
import type { DialStage } from './useDialStream';

// PPPoE 拨号四步固定流程。
const TITLES = ['发现', '认证', '获址', '已连接'];

// 每步在当前 stage 下的副标题文案。
function descOf(i: number, stage: DialStage): string {
  if (stage.status === 'finish') return '完成'; // 已全部成功
  if (i < stage.step) return '完成';
  if (i > stage.step) return '等待';
  if (stage.status === 'error') return '失败';
  if (stage.status === 'process') return '进行中…';
  return '等待';
}

/**
 * DialStageSteps 拨号阶段进度条：把后端 phase 推进出的 DialStage 映射成 Ant Steps——
 * 当前步转圈(process)、已过步绿勾(finish)、卡住步红叉(error)，一眼看出拨到哪一步、卡在哪。
 */
export default function DialStageSteps({ stage }: { stage: DialStage }) {
  const status = stage.status === 'error' ? 'error' : stage.status === 'finish' ? 'finish' : 'process';
  const items = TITLES.map((title, i) => {
    const it: { title: string; description: string; icon?: ReactNode } = { title, description: descOf(i, stage) };
    if (i === stage.step && stage.status === 'process') it.icon = <LoadingOutlined />;
    return it;
  });
  return <Steps size="small" current={stage.step} status={status} items={items} />;
}
