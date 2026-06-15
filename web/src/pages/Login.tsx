import { useState, useEffect } from 'react';
import { Input, Button, Form, App } from 'antd';
import { KeyOutlined, ArrowRightOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import client, { setAPIToken, getAPIToken } from '../api/client';
import { useBranding } from '../branding/BrandingContext';
import './Login.css';

const Login: React.FC = () => {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const { branding } = useBranding();
  const [loading, setLoading] = useState<boolean>(false);

  useEffect(() => {
    if (getAPIToken()) {
      navigate('/dashboard');
    }
  }, [navigate]);

  const onFinish = async (values: { token: string }) => {
    setLoading(true);
    try {
      setAPIToken(values.token);
      const resp = await client.get('/api/v1/version');
      if (resp.status === 200) {
        message.success('连接成功，已授权登录');
        navigate('/dashboard');
      } else {
        throw new Error('鉴权未通过');
      }
    } catch {
      setAPIToken('');
      message.error('Token 校验失败，请确认守护进程是否已配置该密钥');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="kwrt-login">
      <div className="kwrt-login__glow kwrt-login__glow--cyan" />
      <div className="kwrt-login__glow kwrt-login__glow--violet" />
      <div className="kwrt-login__grid" />

      <div className="kwrt-login__card">
        <div style={{ textAlign: 'center', marginBottom: 28 }}>
          <div className="kwrt-login__badge">
            <ThunderboltOutlined style={{ fontSize: 30, color: '#22d3ee' }} />
          </div>
          <h1 className="kwrt-login__brand">{branding.app_name}</h1>
          <div className="kwrt-login__sub">{branding.app_subtitle}</div>
        </div>

        <Form name="login" onFinish={onFinish} layout="vertical" requiredMark={false}>
          <Form.Item name="token" rules={[{ required: true, message: '请输入 API 令牌密钥！' }]}>
            <Input.Password
              prefix={<KeyOutlined />}
              placeholder="API Token (Bearer 令牌)"
              size="large"
              autoFocus
            />
          </Form.Item>

          <Form.Item style={{ marginTop: 8, marginBottom: 0 }}>
            <Button
              className="kwrt-login__btn"
              type="primary"
              htmlType="submit"
              size="large"
              loading={loading}
              block
              icon={<ArrowRightOutlined />}
            >
              进入控制台
            </Button>
          </Form.Item>
        </Form>

        <div className="kwrt-login__hint">请输入网络管理守护进程配置的 API 鉴权密钥</div>
      </div>
    </div>
  );
};

export default Login;
