# 拨号诊断面板 — 设计文档

> 日期：2026-06-26
> 范围：前端为主 + 后端日志查询小改
> 状态：已与用户确认设计，待 review → 转实现计划

---

## 1. 背景与问题

外网（WAN）接口编辑在 [NetOverview.tsx](../../../web/src/pages/NetOverview.tsx) 的右侧抽屉 `IfaceDrawer` 中完成。当前「查看拨号日志」交互存在两个硬伤：

1. **保存即丢失日志入口**：`onSave` 成功后回调 `onSaved()` 会立即 `setDrawer(open:false)` 关闭整个抽屉。而拨号日志 Modal 定义在 `IfaceDrawer` **内部**，抽屉 `destroyOnClose`，一关抽屉 Modal 连带被销毁。
   - 后果：PPPoE「保存后自动拨号」（见 `protoHint`），但**恰恰在最该看拨号过程/失败原因的时刻，日志入口随抽屉一起消失了**。要看日志只能重新「点卡片 → 进抽屉 → 点查看日志」。

2. **体验过于简陋**：日志只是一个终端式滚动框（[DialLogConsole.tsx](../../../web/src/components/DialLogConsole.tsx)），藏在「卡片 → 抽屉 → Modal」三层之下，路径深、不直观。

## 2. 参考：爱快 iKuai 的拨号体验

来自 `docs/爱快系统截图/`：

- **外网设置编辑**（`外网设置-绑定网卡.png`）：整页表单，**状态行 = 「已连接 [断开] [重拨]」**，编辑页内不嵌日志。
- **拨号日志**（`日志中心系统/功能日志-外网拨号日志.png`）：日志中心里的**独立完整页面**——顶部「全部线路下拉 + 开始/结束时间 + 日志详情搜索 + 导出 + 全部清空」，下面「时间 | 线路 | 日志详情」表格 + 分页（共 5000 条 / 250 页）。

**关键判断**：爱快的拨号日志只是**原始 pppd 文本列表**（`local IP address`、`PAP authentication succeeded` 等英文行），用户看不懂"拨到哪一步、卡在哪"。本项目后端 [dialdiag.go](../../../internal/logcenter/dialdiag.go) **已把每条日志解析成 `phase`（发现/认证/获址/建链/断开）+ 人话 `diagnosis` + 修复 `advice`**，内置 pppd 全生命周期诊断表。我们可以把它**可视化**，做出比爱快更专业的诊断体验。

## 3. 目标 / 非目标

**目标**
- 保存 PPPoE / 点重拨后，立刻、稳定地看到完整拨号过程与结果（成功/失败原因），入口不再随抽屉消失。
- 把抽象的 pppd 日志可视化为「阶段进度 + 人话诊断 + 关键信息」的专业诊断面板。
- 历史拨号日志页对齐爱快（补线路筛选）。

**非目标（YAGNI）**
- 不改造外网编辑表单本身的字段/布局（仅升级"运行状态"那一行）。
- 不做 DHCP/静态接口的拨号面板（只有 PPPoE 有真正的多阶段拨号过程）。
- 不做定时重拨 / 异常 IP 检测 / 线路质量监测等爱快附加功能（本期不需要）。
- 不引入图表库画速率曲线（信息卡用文本即可）。

## 4. 已确认的设计决策

| 决策点 | 结论 |
|---|---|
| 保存后行为 | 关抽屉后弹**独立**诊断面板（不再留在抽屉内） |
| 自动弹触发范围 | **仅 PPPoE**，且**账号 / 密码 / 接入方式变动**时（或新建即为 pppoe） |
| 重拨按钮 | 点重拨后**复用同一诊断面板** |
| 面板形态 | **右侧大抽屉 Drawer**（~720px） |
| 本期范围 | **三项全做**：① 拨号诊断面板 ② 外网状态行升级 ③ 日志中心线路筛选 |
| 保存失败 | **不弹**面板，维持留在编辑抽屉报错 |

## 5. 架构与组件拆分

数据逻辑与展示分离，按职责拆成小单元：

```
NetOverview (顶层)
├─ state: dialPanel: { iface: string; name: string } | null   ← 提升到顶层，跨抽屉存活
├─ IfaceDrawer (编辑抽屉)
│    ├─ onSave: 计算 shouldDial → onSaved(saved, { dial })
│    ├─ 状态行升级：已连接● + 当前IP + [断开][重拨][拨号诊断]
│    └─ 删除内部的拨号 Modal（移到顶层）
└─ DialDiagnosticDrawer (新, 右侧 Drawer)  ← 顶层渲染，由 dialPanel 驱动
     ├─ useDialStream(iface)  (新 hook, 数据层)
     ├─ DialStageSteps        (新, 阶段进度条)
     ├─ DialBanner            (诊断横幅, 从 DialLogConsole 抽出)
     ├─ DialInfoCard          (新, 成功信息卡)
     └─ DialLogStream         (终端式日志区, 从 DialLogConsole 抽出)
```

**`useDialStream(iface)` hook（数据层，承载现 DialLogConsole 的全部副作用）**
- 管理 WebSocket 生命周期 + 指数退避重连（搬运现有逻辑）。
- `pendingRef` 收帧缓冲 + 180ms 批量 flush（搬运，避免高频帧打满主线程）。
- 返回：`{ lines, banner, conn, stage, info, clear }`
  - `lines: LogEntry[]`（封顶 MAX_LINES=1000）
  - `banner: DialDiagnosis | null`（成功/失败/进行中结论）
  - `conn: 'connecting'|'open'|'closed'`
  - `stage: { step: number; status: 'process'|'finish'|'error' }`（阶段进度，见 §6）
  - `info: { localIp?; gateway?; dnsPrimary?; dnsSecondary? }`（从日志提取，见 §7）
- 进入时先 `dialDiagnose(iface)` 取一次结论给 banner 即时内容。

> 重构原则：现有 [DialLogConsole.tsx](../../../web/src/components/DialLogConsole.tsx) 的副作用（WS、flush、横幅推算）迁入 `useDialStream`；其纯展示部分拆成 `DialBanner` + `DialLogStream`。`DialDiagnosticDrawer` 组合这些子单元，新增 `DialStageSteps` + `DialInfoCard`。这样每个单元职责单一、可独立理解。

## 6. 阶段进度条（DialStageSteps）

四步固定流程（PPPoE）：**发现 → 认证 → 获址 → 已连接**。由 `useDialStream` 依据日志流的 `phase` + `severity` + `dial_state` 推进。

**phase → step 序号映射**

| 后端 phase | 含义 | step |
|---|---|---|
| `other`（pppd started） | 拨号启动 | 0（发现，process） |
| `discovery` | PADI/PADO/PADS | 0（发现） |
| `auth` | PAP/CHAP | 1（认证） |
| `established`（Connect: ppp） | 链路建立、即将获址 | 2（获址，process） |
| `ipcp` | IPCP 协商 IP | 2（获址） |
| `teardown` | 掉线/拆链 | 当前 step 标 error |

**推进规则**（在 flush 时对每条 entry 累积，单调前进）
- `reachedStep = max(reachedStep, stepOf(phase))`，已到达步之前的全部置 `finish`、当前步置 `process`。
- `dial_state === 'connected'`（成功，命中 `local IP address`）→ `reachedStep = 3`，全部步 `finish`（绿勾）。
- `severity === 'error'` → 当前 `reachedStep` 步置 `error`（红），停止前进；横幅展示失败诊断与建议。
- `phase === 'teardown'`（warning，如对端踢线）→ 当前步置 error 提示断开。

**渲染**：Ant Design `Steps`（size small）；当前步 `process` 带转圈图标，`finish` 绿勾，`error` 红叉。每步下方一行小字状态（如"✓收到 PADO"、"✓PAP 通过"、"拨号中…"、"待建立"）。

## 7. 结果信息卡（DialInfoCard）

拨号成功后展示关键网络参数。来源：从实时日志流 `message` 正则提取（爱快截图中这些行真实存在），回退接口运行态 `runtime_ip`。

| 字段 | 正则（大小写不敏感，容忍多空格） |
|---|---|
| 本端 IP | `local\s+IP address\s+(\d{1,3}(?:\.\d{1,3}){3})` |
| 网关 | `remote IP address\s+(\d{1,3}(?:\.\d{1,3}){3})` |
| 主 DNS | `primary DNS address\s+(\S+)` |
| 备 DNS | `secondary DNS address\s+(\S+)` |

仅当 `info` 至少含 `localIp` 时渲染信息卡；否则隐藏（拨号中/失败不显示）。

## 8. 触发判定（IfaceDrawer.onSave）

保存**成功后**计算 `shouldDial`，决定是否打开诊断面板：

```
finalProto = body.proto            // 'pppoe' | 'dhcp' | 'static'
isPppoe    = finalProto === 'pppoe'

shouldDial =
  isPppoe && (
    !editing ||                              // 新建即为 pppoe
    editing.proto !== 'pppoe' ||             // 接入方式切到 pppoe
    body.username !== editing.username ||     // 账号变
    body.password !== editing.password        // 密码变
  )
```

- 仅改 MTU / 备注 / 高级项等不影响拨号的字段 → `shouldDial = false`，不弹。
- 从 pppoe 切到 dhcp/static（`finalProto !== 'pppoe'`）→ 不弹。
- 用 `createNetIface`/`updateNetIface` 返回的 `NetIface.id` 作为面板的 `iface`（两者均返回含 id 的对象，已确认）。

`onSaved` 签名扩展为 `onSaved(saved: NetIface, opts: { dial: boolean })`；NetOverview 据 `opts.dial` 在关抽屉后 `setDialPanel({ iface: saved.id, name: saved.name })`。

**重拨**：`onAction(id, 'restart')` 成功后同样 `setDialPanel({ iface, name })` 打开面板（编辑抽屉可保持打开，诊断 Drawer 叠加于其上）。

## 9. 外网编辑状态行升级

现「运行状态」一行（[NetOverview.tsx](../../../web/src/pages/NetOverview.tsx) 第 692-704 行附近）升级为对齐爱快：

```
状态：  ● 已连接    当前IP 119.112.135.194    [断开] [重拨] [拨号诊断]
```

- 状态徽标复用 `ConnBadge`（已连接/拨号中/未连接三态）。
- 「断开」「重拨」沿用现有 `onAction`。
- 「拨号诊断」按钮替换原「查看拨号日志」，点击 `setDialPanel({ iface: editing.id, name })` 打开同一诊断 Drawer（编辑态随时查看）。

## 10. 日志中心线路筛选（对齐爱快）

历史拨号日志页 [LogCenter.tsx](../../../web/src/pages/logcenter/LogCenter.tsx) 的 `dialup` 源补「线路」下拉。

**后端改动**
- `logcenter.Filter` 增 `Iface string`。
- [logs.go](../../../internal/api/logs.go) `filter()` 解析 `q.Get("iface")`。
- `logcenter.Center.Query` / `Export` 在 `dialup` 源对 entries 按 `strings.Contains(e.Iface, iface)` 过滤（`Diagnose` 已有同款先例）。
- 仅 `dialup` 源支持 iface；其它源忽略该参数。

**前端改动**
- `LogQuery` 增 `iface?: string`；`queryLogs`/`exportLogsURL` 透传。
- `LogCenter` 在 `source==='dialup'` 时渲染「线路」下拉（默认「全部线路」=不传 iface）。选项复用 `net.getNetOverview()` 返回的 `wans`（含 id/name，**无需新增 API**），下拉值用接口 name（如 `wan`）；后端以 `strings.Contains(e.Iface, iface)` 匹配，逻辑名 `wan` 可命中设备名 `pppoe-wan`。
- 切换线路重置分页到第 1 页。

## 11. 数据流

```
保存 PPPoE / 点重拨
    │
    ▼
NetOverview.setDialPanel({ iface, name })
    │
    ▼
DialDiagnosticDrawer 打开
    │  useDialStream(iface):
    │    GET /api/v1/logs/dialup/diagnose?iface  ── 即时横幅
    │    WS  /api/v1/logs/dialup/stream?iface    ── 实时帧
    ▼
每帧 → flush(180ms) → 累积 lines / 推进 stage / 提取 info / 重算 banner
    │
    ▼
DialStageSteps + DialBanner + DialInfoCard + DialLogStream 渲染
    │
    ▼
用户看到「拨到哪步 / 成功(IP+DNS) / 失败原因+建议」→ 自行关闭
```

## 12. 文件清单

**新增**
- `web/src/components/dial/DialDiagnosticDrawer.tsx` — 右侧诊断 Drawer 容器
- `web/src/components/dial/useDialStream.ts` — 数据层 hook（WS + flush + stage + info）
- `web/src/components/dial/DialStageSteps.tsx` — 阶段进度条
- `web/src/components/dial/DialInfoCard.tsx` — 成功信息卡
- `web/src/components/dial/DialLogStream.tsx` — 终端式日志区（从 DialLogConsole 抽出）
- `web/src/components/dial/DialBanner.tsx` — 诊断横幅（从 DialLogConsole 抽出）

**修改**
- `web/src/pages/NetOverview.tsx` — 顶层 dialPanel 状态、触发判定、状态行升级、删内部 Modal
- `web/src/pages/logcenter/LogCenter.tsx` — dialup 线路下拉
- `web/src/api/logs.ts` — `LogQuery.iface`
- `internal/logcenter/*.go`（Filter + Query/Export 过滤）、`internal/api/logs.go`（解析 iface）

**移除/替代**
- `web/src/components/DialLogConsole.tsx` — 退役并删除：副作用逻辑迁入 `useDialStream`、展示拆入 `DialBanner`/`DialLogStream`；`NetOverview` 改引用新 `DialDiagnosticDrawer`，不再引用 `DialLogConsole`。

## 13. 测试要点

- **前端 `tsc -b`** 通过；`useDialStream` 的阶段推进/信息提取为纯函数，便于单测（喂一组模拟 LogEntry 序列，断言 stage/info）。
- **后端**：`logcenter` 过滤新增 `Iface` 的单测（dialup 按线路过滤命中/落空）；`go test ./...` + `go vet` 全绿。
- **手验**（store 后端可跑通 UI 骨架；真机 optest 验真实拨号流）：
  1. 新建 PPPoE 保存 → 诊断 Drawer 自动弹、阶段推进、成功显 IP/DNS。
  2. 改 MTU 保存 → **不**弹。
  3. 错误账号保存 → 认证步标红 + 失败诊断 + 建议。
  4. 点重拨 → 复用面板。
  5. 日志中心拨号页切线路 → 仅显示该线路。

## 14. 风险与缓解

- **两个右侧 Drawer 叠加**（重拨时编辑抽屉 + 诊断抽屉）：Ant Design 支持多层 Drawer，后开者 z-index 更高；关诊断回到编辑。可接受。
- **store / 非 OpenWrt 后端无真实拨号流**：`diagnose` 返回 `unknown`、WS 无帧 → 面板显示"暂无拨号记录/等待中"，骨架仍可渲染。阶段/信息卡逻辑用单测覆盖，不依赖真机。
- **信息卡正则依赖 pppd 文案**：以真实 pppd 2.4.x/2.5.x 文案为准（爱快截图已验证存在），提取失败时回退 `runtime_ip`，不报错。
