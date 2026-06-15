# KWRT 网络管理 API 参考（中文 · v1）

> 权威契约见 [internal/api/openapi.yaml](../internal/api/openapi.yaml)（也是 `/api/docs` Scalar UI 的来源）。
> 前端 TS 类型见 [web/src/api/netcfg.ts](../web/src/api/netcfg.ts)，与后端 Go 结构体逐字段对齐。
> 后端 Go 类型见 [internal/netcfg/types.go](../internal/netcfg/types.go)。

## 约定

- 鉴权：除 `/api/v1/health` 与 `GET /api/v1/ui/branding` 外，均需 `Authorization: Bearer <token>`。
- 领域 JSON **一律 snake_case**。请求体启用 `DisallowUnknownFields()`——**多发一个 key 直接 400**。
- 列表返回多为 `{ "items": [...] }`；静态分配另带 `arp_bind`，路由表带 `family`。
- 错误体：`{ "error": { "code": "...", "message": "..." } }`，常见 400（校验/未知字段）/ 401（无 token）/ 404（不存在）。
- 实时变更经 WebSocket `/api/v1/events` 推送（事件类型 `dhcp.changed` / `static.changed` / `lease.changed` / `acl.changed` / `route.changed`）。

## 端点分组

### DHCP 服务端 `/api/v1/dhcp/servers`
`GET` 列表 · `POST` 新增 · `GET/PUT/DELETE {id}` · `POST {id}/toggle {enabled}` · `POST /batch {action,ids}` · `POST /api/v1/dhcp/restart`
字段：`interface, enabled, ip_start, ip_end, netmask, gateway, dns_primary, dns_secondary, lease_minutes, exclude[], expired_keep_hours, check_ip, relay_only, assoc_interface, custom_options[{code,value,type}], remaining(只读)`。

### DHCP 静态分配 `/api/v1/dhcp/statics`
`GET`（带 `arp_bind`）· `POST` · `PUT/DELETE {id}` · `POST {id}/toggle` · `POST /batch` · `PUT /arp-bind {enabled}`
字段：`hostname, ip, mac, gateway, interface, dns_primary, dns_secondary, remark, enabled`。

### DHCP 终端列表 `/api/v1/dhcp/leases`（只读 + 动作）
`GET ?interface=&status=static|dynamic&q=` · `POST /reserve {ip,mac,hostname,interface}` · `POST /blacklist {mac,remark}` · `POST /fix-subnet {interface}`
字段：`hostname, ip, mac, expiry, remaining_seconds, interface, static, remark`。

### DHCP 黑白名单 `/api/v1/dhcp/acl`
`GET` · `PUT /mode {mode:blacklist|whitelist}` · `POST /entries` · `PUT/DELETE /entries/{id}` · `POST /entries/{id}/toggle`
字段：`mode, entries[{id,mac,remark,enabled}]`。

### 静态路由 `/api/v1/routes`
`GET` · `POST` · `GET/PUT/DELETE {id}` · `POST {id}/toggle {enabled}` · `POST {id}/duplicate` · `POST /batch {action,ids}`
字段：`family(ipv4|ipv6), interface(auto|名), target, netmask, prefix, gateway, metric, remark, enabled`。

### 当前路由表 `/api/v1/route-table?family=ipv4|ipv6`（只读）
返回 `{ family, items[{interface,target,netmask,gateway,metric}] }`。

### 公共 / 状态
`GET /api/v1/interfaces`（服务接口 / 线路下拉源）· `GET /api/v1/netcfg/status`（`{backend,dhcp_ok,pending,message}`）。

### 备份 / 导入导出（壳子）
`GET /api/v1/export/all`（meta + netcfg.json 的 zip）· `POST /api/v1/import/zip` · `/api/v1/backup/*`（存储渠道、计划、运行历史）。

> 完整请求/响应 schema 以 `openapi.yaml` 为准；本页仅作速查。
