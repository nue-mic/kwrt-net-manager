# 线路测速增强：多节点挨个测 + 历史趋势（设计）

> 2026-06-20 · 状态：已认可，进入实现。仿爱快「应用工具 > 线路测速」的增强。

## 1. 目标与范围

把现有「一次性自动选最近节点测一下」升级为：

- **多 speedtest.net 节点挨个测**：列出附近节点，逐个测，出对比表。
- **智能默认 + 可手调**：自动预勾选「最近 + 多运营商」一组节点，用户可增删。
- **历史表 + 趋势图**：每次结果落旁车，页面下方看历史与下载/上传趋势。
- **全自动**：含安装探测——未装 `speedtest-go` 时自动先装再测，无需单独手点安装。

不做（YAGNI / 后续）：iperf3 / CDN HTTP / LibreSpeed 等其它测速途径；逐秒实时仪表盘指针；多线路指定出接口（`--source` 预留）。

## 2. 真机实证（ImmortalWrt，speedtest-go v1.7.8）

- flag 支持：`-l/--list`、`-s/--server=ID...`、`--json`、`--search`、`--source`、`--no-download/--no-upload`。
- `--list` 文本（`--list --json` 无效，仍输出文本）：
  ```
  ✓ ISP: 119.112.135.194 (China Unicom) [38.93,121.62]
  [43752]    461.09km 20ms 	Beijing (China) by BJ Unicom
  [ 5396]    855.62km 78ms 	Suzhou (China) by China Telecom JiangSu 5G
  [59386]    973.79km Timeout 	HangZhou (China) by 浙江电信
  ```
  → 解析：`[<id>] <dist>km <ping|Timeout> <城市(国家)> by <sponsor>`；id 可有前导空格；自带延迟探测，`Timeout`=不可达。
- `--server <id> --json` 结构（单位关键）：
  ```json
  {"user_info":{"Isp":"China Unicom"},
   "servers":[{"id":"43752","name":"Beijing","sponsor":"BJ Unicom","distance":0,
     "latency":21552729,"jitter":2215026,            // 纳秒 → ÷1e6=ms
     "dl_speed":29699216.34,"ul_speed":7546696.05,   // bps → ÷1e6=Mbps
     "packet_loss":{"sent":0,"dup":0,"max":0}}]}
  ```

## 3. 架构（方案 A：shell 调 speedtest-go）

- 节点发现：`speedtest-go --list` → 解析文本。
- 单节点测：`speedtest-go --server <id> --json` → 解析 JSON。
- 后端一个 job goroutine **串行**跑选中节点，逐个更新状态；同一时刻仅一个任务。
- 沿用现有「shell 调已装工具 + 一键安装」模式，零新增 Go 依赖。

## 4. 全自动状态机

```
idle → [installing 未装则自动装] → listing 取节点(含ISP) → testing 逐个(1/N…) → done
              └ 装失败→error                                  └ 单节点失败→标failed继续下一个
```

- 未装时点开始：自动装（phase=installing，「正在安装测速组件…」）→ 自动挑默认节点 → 开测。
- 已装时：前端先列节点、智能预勾选、可手调，再开测。

## 5. 数据模型（internal/speedtest）

```go
type Server struct { ID, Name, Sponsor string; DistanceKm, PingMs float64; Reachable, Recommended bool }
type NodeResult struct {
    ID, Name, Sponsor string; DistanceKm float64
    Status string // pending|testing|done|failed
    DownloadMbps, UploadMbps, PingMs, JitterMs, LossPct float64
    Error string
}
type Status struct {
    Phase string // idle|installing|listing|testing|done|error
    Running bool; Message string; Nodes []NodeResult
    StartedAt, FinishedAt, Error, ISP string
}
type HistoryEntry struct {
    Time string; BestNode string
    BestDownloadMbps, BestUploadMbps, MinPingMs float64
    Nodes []NodeResult
}
```

智能默认 `pickRecommended(servers, n=3)`：过滤可达 → 按距离排序 → 取最近，再按运营商桶（联通/电信/移动/Unicom/Telecom/Mobile 关键字）去重补足，至多 n 个。

## 6. API（在现有 4 端点上扩展）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/speedtest/status` | 扩为新 Status（phase/message/nodes[]/isp） |
| GET | `/api/v1/speedtest/servers` | 新增：附近节点（含 recommended/ping/reachable） |
| POST | `/api/v1/speedtest/run` | 扩：body `{server_ids:[]}`（空=后端自动挑）；未装自动安装 |
| GET | `/api/v1/speedtest/service` | 保留（探测） |
| POST | `/api/v1/speedtest/install` | 保留（显式安装兜底） |
| GET | `/api/v1/speedtest/history` | 新增：历史 `{items:[HistoryEntry]}` |
| POST | `/api/v1/speedtest/history/clear` | 新增：清空 |

## 7. 历史存储

- 每次任务完成追加到旁车 `DATA_DIR/speedtest_history.json`（保留最近 50 次）。
- `speedtest.New(run, dataDir)` 加 dataDir 参数（改 main.go 装配 + 测试）。

## 8. 前端（web/src/pages/tools/Speedtest.tsx 重写）

```
开始测速  [选择节点 ▾(已选3)]
┌ 本次结果（多节点对比表，实时刷新，最优行高亮）──────────┐
│ 节点 | 运营商 | 下载 | 上传 | 延迟 | 抖动 | 状态(待测/测试中/✓/✗) │
└──────────────────────────────────────────────────────┘
── 历史趋势图（recharts，近20次下载/上传）── ［历史表 | 清空］
```

- 节点选择：抽屉/下拉列出 servers，默认勾选 recommended，可搜索/筛选/增删；上限 8。
- 安装透明化：未装时按钮文案「开始测速（将先自动安装组件）」，跑时显示安装进度。
- 轮询：running 时 1.5s 轮询 status，结束停。

## 9. 错误处理

- 安装失败 → phase=error + 安装输出末尾。
- 单节点失败（不可达/限速/Timeout）→ 该行 failed，继续下一个，不中断整任务。
- 陈旧卡死兜底：`max(5min, 节点数×90s)`。
- `--json` 已真机确认可用；解析失败回退提取错误信息。

## 10. 测试

- 单测（fakeRunner，喂真机实采输出）：`parseServerList`、`parseNodeJSON`（单位换算）、`pickRecommended` 运营商去重、Run 状态流转（install→list→test）、历史读写。
- 真机：speedtest-go flag 已验；实现后跑 2–3 节点端到端 + 历史落盘。
