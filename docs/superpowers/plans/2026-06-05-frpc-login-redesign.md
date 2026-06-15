# FRPC 炫酷登录页 + 全站品牌区分 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把登录页重做成深色玻璃+霍光科技风，并把全站品牌从中性 "FRP Manager" 统一为 "FRPC"，与服务端 FRPS 区分。

**Architecture:** 新增 `Login.css` 承载固定深色样式与 CSS 动效，重写 `Login.tsx`（登录逻辑不变、去掉亮色主题依赖）；其余 5 处品牌文案做精确字符串替换。后台主题系统不受影响。

**Tech Stack:** React 19 + TypeScript + Vite + Ant Design 6（覆盖样式，不引动画库）。

**约束：** 验证用 `tsc -b && vite build`；不动后端/API；登录 `onFinish` 逻辑与 `message` 提示保持原样。

---

## Task 1: 登录页重写（深色玻璃 + 霍光）

**Files:**
- Create: `web/src/pages/Login.css`
- Modify: `web/src/pages/Login.tsx`（整文件重写）

- [ ] **Step 1: 新建 `web/src/pages/Login.css`**

```css
/* FRPC 登录页 —— 固定深色科技风（不随全站亮/暗主题切换） */
.frpc-login {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  background: radial-gradient(circle at 50% 0%, #131c33 0%, #0a0e1a 60%, #070a14 100%);
}

/* 缓慢浮动的青/紫光晕 */
.frpc-login__glow {
  position: absolute;
  border-radius: 50%;
  filter: blur(80px);
  opacity: 0.5;
  pointer-events: none;
}
.frpc-login__glow--cyan {
  width: 480px; height: 480px; background: #22d3ee;
  top: -120px; left: -100px;
  animation: frpcFloatA 14s ease-in-out infinite;
}
.frpc-login__glow--violet {
  width: 520px; height: 520px; background: #818cf8;
  bottom: -160px; right: -120px;
  animation: frpcFloatB 18s ease-in-out infinite;
}

/* 极淡网格纹理，向边缘渐隐 */
.frpc-login__grid {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background-image:
    linear-gradient(rgba(255,255,255,0.04) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255,255,255,0.04) 1px, transparent 1px);
  background-size: 44px 44px;
  -webkit-mask-image: radial-gradient(circle at 50% 50%, #000 0%, transparent 75%);
  mask-image: radial-gradient(circle at 50% 50%, #000 0%, transparent 75%);
}

/* 毛玻璃卡片 */
.frpc-login__card {
  position: relative;
  z-index: 2;
  width: 420px;
  padding: 40px 36px;
  border-radius: 20px;
  background: rgba(255,255,255,0.06);
  border: 1px solid rgba(255,255,255,0.12);
  -webkit-backdrop-filter: blur(20px);
  backdrop-filter: blur(20px);
  box-shadow:
    0 24px 60px rgba(0,0,0,0.5),
    inset 0 1px 0 rgba(255,255,255,0.12),
    0 0 0 1px rgba(34,211,238,0.08);
  animation: frpcCardIn 0.7s cubic-bezier(0.22, 1, 0.36, 1);
}

/* 品牌图标圈 */
.frpc-login__badge {
  width: 64px; height: 64px;
  margin: 0 auto 14px;
  border-radius: 16px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, rgba(34,211,238,0.18), rgba(129,140,248,0.18));
  border: 1px solid rgba(255,255,255,0.16);
  box-shadow: 0 0 28px rgba(34,211,238,0.25);
}

/* 品牌渐变字 */
.frpc-login__brand {
  margin: 0;
  font-size: 38px;
  font-weight: 800;
  letter-spacing: 6px;
  line-height: 1.1;
  background: linear-gradient(90deg, #22d3ee, #818cf8);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  text-shadow: 0 0 24px rgba(34,211,238,0.35);
}
.frpc-login__sub {
  margin-top: 6px;
  color: rgba(255,255,255,0.55);
  font-size: 13px;
  letter-spacing: 2px;
}
.frpc-login__hint {
  margin-top: 18px;
  text-align: center;
  color: rgba(255,255,255,0.35);
  font-size: 12px;
}

/* AntD 输入框 / 按钮在登录页内的深色覆盖 */
.frpc-login .ant-input-affix-wrapper {
  background: rgba(255,255,255,0.05);
  border-color: rgba(255,255,255,0.14);
}
.frpc-login .ant-input-affix-wrapper input {
  background: transparent;
  color: #fff;
}
.frpc-login .ant-input-affix-wrapper input::placeholder {
  color: rgba(255,255,255,0.3);
}
.frpc-login .ant-input-affix-wrapper .ant-input-prefix .anticon,
.frpc-login .ant-input-affix-wrapper .ant-input-suffix .anticon {
  color: rgba(255,255,255,0.4);
}
.frpc-login .ant-input-affix-wrapper:focus-within {
  border-color: #22d3ee;
  box-shadow: 0 0 0 2px rgba(34,211,238,0.18);
}
.frpc-login__btn {
  height: 44px;
  border: none;
  font-weight: 600;
  letter-spacing: 1px;
  background: linear-gradient(90deg, #22d3ee, #818cf8);
  box-shadow: 0 8px 24px rgba(34,211,238,0.3);
  transition: box-shadow 0.25s ease, transform 0.15s ease;
}
.frpc-login__btn:hover {
  box-shadow: 0 10px 32px rgba(129,140,248,0.5) !important;
  transform: translateY(-1px);
}

@keyframes frpcFloatA { 0%,100% { transform: translate(0,0); } 50% { transform: translate(60px,40px); } }
@keyframes frpcFloatB { 0%,100% { transform: translate(0,0); } 50% { transform: translate(-50px,-30px); } }
@keyframes frpcCardIn { from { opacity: 0; transform: translateY(24px); } to { opacity: 1; transform: translateY(0); } }
```

- [ ] **Step 2: 重写 `web/src/pages/Login.tsx`**（整文件替换为下面内容）

```tsx
import { useState, useEffect } from 'react';
import { Input, Button, Form, App } from 'antd';
import { KeyOutlined, ArrowRightOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import client, { setAPIToken, getAPIToken } from '../api/client';
import './Login.css';

const Login: React.FC = () => {
  const navigate = useNavigate();
  const { message } = App.useApp();
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
    <div className="frpc-login">
      <div className="frpc-login__glow frpc-login__glow--cyan" />
      <div className="frpc-login__glow frpc-login__glow--violet" />
      <div className="frpc-login__grid" />

      <div className="frpc-login__card">
        <div style={{ textAlign: 'center', marginBottom: 28 }}>
          <div className="frpc-login__badge">
            <ThunderboltOutlined style={{ fontSize: 30, color: '#22d3ee' }} />
          </div>
          <h1 className="frpc-login__brand">FRPC</h1>
          <div className="frpc-login__sub">客户端管理控制台</div>
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
              className="frpc-login__btn"
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

        <div className="frpc-login__hint">请输入 FRPC 守护进程配置的 API 鉴权密钥</div>
      </div>
    </div>
  );
};

export default Login;
```

- [ ] **Step 3: 类型检查 + 构建**

```bash
cd web && npx tsc -b 2>&1 | tail -5 && npm run build 2>&1 | tail -3
```
Expected: tsc 无报错；vite build 成功（`✓ built`）。

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/Login.tsx web/src/pages/Login.css
git commit -m "feat(ui): 登录页重做为深色玻璃+霍光科技风，品牌改 FRPC"
```

---

## Task 2: 全站品牌文案改 FRPC

**Files:**
- Modify: `web/index.html:7`
- Modify: `web/src/components/MainLayout.tsx:160`
- Modify: `web/src/pages/Settings.tsx:162`
- Modify: `web/src/pages/ImportExport.tsx:209`

- [ ] **Step 1: index.html 浏览器标题**

把 `web/index.html` 第 7 行：
```html
    <title>FRP Manager · 内网穿透管理面板</title>
```
改为：
```html
    <title>FRPC · 内网穿透客户端管理控制台</title>
```

- [ ] **Step 2: MainLayout 顶栏 logo**

把 `web/src/components/MainLayout.tsx` 的：
```tsx
          <Text strong style={{ color: '#fff', fontSize: 15, letterSpacing: 0.5 }}>
            FRP Manager
          </Text>
```
改为：
```tsx
          <Text strong style={{ color: '#fff', fontSize: 15, letterSpacing: 0.5 }}>
            FRPC <span style={{ fontWeight: 400, fontSize: 12, opacity: 0.5 }}>客户端</span>
          </Text>
```

- [ ] **Step 3: Settings 应用名称**

把 `web/src/pages/Settings.tsx` 中：
```tsx
                  <SafetyCertificateOutlined style={{ color: token.colorPrimary }} />
                  FRP Manager
```
改为：
```tsx
                  <SafetyCertificateOutlined style={{ color: token.colorPrimary }} />
                  FRPC
```

- [ ] **Step 4: ImportExport 文案**

把 `web/src/pages/ImportExport.tsx` 中：
```tsx
              从本地选择现有的 FRP 客户端配置文件（.toml 或 .ini）上传并导入到服务中。
```
改为：
```tsx
              从本地选择现有的 FRPC 客户端配置文件（.toml 或 .ini）上传并导入到服务中。
```

- [ ] **Step 5: 残留核对 + 构建**

```bash
cd web
echo "=== 应无中性 'FRP Manager' 残留 ==="
grep -rn "FRP Manager" src index.html || echo "无残留 ✓"
npx tsc -b 2>&1 | tail -3 && npm run build 2>&1 | tail -2
```
Expected: 无 `FRP Manager` 残留；tsc + build 通过。

- [ ] **Step 6: Commit**

```bash
git add web/index.html web/src/components/MainLayout.tsx web/src/pages/Settings.tsx web/src/pages/ImportExport.tsx
git commit -m "docs(ui): 全站品牌文案统一为 FRPC，与服务端 FRPS 区分"
```

---

## Self-Review（已核对）

- **Spec 覆盖**：登录页视觉 → Task 1（Login.css + Login.tsx，含背景/光晕/网格/玻璃卡片/渐变品牌/输入框/按钮/动效）；品牌文案 6 处 → Login 副标在 Task 1，其余 5 处在 Task 2（index.html/MainLayout/Settings/ImportExport）。全覆盖。
- **占位符**：无 TBD；Login.tsx/Login.css 给出完整代码。
- **一致性**：CSS 类名 `frpc-login__*` 与 Login.tsx className 一致；配色 `#22d3ee`/`#818cf8` 与 spec 一致；登录逻辑 `onFinish`/`message` 原样保留。
- **风险**：登录页不再用 `antdTheme.useToken()`，去掉了原 import（Typography/Space/theme/Card/SafetyCertificateOutlined），改用原生标签 + 新图标 `ThunderboltOutlined`，避免未使用 import 触发 tsc 报错。
