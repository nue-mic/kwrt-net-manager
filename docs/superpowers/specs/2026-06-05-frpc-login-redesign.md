# 设计文档：FRPC 炫酷登录页 + 全站品牌区分

> 日期：2026-06-05
> 状态：已批准
> 作者：rthink + Claude

## 1. 背景与目标

本仓库是 FRP **客户端**管理器，需与姊妹仓库 FRP **服务端**管理器（frps-manager）在 UI 上明确区分。
当前登录页与全站品牌仍用中性 "FRP Manager"。目标：
1. 把登录页重做成**炫酷、高级、科技风**（深色玻璃 + 霍光）。
2. 全站品牌文案统一为 **FRPC**（客户端），与 FRPS 区分。

## 2. 登录页视觉设计（`web/src/pages/Login.tsx` 重写，登录逻辑不变）

**固定深色科技风**：不依赖 AntD 亮/暗主题，登录页自带深色配色；后台进入后仍跟随原主题切换，互不影响。

配色变量（集中定义，便于微调）：
- 背景底：`#0a0e1a → #0d1424` 深蓝黑渐变
- 霍光青：`#22d3ee`
- 霍光紫：`#818cf8`
- 玻璃卡片底：`rgba(255,255,255,0.06)`，边框 `rgba(255,255,255,0.12)`

视觉层次：
- **背景**：深蓝黑渐变 + 2~3 个缓慢浮动的青/紫 `radial-gradient` 光晕（CSS `@keyframes` 飘移）+ 极淡网格纹理叠加。
- **卡片**：毛玻璃 `backdrop-filter: blur(20px)`（含 `-webkit-` 前缀）+ 半透明底 + 1px 霍光边框 + 内发光 `box-shadow`；圆角 20；入场淡入上浮动效。
- **品牌区**：发光图标（连接/盾牌）→ **FRPC** 大字（青→紫渐变文字 + 轻微辉光）→ 中文副标"客户端管理控制台"。
- **输入框**：深色玻璃底，聚焦时青色辉光边框（`box-shadow` 过渡）。
- **按钮**：青→紫渐变 + hover 辉光扩散 + 箭头图标，文案"进入控制台 →"。
- **微动效**：光晕浮动、卡片入场淡入上浮、按钮 hover 发光。纯 CSS `@keyframes`，不引第三方动画库。

技术约束：
- 保留 AntD `Form` / `Input.Password` / `Button`，仅覆盖视觉；`onFinish` token 校验逻辑、`message` 提示**完全不动**。
- 动效样式新增到 `web/src/pages/Login.css`，由 `Login.tsx` import。
- 登录页**不调用** `antdTheme.useToken()` 的亮色值（避免被主题影响）。

## 3. 全站品牌文案改造（共 6 处，与 FRPS 区分）

| 文件 | 现状 | 改为 |
|---|---|---|
| `web/index.html` `<title>` | `FRP Manager · 内网穿透管理面板` | `FRPC · 内网穿透客户端管理控制台` |
| `web/src/components/MainLayout.tsx:160` | `FRP Manager` | `FRPC`（顶栏 logo，配"客户端"小标识）|
| `web/src/pages/Login.tsx` 标题/副标 | `FRP 控制台登录` / `请输入 FRP Manager 守护进程…` | 品牌区 `FRPC` + `请输入 FRPC 守护进程配置的 API 鉴权密钥…` |
| `web/src/pages/Settings.tsx:162` | `FRP Manager` | `FRPC` |
| `web/src/pages/ImportExport.tsx:209` | `FRP 客户端配置文件` | `FRPC 客户端配置文件` |

`MainLayout` 中已有的 `'FRPC 实例'` 保持不变。

## 4. 范围之外（YAGNI）
- 不改后台其它页面的视觉风格（仅改品牌文案）。
- 不引玻璃/动画组件库。
- 不动后端、不动 API。

## 5. 验证
- 前端 `tsc -b && vite build` 通过。
- 本地 `npm run dev` 目测：登录页深色玻璃霍光效果、动效流畅、品牌为 FRPC；后台主题切换仍正常。
- 残留核对：`web/src` 与 `index.html` 中不再有中性 `FRP Manager`（除 docs/历史）。
