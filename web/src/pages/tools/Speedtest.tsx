import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Card, Col, Row, Space, Spin, Statistic, Tooltip, Typography } from 'antd';
import { ThunderboltOutlined, ArrowDownOutlined, ArrowUpOutlined, DashboardOutlined } from '@ant-design/icons';
import PageCard from '../../components/PageCard';
import { extractErr } from '../../hooks/useNetData';
import * as st from '../../api/speedtest';

export default function SpeedtestPage() {
  const { message } = App.useApp();
  const [svc, setSvc] = useState<st.SpeedtestSvcInfo | null>(null);
  const [status, setStatus] = useState<st.SpeedtestStatus | null>(null);
  const [installing, setInstalling] = useState(false);
  const [starting, setStarting] = useState(false);
  const timer = useRef<number | null>(null);

  const loadSvc = async () => {
    try {
      setSvc(await st.getSpeedtestService());
    } catch {
      /* 忽略 */
    }
  };

  // poll 自维护轮询：测速中(running)就启动 1.5s 间隔轮询，结束即停。
  // 这样无论是本页点「开始测速」还是页面挂载时发现已有测速在跑，都能自动刷新进度。
  const poll = useCallback(async () => {
    try {
      const s = await st.getSpeedtestStatus();
      setStatus(s);
      if (s.running) {
        if (!timer.current) timer.current = window.setInterval(() => void poll(), 1500);
      } else if (timer.current) {
        window.clearInterval(timer.current);
        timer.current = null;
      }
    } catch {
      /* 忽略 */
    }
  }, []);

  useEffect(() => {
    void loadSvc();
    void poll();
    return () => {
      if (timer.current) window.clearInterval(timer.current);
    };
  }, [poll]);

  const onRun = async () => {
    setStarting(true);
    try {
      const s = await st.runSpeedtest();
      setStatus(s);
      void poll(); // 立即拉一次以启动自维护轮询
    } catch (e) {
      message.error(extractErr(e));
    } finally {
      setStarting(false);
    }
  };

  const onInstall = async () => {
    setInstalling(true);
    try {
      await st.installSpeedtest();
      message.success('测速组件安装完成');
      void loadSvc();
    } catch (e) {
      message.error('安装失败：' + extractErr(e));
    } finally {
      setInstalling(false);
    }
  };

  const uninstalled = svc !== null && !svc.installed;
  const running = !!status?.running;
  const r = status?.result;

  return (
    <PageCard
      breadcrumb={['应用工具', '线路测速']}
      title="线路测速"
      toolbar={
        <Space>
          {uninstalled && (
            <Tooltip title="安装 speedtest-go 后才能测速">
              <Button icon={<ThunderboltOutlined />} loading={installing} onClick={onInstall}>
                一键安装测速组件
              </Button>
            </Tooltip>
          )}
          <Button type="primary" icon={<DashboardOutlined />} loading={starting} disabled={uninstalled || running} onClick={onRun}>
            {running ? '测速中…' : '开始测速'}
          </Button>
        </Space>
      }
    >
      {uninstalled && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          message="未安装 speedtest-go，无法测速。点右上角「一键安装测速组件」（需联网）。"
        />
      )}
      <Alert
        type="warning"
        showIcon
        style={{ marginBottom: 16 }}
        message="OpenWrt 原生 speedtest-go：走 speedtest.net 自动选最近服务器测下载/上传/延迟。与爱快不同，结果在测速完成后一次性给出（非逐秒指针）；旁路由测的是经主路由的共享上行带宽。"
      />

      {running ? (
        <div style={{ textAlign: 'center', padding: '48px 0' }}>
          <Spin size="large" />
          <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>
            正在测速，约需 20–40 秒，请稍候…（开始于 {status?.started_at}）
          </Typography.Paragraph>
        </div>
      ) : r ? (
        <>
          <Row gutter={[16, 16]}>
            <Col xs={12} md={6}>
              <Card size="small">
                <Statistic title="下载" value={r.download_mbps} precision={2} suffix="Mbps" prefix={<ArrowDownOutlined style={{ color: '#52c41a' }} />} />
              </Card>
            </Col>
            <Col xs={12} md={6}>
              <Card size="small">
                <Statistic title="上传" value={r.upload_mbps} precision={2} suffix="Mbps" prefix={<ArrowUpOutlined style={{ color: '#1677ff' }} />} />
              </Card>
            </Col>
            <Col xs={12} md={6}>
              <Card size="small">
                <Statistic title="延迟" value={r.ping_ms} precision={1} suffix="ms" />
              </Card>
            </Col>
            <Col xs={12} md={6}>
              <Card size="small">
                <Statistic title="完成时间" value={status?.finished_at ?? '-'} valueStyle={{ fontSize: 16 }} />
              </Card>
            </Col>
          </Row>
          <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>
            测速服务器：{r.server || '-'}
            {r.isp ? `　运营商：${r.isp}` : ''}
          </Typography.Paragraph>
        </>
      ) : status?.error ? (
        <Alert type="error" showIcon message="测速失败" description={status.error} />
      ) : (
        <Typography.Paragraph type="secondary">点击右上角「开始测速」开始一次线路测速。</Typography.Paragraph>
      )}
    </PageCard>
  );
}
