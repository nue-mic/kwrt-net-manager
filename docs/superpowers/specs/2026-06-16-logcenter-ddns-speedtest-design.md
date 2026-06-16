# 日志中心 + 动态域名(DDNS) + 线路测速 —— 设计（OpenWrt 原生优先）

> 2026-06-16。来源：爱快截图（日志中心系统/高级应用-动态域名/应用工具-线路测速）。
> **第一准则**：只迁移 OpenWrt 能原生实现的功能；不能原生的，诚实降级并标注，绝不造假。

## 0. 真机核实（ImmortalWrt 24.10.6 / x86_64，USTC 源）

- `ddns-scripts 2.8.2` 可装（`/etc/config/ddns`，luci-app-ddns 同源）。
- 测速包：`speedtest-go`（纯 Go 单二进制、`--json` 输出、走 speedtest.net 自动选最近服务器＝国内可用）、speedtest-cli(py)、speedtest-netperf、speedtestcpp。**选 speedtest-go**（无 python 依赖、易解析、国内可用）。
- `logread` 可读（含 kwrtmgrd 自身日志）；dnsmasq-dhcp 行当前为空＝该旁路由 DHCP 池停用、无 DHCP 事件（符合预期）。
- `ip neigh` 可用：`IP dev IFACE lladdr MAC STATE`。

## 1. 日志中心（Log Center）

统一日志查看：`GET /api/v1/logs/{source}?start&end&keyword&page&page_size` → `{items,total}`；按 source 列不同。导出 `/api/v1/logs/{source}/export`；清空仅对「本工具自管」源有效。

| 爱快页 | source | OpenWrt 原生来源 | 可行性 |
|---|---|---|---|
| 系统日志 | `system` | `logread` 全量，解析 时间/设施.级别/进程/消息 | ✅ 原生 |
| DHCP日志 | `dhcp` | `logread` 过滤 `dnsmasq-dhcp`(v4 DISCOVER/OFFER/REQUEST/ACK/NAK)+`odhcpd`(v6)，解析 类型/接口/MAC/IP | ✅ 原生（投射 dnsmasq `logdhcp=1` 拿全量） |
| 外网拨号日志 | `dialup` | `logread` 过滤 `pppd/pppoe/netifd/udhcpc`（WAN up/down/拿地址） | ✅ 原生（旁路由可能为空） |
| 动态域名日志 | `ddns` | ddns-scripts 日志 `/var/log/ddns/*.log` | ✅ 原生（DDNS 启用后） |
| 操作日志 | `operation` | **本工具审计**：登录 + 每个写操作（模块/动作/用户/客户端IP/时间）→ `DATA_DIR/logs/operation.jsonl`（环形截断） | ✅ 应用自管（我们的 API 我们记） |
| ARP日志 | `arp` | **本工具轮询** `ip neigh` 差分：新绑定 + 某 IP 的 MAC 变化(=疑似 ARP 冲突/欺骗) → `DATA_DIR/logs/arp.jsonl` | ✅ 应用逻辑 over 原生 `ip neigh` |

**降级/不做**：认证日志(PPPoE/Portal 用户)、无线终端日志(需本机为 AP)、消息通知/告警——非旁路由场景或无原生来源，跳过并在 UI 说明。

前端：一个通用 `LogCenter` 组件，按 source 参数渲染（共享时间范围+关键字过滤、分页、导出），左侧菜单「日志中心」下挂 系统/DHCP/拨号/DDNS/操作/ARP 多入口。

## 2. 动态域名 DDNS（高级应用）

原生 `ddns-scripts`：`/etc/config/ddns` 具名 `config service`，marker 隔离（`kwrt-net-manager-ddns`），一键安装复用 DoH 的自愈回退。

- 列表 `GET /api/v1/ddns`：provider(service_name)、域名(lookup_host)、ip_source、接口、记录类型(A/AAAA)、状态、**更新结果+当前IP+更新时间**（读 `/var/run/ddns/<name>.ip` 与 ddns 状态）。
- 增改：服务商（cloudflare/dnspod/aliyun/no-ip/dyndns/华为…，下拉=本机 `/usr/share/ddns/default/*.json` 实际支持的）、域名、配置方式(Token/Key/账号密码→`param_opt`/`username`/`password`)、解析设置=**外网线路**（`ip_source='web'` 探测公网出口IP，或 `'network'` 取接口IP）、解析网卡(`ip_network`=wan)、记录类型(A/AAAA→`use_ipv6`)。
- 启用/停用/删除/批量；服务探测+一键安装。
- **降级**：爱快「解析设置=终端MAC」（把某 LAN 设备的当前 IP/IPv6 顶到域名）—— ddns-scripts 无此原生能力，**不做**，UI 说明只支持「外网线路/出口IP」。

## 3. 线路测速（应用工具）

原生 `speedtest-go`：一键安装（自愈回退）。异步：`POST /api/v1/speedtest/run` 起后台测速；`GET /api/v1/speedtest/status` → `{running,result:{download_mbps,upload_mbps,ping_ms,server,isp},error,started_at}`。解析 `speedtest-go --json`。

- **降级**：爱快是自带测速程序+实时仪表盘逐秒刷新；speedtest-go 输出最终结果（下载/上传/延迟/服务器），前端展示「测速中…」+最终结果（非逐秒指针）。诚实标注。
- 多线路：按所选出接口测（speedtest-go 默认走默认路由；指定接口能力有限，先支持默认线路，标注）。

## 4. 通用：包安装自愈复用

把 `runPkgInstall`/`fallbackMirrorGroups`（DoH 自愈）泛化为 `installPkgs(pkgs, initdName)`，DDNS/测速共用：现有源→USTC→官方，临时源装完即删，不动 distfeeds。

## 5. 安全红线（沿用）

- 只动带本工具 marker 的 UCI 具名节（ddns）；不碰 stock。
- 审计/ARP 日志落 `DATA_DIR/logs/*.jsonl`，环形截断（默认上限如 5000 行/源），不撑爆 tmpfs。
- 全部校验在 Go 层、写 UCI 之前。
