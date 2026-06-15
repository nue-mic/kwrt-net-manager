import type { FC, ReactNode } from 'react';
import { Breadcrumb, Card, Space, Typography, theme as antdTheme } from 'antd';

const { Text } = Typography;

interface PageCardProps {
  /** 面包屑，如 ['网络设置', 'DHCP设置', 'DHCP服务端']（最后一段高亮）。 */
  breadcrumb?: string[];
  title: string;
  /** 标题右侧（如「帮助 / 反馈」或状态文字）。 */
  extra?: ReactNode;
  /** 工具条（绿色添加、导入导出、启停删除、搜索等）。整行展示在标题下方。 */
  toolbar?: ReactNode;
  children: ReactNode;
}

/**
 * 仿爱快的页面外壳：面包屑 + 标题栏 + 工具条 + 内容卡片。
 * 让 DHCP / 静态路由各页布局一致。
 */
const PageCard: FC<PageCardProps> = ({ breadcrumb, title, extra, toolbar, children }) => {
  const { token } = antdTheme.useToken();
  return (
    <div>
      {breadcrumb && breadcrumb.length > 0 && (
        <Breadcrumb
          style={{ marginBottom: 12, fontSize: 13 }}
          items={breadcrumb.map((b, i) => ({
            title:
              i === breadcrumb.length - 1 ? (
                <Text strong style={{ color: token.colorText }}>
                  {b}
                </Text>
              ) : (
                <Text type="secondary">{b}</Text>
              ),
          }))}
        />
      )}
      <Card
        styles={{ body: { padding: 0 } }}
        style={{ boxShadow: '0 1px 2px rgba(0,0,0,0.04)' }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '14px 20px',
            borderBottom: `1px solid ${token.colorBorderSecondary}`,
          }}
        >
          <Text strong style={{ fontSize: 16 }}>
            {title}
          </Text>
          <Space size="middle">{extra}</Space>
        </div>
        {toolbar && (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              flexWrap: 'wrap',
              gap: 12,
              padding: '12px 20px',
              borderBottom: `1px solid ${token.colorBorderSecondary}`,
              background: token.colorFillQuaternary,
            }}
          >
            {toolbar}
          </div>
        )}
        <div style={{ padding: 20 }}>{children}</div>
      </Card>
    </div>
  );
};

export default PageCard;
