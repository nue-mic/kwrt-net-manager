import { useState, useEffect } from 'react';
import {
  Card,
  Space,
  Typography,
  Form,
  Button,
  Switch,
  Divider,
  Descriptions,
  Tag,
  App,
  Row,
  Col,
  Alert,
  Input,
  Select,
  Tooltip,
  theme as antdTheme,
} from 'antd';
import {
  UserOutlined,
  SettingOutlined,
  KeyOutlined,
  TagsOutlined,
  ControlOutlined,
} from '@ant-design/icons';
import { clearAPIToken, getAPIToken } from '../api/client';
import { useTheme } from '../theme/ThemeContext';
import { useBranding } from '../branding/BrandingContext';
import { updateBranding } from '../api/branding';
import { getSysConfig, updateSysConfig, type SysConfigResp, type SysConfigPatch } from '../api/syscfg';

const { Title, Text } = Typography;

const Settings: React.FC = () => {
  const { token } = antdTheme.useToken();
  const { message, modal } = App.useApp();
  const { mode, setMode, resolved } = useTheme();
  const { branding, setBrandingLocal } = useBranding();

  const [brandForm] = Form.useForm();
  const [savingBrand, setSavingBrand] = useState(false);
  // 品牌可能在挂载后由公开 GET 异步校正，故用 effect 同步表单回填。
  useEffect(() => {
    brandForm.setFieldsValue({
      app_name: branding.app_name,
      app_subtitle: branding.app_subtitle,
      html_title: branding.html_title,
    });
  }, [branding, brandForm]);

  const onSaveBranding = async (vals: {
    app_name?: string;
    app_subtitle?: string;
    html_title?: string;
  }) => {
    setSavingBrand(true);
    try {
      // 空串显式发给后端 → 重置为默认；后端返回生效值。
      const next = await updateBranding({
        app_name: vals.app_name ?? '',
        app_subtitle: vals.app_subtitle ?? '',
        html_title: vals.html_title ?? '',
      });
      setBrandingLocal(next); // 即时刷新侧边栏 / 登录页 / 浏览器标题
      brandForm.setFieldsValue(next);
      message.success('品牌已保存，已即时生效');
    } catch {
      message.error('保存失败，请检查登录令牌与网络');
    } finally {
      setSavingBrand(false);
    }
  };

  const [autoCollapse, setAutoCollapse] = useState<boolean>(
    () => localStorage.getItem('kwrtnet_sidebar_collapse') === '1'
  );
  const tokenMasked = (() => {
    const t = getAPIToken();
    if (!t) return '未保存';
    if (t.length <= 8) return '****';
    return `${t.slice(0, 4)}…${t.slice(-4)}`;
  })();

  const onChangeToken = () => {
    modal.confirm({
      title: '更换 API Token？',
      content: '这会清除当前保存的 Token 并跳转回登录页，请确保新的 Token 已准备好。',
      okText: '我已准备好',
      cancelText: '取消',
      onOk: () => {
        clearAPIToken();
        message.success('已清除本地 Token');
        window.location.href = '/login';
      },
    });
  };

  const onToggleSidebar = (v: boolean) => {
    setAutoCollapse(v);
    localStorage.setItem('kwrtnet_sidebar_collapse', v ? '1' : '0');
    message.success('已保存，下次刷新生效');
  };

  // ---- 系统运行时配置（env 默认 ∪ meta.json 覆盖，免重启生效）----
  type SysField = 'log_level' | 'self_update_enabled' | 'docs_enabled' | 'cors_origins';
  const [sysForm] = Form.useForm();
  const [sysCfg, setSysCfg] = useState<SysConfigResp | null>(null);
  const [savingSys, setSavingSys] = useState(false);
  // 每个字段是否「跟随 env」：true = 不覆盖、随环境变量；false = 固定为下方 UI 值。
  const [follow, setFollow] = useState<Record<SysField, boolean>>({
    log_level: true,
    self_update_enabled: true,
    docs_enabled: true,
    cors_origins: true,
  });

  // 用一份服务端响应回填表单与「跟随 env」开关，保证显示=服务端真实生效值。
  const fillSysForm = (r: SysConfigResp) => {
    sysForm.setFieldsValue({
      log_level: r.effective.log_level,
      self_update_enabled: r.effective.self_update_enabled,
      docs_enabled: r.effective.docs_enabled,
      cors_origins: (r.effective.cors_origins || []).join(', '),
    });
    setFollow({
      log_level: !r.overridden.log_level,
      self_update_enabled: !r.overridden.self_update_enabled,
      docs_enabled: !r.overridden.docs_enabled,
      cors_origins: !r.overridden.cors_origins,
    });
  };

  // 挂载时拉取一次；setState 落在 then 回调（异步）里，未登录/网络问题静默（卡片不渲染）。
  useEffect(() => {
    getSysConfig()
      .then((r) => {
        setSysCfg(r);
        fillSysForm(r);
      })
      .catch(() => undefined);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 切换某字段「跟随 env」：打开跟随时把输入框回显为 env 默认，便于看清将生效什么。
  const toggleFollow = (k: SysField, v: boolean) => {
    setFollow((prev) => ({ ...prev, [k]: v }));
    if (v && sysCfg) {
      const env = sysCfg.env_default;
      sysForm.setFieldsValue({
        [k]: k === 'cors_origins' ? (env.cors_origins || []).join(', ') : env[k],
      });
    }
  };

  const onSaveSysCfg = async (vals: {
    log_level: string;
    self_update_enabled: boolean;
    docs_enabled: boolean;
    cors_origins: string;
  }) => {
    // 跟随 env 的字段进 reset[]（清除覆盖）；其余作为覆盖下发。这样逐项「跟随 env」
    // 真正生效，且空 CORS 不会被悄悄兜底成 *（与后端「非空校验」语义一致）。
    const patch: SysConfigPatch = { reset: [] };
    if (follow.log_level) patch.reset!.push('log_level');
    else patch.log_level = vals.log_level;
    if (follow.self_update_enabled) patch.reset!.push('self_update_enabled');
    else patch.self_update_enabled = vals.self_update_enabled;
    if (follow.docs_enabled) patch.reset!.push('docs_enabled');
    else patch.docs_enabled = vals.docs_enabled;
    if (follow.cors_origins) {
      patch.reset!.push('cors_origins');
    } else {
      const origins = vals.cors_origins
        .split(/[,\n]/)
        .map((s) => s.trim())
        .filter(Boolean);
      if (origins.length === 0) {
        message.error('CORS 白名单不能为空：填具体来源、填 * 放行全部，或切到「跟随 env」');
        return;
      }
      patch.cors_origins = origins;
    }
    setSavingSys(true);
    try {
      const r = await updateSysConfig(patch);
      setSysCfg(r);
      fillSysForm(r);
      message.success('系统配置已保存，已即时生效（无需重启）');
    } catch (e) {
      const err = e as { response?: { data?: { error?: { message?: string } } } };
      message.error('保存失败：' + (err.response?.data?.error?.message || '请检查输入与网络'));
    } finally {
      setSavingSys(false);
    }
  };

  const onResetSysCfg = async () => {
    setSavingSys(true);
    try {
      const r = await updateSysConfig({ reset: ['log_level', 'self_update_enabled', 'docs_enabled', 'cors_origins'] });
      setSysCfg(r);
      fillSysForm(r);
      message.success('已恢复为环境变量默认值');
    } catch {
      message.error('恢复失败');
    } finally {
      setSavingSys(false);
    }
  };

  // 字段标签 + 「跟随 env」开关：开=跟随环境变量(灰显输入)，关=用下方 UI 值覆盖。
  const sysLabel = (k: SysField, text: string) => (
    <Space size={6}>
      {text}
      <Tooltip title="开：跟随 KWRTNET_* 环境变量（保存即清除该项覆盖）；关：固定为下方填写的值">
        <Switch
          size="small"
          checkedChildren="跟随env"
          unCheckedChildren="自定义"
          checked={follow[k]}
          onChange={(v) => toggleFollow(k, v)}
        />
      </Tooltip>
    </Space>
  );

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card styles={{ body: { padding: 18 } }} style={{ borderRadius: 10 }}>
        <Space direction="vertical" size={4}>
          <Title level={4} style={{ margin: 0 }}>
            <SettingOutlined /> 设置
          </Title>
          <Text type="secondary" style={{ fontSize: 13 }}>
            个性化、账户和版本信息。外观与账户偏好保存在浏览器本地；品牌保存在服务端，跨设备 / 清缓存后依然生效。
          </Text>
        </Space>
      </Card>

      <Row gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card title={<Space><UserOutlined /> 账户</Space>} styles={{ body: { padding: 18 } }} style={{ borderRadius: 10 }}>
            <Descriptions column={1} size="small" labelStyle={{ width: 100 }}>
              <Descriptions.Item label="鉴权方式">
                <Tag color="processing" icon={<KeyOutlined />}>Bearer Token</Tag>
              </Descriptions.Item>
              <Descriptions.Item label="当前 Token">
                <Text code>{tokenMasked}</Text>
              </Descriptions.Item>
            </Descriptions>
            <Divider style={{ margin: '16px 0' }} />
            <Space>
              <Button danger onClick={onChangeToken}>更换 / 清除 Token</Button>
            </Space>
            <Alert
              type="warning"
              showIcon
              style={{ marginTop: 16, borderRadius: 8 }}
              message="安全提示"
              description={
                <Text style={{ fontSize: 12 }}>
                  Token 被存放在浏览器 localStorage 中，存在被 XSS 读取的风险。生产环境建议结合反向代理 IP 白名单 / Basic Auth 一并加固。
                </Text>
              }
            />
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card title={<Space><SettingOutlined /> 外观与交互</Space>} styles={{ body: { padding: 18 } }} style={{ borderRadius: 10 }}>
            <Form layout="horizontal" labelCol={{ span: 8 }} wrapperCol={{ span: 16 }}>
              <Form.Item label="主题模式">
                <Space>
                  <Switch
                    checkedChildren="跟随系统"
                    unCheckedChildren="手动"
                    checked={mode === 'system'}
                    onChange={(v) => setMode(v ? 'system' : resolved)}
                  />
                  {mode !== 'system' && (
                    <Switch
                      checkedChildren="深色"
                      unCheckedChildren="浅色"
                      checked={mode === 'dark'}
                      onChange={(v) => setMode(v ? 'dark' : 'light')}
                    />
                  )}
                  <Tag bordered={false}>当前：{resolved === 'dark' ? '深色' : '浅色'}</Tag>
                </Space>
              </Form.Item>
              <Form.Item label="侧边栏默认折叠">
                <Switch checked={autoCollapse} onChange={onToggleSidebar} />
              </Form.Item>
              <Form.Item label="主色">
                <Text code style={{ background: token.colorPrimary, color: '#fff', padding: '2px 8px' }}>
                  {token.colorPrimary}
                </Text>
              </Form.Item>
            </Form>
          </Card>
        </Col>

      </Row>

      <Card
        title={<Space><TagsOutlined /> 品牌 / 标识</Space>}
        styles={{ body: { padding: 18 } }}
        style={{ borderRadius: 10 }}
      >
        <Form
          form={brandForm}
          layout="vertical"
          onFinish={onSaveBranding}
          style={{ maxWidth: 560 }}
        >
          <Form.Item
            label="品牌名"
            name="app_name"
            extra="显示在侧边栏与登录页顶部。留空恢复默认「KWRT 网络管理」。"
          >
            <Input placeholder="KWRT 网络管理" maxLength={40} allowClear />
          </Form.Item>
          <Form.Item
            label="副标题"
            name="app_subtitle"
            extra="品牌名下方的小字（侧边栏与登录页）。留空恢复默认「DHCP · 静态路由」。"
          >
            <Input placeholder="DHCP · 静态路由" maxLength={60} allowClear />
          </Form.Item>
          <Form.Item
            label="浏览器标签标题"
            name="html_title"
            extra="浏览器标签页 title。留空恢复默认。"
          >
            <Input placeholder="KWRT 网络管理 · DHCP / 静态路由控制台" maxLength={120} allowClear />
          </Form.Item>
          <Form.Item style={{ marginBottom: 0 }}>
            <Button type="primary" htmlType="submit" loading={savingBrand}>
              保存品牌
            </Button>
          </Form.Item>
        </Form>
        <Alert
          type="info"
          showIcon
          style={{ marginTop: 16, borderRadius: 8 }}
          message="保存在服务端"
          description={
            <Text style={{ fontSize: 12 }}>
              品牌保存在守护进程数据目录（meta.json），清浏览器缓存、换浏览器或重新登录后依然生效；首屏由服务端注入，零闪烁。
            </Text>
          }
        />
      </Card>

      {sysCfg && (
        <Card
          title={<Space><ControlOutlined /> 系统 / 运行（免重启即时生效）</Space>}
          styles={{ body: { padding: 18 } }}
          style={{ borderRadius: 10 }}
        >
          <Form
            form={sysForm}
            layout="horizontal"
            labelCol={{ span: 7 }}
            wrapperCol={{ span: 17 }}
            onFinish={onSaveSysCfg}
            style={{ maxWidth: 640 }}
          >
            <Form.Item label={sysLabel('log_level', '日志级别')} name="log_level">
              <Select
                disabled={follow.log_level}
                options={[
                  { value: 'trace', label: 'trace（最详细，等同 debug）' },
                  { value: 'debug', label: 'debug（排查问题）' },
                  { value: 'info', label: 'info（默认）' },
                  { value: 'warn', label: 'warn' },
                  { value: 'error', label: 'error（最少）' },
                ]}
              />
            </Form.Item>
            <Form.Item
              label={sysLabel('self_update_enabled', 'Web 自更新')}
              name="self_update_enabled"
              valuePropName="checked"
              extra="关闭后「关于」页的一键更新被禁用"
            >
              <Switch disabled={follow.self_update_enabled} checkedChildren="开" unCheckedChildren="关" />
            </Form.Item>
            <Form.Item
              label={sysLabel('docs_enabled', 'API 文档 /api/docs')}
              name="docs_enabled"
              valuePropName="checked"
              extra="关闭后 /api/docs 返回 404"
            >
              <Switch disabled={follow.docs_enabled} checkedChildren="开" unCheckedChildren="关" />
            </Form.Item>
            <Form.Item
              label={sysLabel('cors_origins', 'CORS 白名单')}
              name="cors_origins"
              extra="逗号或换行分隔；填 * 放行全部跨域（含 * 时整列按通配处理）。改动对后续请求/新 WS 连接即时生效。"
            >
              <Input.TextArea disabled={follow.cors_origins} autoSize={{ minRows: 1, maxRows: 4 }} placeholder="*" />
            </Form.Item>
            <Form.Item wrapperCol={{ offset: 7 }} style={{ marginBottom: 0 }}>
              <Space>
                <Button type="primary" htmlType="submit" loading={savingSys}>保存系统配置</Button>
                <Tooltip title="清除全部 UI 覆盖，回退到 KWRTNET_* 环境变量默认值">
                  <Button onClick={onResetSysCfg} loading={savingSys}>恢复 env 默认</Button>
                </Tooltip>
              </Space>
            </Form.Item>
          </Form>
          <Alert
            type="info"
            showIcon
            style={{ marginTop: 16, borderRadius: 8 }}
            message="env 默认 ∪ UI 覆盖（存 meta.json，跨重启保留、随备份迁移）"
            description={
              <Text style={{ fontSize: 12 }}>
                这些原本是 KWRTNET_* 环境变量（装机写死、改了要 SSH + 重启）。每行的「跟随 env」开关：
                开=该项不覆盖、跟随环境变量（保存即清除覆盖）；关=固定为你填写的值。日志级别/自更新/文档/CORS
                改动均**无需重启**即时生效；「恢复 env 默认」会一次性清除全部覆盖。
              </Text>
            }
          />
        </Card>
      )}
    </Space>
  );
};

export default Settings;
