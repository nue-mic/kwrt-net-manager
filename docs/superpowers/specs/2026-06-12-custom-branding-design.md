# 设计文档：可自定义品牌（品牌名 + 浏览器标题）

> 日期：2026-06-12 ｜ 状态：已确认，进入实现
> 目标：让管理员在后台自定义「品牌名」与「浏览器标签标题」，服务端持久化，清浏览器缓存 / 重新登录后仍生效，且**首屏零闪**。默认值即当前硬编码值。

## 1. 需求与边界

- **两个可编辑字段**（用户确认）：
  - `app_name` —— 品牌名，默认 `FRPC`。**联动**：侧边栏品牌主标题 + 登录页品牌主标题。
  - `html_title` —— 浏览器标签 `<title>`，默认 `FRPC · 内网穿透客户端管理控制台`。
- 副标题「客户端管理面板」**保持默认、不可改**（本期不做）。
- About 页（产品介绍页）**不改**。
- **零闪**（用户确认）：首屏第一帧标题与品牌即为自定义值，不出现"默认→自定义"跳变。
- **持久化在服务端**（非 localStorage），因此清缓存 / 换浏览器 / 重登都不丢。
- 非目标（YAGNI）：Logo 图片、主题色、多语言、白标全套。

## 2. 数据与持久化

复用现有 `metaStore` 范式（原子写 + 读写锁 + 快照），存入 `<DataDir>/meta.json`：

```go
// internal/manager/meta.go
type Meta struct {
    Version      int              `json:"version"`
    AutoStart    []string         `json:"auto_start"`
    Sort         []string         `json:"sort"`
    LogViewSince map[string]int64 `json:"log_view_since,omitempty"`
    Branding     *Branding        `json:"branding,omitempty"` // 新增
}

type Branding struct {
    AppName   string `json:"app_name,omitempty"`
    HTMLTitle string `json:"html_title,omitempty"`
}
```

- 默认值用 Go 常量作**单一事实源**：
  `DefaultAppName = "FRPC"`、`DefaultHTMLTitle = "FRPC · 内网穿透客户端管理控制台"`。
- `metaStore.setBranding(b)`：加锁 → 赋值 → `flushLocked()`（沿用原子写）。
- `Manager.GetBranding()` 返回**生效值**（存了非空覆盖值就用覆盖值，否则回默认），保证调用方拿到的永远是可直接用的完整品牌。
- `Manager.SetBranding(in)`：trim；空串视为"清除→回默认"（存空，读时回默认）；长度上限 `app_name ≤ 40`、`html_title ≤ 120`（超限截断或 400，取截断更友好）。

## 3. 后端 API

新建 [internal/api/ui.go](internal/api/ui.go)，两个端点：

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/api/v1/ui/branding` | **公开** | 返回生效品牌 `{app_name, html_title}`；登录页/未登录态也能读 |
| PUT | `/api/v1/ui/branding` | **Bearer** | 入参 `{app_name?, html_title?}`，`decodeJSON` 严格解析 + 校验 + 持久化，返回生效值 |

- GET 注册到 [server.go](internal/api/server.go) **公开区**（与 `/health`、`/docs` 同级）。
- PUT 注册到 `.Bearer()` **保护组**。
- 响应/请求 JSON 用 **snake_case**（`app_name`/`html_title`），与 Snapshot/系统类一致（非 ClientConfigV1 的 camelCase 子树）。
- 同步 `openapi.yaml`（GET 标 `security: []` 公开，PUT 标 BearerAuth）与 `docs/API.zh-CN.md`。

## 4. 零闪：服务端注入 index.html

frpcmgrd 内嵌 SPA（`web/dist` 经 go:embed）。改造 index.html 的分发（含 SPA fallback）：**每次返回 index.html 时就地注入当前品牌**。

1. 读内嵌 `index.html`（很小），用当前 `Manager.GetBranding()` 做两处替换：
   - 把 `<title>…</title>` 替换为 `<title>{html.EscapeString(html_title)}</title>`。
   - 在 `</head>` 前注入：`<script>window.__FRPC_BRANDING__ = {jsonMarshal(branding)};</script>`（`json.Marshal` 默认转义 `<`,`>`,`&`，可安全内嵌 `<script>`）。
2. index.html 响应加 `Cache-Control: no-cache`（SPA 壳本就该 no-cache；静态资源 JS/CSS 仍带强缓存不变）。
3. 静态资源（hash 命名的 JS/CSS）走原有缓存路径，**只有 index.html 特殊处理**。

> 安全：品牌值仅由已鉴权管理员经 PUT 设定（非任意用户输入），且注入时做 HTML/JSON 转义，杜绝标签闭合/脚本逃逸。

## 5. 前端

- 新增 [web/src/api/branding.ts](web/src/api/branding.ts)：
  - `DEFAULT_BRANDING`（与后端常量一致，作为兜底）。
  - `readBootstrapBranding()`：同步读 `window.__FRPC_BRANDING__`，缺失则回默认 —— **零闪初值来源**。
  - `getBranding()` / `updateBranding(payload)`：axios 调 `/api/v1/ui/branding`（GET 公开、PUT 带 token）。
- 新增轻量 `BrandingProvider` + `useBranding()`（React Context）：
  - 初值同步取自 `readBootstrapBranding()`（首帧即正确，零闪）。
  - 提供 `branding` 与 `setBrandingLocal()`（保存成功后即时更新，无需刷新）。
  - 挂载时 `document.title = branding.html_title`（服务端已注入正确 title，这里只是与运行时保持同步）。
- 应用点替换硬编码：
  - 侧边栏 [MainLayout.tsx:191](web/src/components/MainLayout.tsx#L191) → `app_name`。
  - 登录页 [Login.tsx:49](web/src/pages/Login.tsx#L49) → `app_name`。
  - `document.title` ← `html_title`（Provider 内 effect）。
- 设置页 [Settings.tsx](web/src/pages/Settings.tsx)「外观」下新增「品牌」分区：
  - Antd `Form` 两字段（品牌名、浏览器标题），初值取自 `useBranding()`。
  - 保存 → `updateBranding()` → `message.success` → `setBrandingLocal()`（即时刷新侧边栏 + `document.title`，无需重登/刷新）。

## 6. 数据流

```
设置页保存 ──PUT /ui/branding(token)──▶ manager.SetBranding ──▶ meta.json(原子写)
                                              │
读取：                                         ▼
  首屏: 服务端注入 <title> + window.__FRPC_BRANDING__  ──同步──▶ React 首帧(零闪)
  设置页/运行时: GET /ui/branding(公开) 或 context  ──▶ 表单回填 / 联动刷新
```

## 7. 测试与验证

- 后端：`meta_test.go` 增 branding 持久化往返测试（写→重开→校验、空值回默认、长度校验）；`go vet ./...`、`make test`。
- API：本地 `make run` + curl 验证 GET 公开可读、PUT 带 token 可写、index.html 注入了 `<title>` 与 `window.__FRPC_BRANDING__`。
- 前端：`tsc -b` 通过、`npm run build` 成功、`npm run gen:api` 重生成 schema。
- 字段对接：严格按 [web-api-binding](.claude/skills/web-api-binding/SKILL.md) 核对 snake_case 字段名。

## 8. 改动文件清单

后端：`internal/manager/meta.go`、`internal/manager/manager.go`、`internal/manager/meta_test.go`、`internal/api/ui.go`(新)、`internal/api/server.go`、`internal/api/openapi.yaml`、`docs/API.zh-CN.md`。
前端：`web/index.html`(确认 title 占位)、`web/src/api/branding.ts`(新)、`web/src/api/types.ts`/`schema.d.ts`(gen)、`web/src/branding/BrandingContext.tsx`(新)、`web/src/main.tsx`/`App.tsx`(挂 Provider)、`web/src/components/MainLayout.tsx`、`web/src/pages/Login.tsx`、`web/src/pages/Settings.tsx`。

## 9. 发布

实现 → 本地 `go vet`/`make test`/`tsc`/`build` 全绿 + 本地 run 验证注入 → commit(`feat`) → push main → CI 自动 patch+1 发布（预计 v1.2.46）→ 设备 `fmc update` 拉新二进制验证。
