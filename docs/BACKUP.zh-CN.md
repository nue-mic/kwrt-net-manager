# 定时备份机制设计（kwrt-net-manager）

> 把「手动点导出 ZIP」升级为「配置好存储渠道 → 设定定时规则 → 守护进程按时打包并上传到云端」。
> 全部配置持久化在 `meta.json`，跨重启 / 更新 / 备份还原均不丢失。

---

## 1. 总体模型

三个解耦概念：

| 概念 | 说明 | 数量 |
|---|---|---|
| **存储渠道 Channel** | 一个云端存储目标的连接配置。当前支持 `s3`（统一覆盖 AWS S3 / 阿里云 OSS / Cloudflare R2 / MinIO 等所有 S3 兼容对象存储）与 `webdav`（Nextcloud / 坚果云 / 群晖等）。 | 多个 |
| **备份计划 Schedule** | 一条「按 cron 规则、把全量配置打包上传到某个渠道」的定时任务。引用一个 Channel，可单独开启 / 关闭。 | 多个 |
| **备份记录 Run** | 每次执行（定时或手动）的结果：时间、状态、对象路径、大小、错误。滚动保留最近若干条。 | 历史 |

执行链路：`cron 触发 / 手动触发 → 复用 ExportAll 逻辑打包全量 zip（profiles/*.toml + meta.json）→ 渲染对象路径 → 上传到渠道 → 按保留策略清理旧备份 → 记录 Run + 推事件`。

---

## 2. 存储路径方案（专业、最流行）

每个计划用一个**单一路径模板**渲染对象键，默认：

```
frpcmgr-backups/{schedule}/{year}/{month}/frpcmgr-{date}-{time}.zip
```

渲染示例（计划名「每日」，UTC 2026-06-13 03:00:05）：

```
frpcmgr-backups/每日/2026/06/frpcmgr-20260613-030005.zip
```

支持的占位符（时间统一按 **UTC** 渲染，避免歧义；与导出 zip 文件名一致）：

| 占位符 | 含义 | 示例 |
|---|---|---|
| `{schedule}` | 计划名（slug 化：去除路径不安全字符） | `每日` |
| `{host}` | 主机名 | `vps-hk-1` |
| `{year}` / `{YYYY}` | 年 | `2026` |
| `{month}` / `{MM}` | 月（补零） | `06` |
| `{day}` / `{DD}` | 日（补零） | `13` |
| `{date}` | `YYYYMMDD` | `20260613` |
| `{time}` | `HHMMSS` | `030005` |
| `{datetime}` / `{ts}` | `YYYYMMDD-HHMMSS` | `20260613-030005` |

**保留策略（安全、健壮）**：取模板中**第一个时间类占位符之前**的稳定目录作为「保留根」，在此根下递归列举对象后，**只挑出由本计划自己的模板正则匹配中的对象**（用模板生成一个锚定正则，时间 token → 数字类）——因此**绝不会误删别的计划、或同目录下别的应用的文件**，也不依赖 `.zip` 后缀。保留份数的「新旧」判定使用**存储后端返回的真实修改时间**（S3 `LastModified` / WebDAV `ModTime`）而非 key 字典序，所以即便用户自定义模板把时间戳顺序打乱，也不会误删最新备份。保存计划时还会**校验同一渠道下不存在保留作用域冲突的两个计划**（否则 400 拒绝）。

渠道还可配置一个**基础前缀 `prefix`**（S3 为桶内子目录、WebDAV 为根下子路径），最终键 = `prefix` ⊕ 渲染后的模板。

---

## 3. 数据模型（持久化于 `meta.json` 的 `backup` 字段）

```jsonc
"backup": {
  "channels": [
    {
      "id": "ch_a1b2c3d4e5f6", "name": "我的R2", "kind": "s3",
      "s3": {
        "endpoint": "xxx.r2.cloudflarestorage.com", "region": "auto",
        "bucket": "frpc", "access_key_id": "AK...", "secret_access_key": "SK...",
        "prefix": "", "use_ssl": true, "path_style": true
      },
      "created_at": 1750000000, "updated_at": 1750000000
    },
    {
      "id": "ch_...", "name": "坚果云", "kind": "webdav",
      "webdav": { "base_url": "https://dav.jianguoyun.com/dav/", "username": "u", "password": "p", "prefix": "frpc" }
    }
  ],
  "schedules": [
    {
      "id": "sc_...", "name": "每日", "enabled": true,
      "cron": "0 3 * * *", "channel_id": "ch_a1b2c3d4e5f6",
      "path_template": "frpcmgr-backups/{schedule}/{year}/{month}/frpcmgr-{date}-{time}.zip",
      "retention": 14, "created_at": 1750000000, "updated_at": 1750000000
    }
  ],
  "runs": [
    { "id": "run_...", "schedule_id": "sc_...", "channel_id": "ch_...",
      "trigger": "schedule", "status": "success",
      "started_at": 1750000000, "finished_at": 1750000003,
      "object_path": "frpcmgr-backups/每日/2026/06/frpcmgr-20260613-030005.zip",
      "size_bytes": 20480, "error": "" }
  ]
}
```

- **ID**：`ch_` / `sc_` / `run_` 前缀 + 12 位十六进制随机（crypto/rand）。
- **cron**：robfig/cron/v3 标准 5 段表达式，或 `@daily`/`@hourly`/`@every 6h` 描述符。
- **retention**：保留最近 N 个，`0` = 不限。
- **runs**：滚动保留最近 200 条（仅本机历史，不随备份还原迁移）。

### 持久化与还原不丢
- channels / schedules / runs 全部存 `meta.json`，与 branding / system_config 同一份原子写文件，**跨重启 / `kmc update` 不丢**。
- `GET /export/all` 的 zip 自带 `meta.json` → channels / schedules 随备份一起带走；`POST /import/zip` 的 `ImportMeta` 还原 channels + schedules（runs 属本机历史不还原）。回报 `backup_restored`。

### 安全（密钥处理）
- 渠道密钥（S3 `secret_access_key` / WebDAV `password`）在本机 `meta.json` 中**明文存储**（守护进程需用它发起上传），跨重启/更新不丢。
- **API 读取一律脱敏**：`GET` 不回显密钥/口令，只回 `secret_access_key_set` / `password_set` 布尔。
- **更新留空保持不变**：`PUT` 时密钥字段留空 = 沿用原值（合并在存储层锁内完成，避免并发丢字段）；切换渠道类型会清空另一类型的残留密钥。
- **测试连接不外泄密钥**：测试未保存配置（`/channels/test`）只测请求体里自带的配置，**绝不复用存量密钥**；要用存量密钥测已保存渠道请走 `/channels/{id}/test`（目标地址不可被请求体改写）。
- **备份产物剔除密钥**：`/export/all` 下载包与定时上传到云端的 zip 里，`meta.json` 的渠道密钥被**置空（redact）**——凭据不会随备份外流到（可能共享的）存储目标或下载文件。
- **还原语义**：导入备份时渠道结构（名称/endpoint/bucket 等）照常还原；密钥若为空，则在**同主机**（已有同 id 渠道）下沿用本机现有密钥，**跨主机**还原后需在 UI 重新填写密钥。引用了不存在渠道的孤儿计划在导入时被剔除。

---

## 4. API（`/api/v1/backup/*`，均在鉴权子树）

| 方法 路径 | 说明 |
|---|---|
| `GET /backup/channels` | 列存储渠道（脱敏） |
| `POST /backup/channels` | 新建渠道 |
| `PUT /backup/channels/{id}` | 更新渠道（密钥留空=不变） |
| `DELETE /backup/channels/{id}` | 删除渠道（被计划引用则 409） |
| `POST /backup/channels/{id}/test` | 测试连通性（S3 BucketExists / WebDAV stat 根） |
| `POST /backup/channels/test` | 测试一份未保存的渠道配置 |
| `GET /backup/channels/{id}/objects` | 浏览渠道上实际存在的 `.zip` 备份（最新在前，恢复用） |
| `POST /backup/channels/{id}/restore` | 下载某备份对象并恢复配置（`{key}`，复用 import 逻辑） |
| `GET /backup/schedules` | 列备份计划（含 last_run 摘要） |
| `POST /backup/schedules` | 新建计划 |
| `PUT /backup/schedules/{id}` | 更新计划 |
| `DELETE /backup/schedules/{id}` | 删除计划 |
| `POST /backup/schedules/{id}/toggle` | 开启 / 关闭 |
| `POST /backup/schedules/{id}/run` | 立即执行一次（手动触发） |
| `GET /backup/runs?limit=` | 备份历史 |

任何 channel/schedule 变更后，后端调用 `scheduler.Reload()` 重建 cron 注册表，**改动即时生效，无需重启**。

---

## 5. 代码结构

```
internal/backup/
  model.go      # Channel / S3Config / WebDAVConfig / Schedule / RunRecord + 脱敏/合并/clone
  id.go         # newID(prefix) crypto/rand
  path.go       # 路径模板渲染 + 保留前缀推导 + slug
  uploader.go   # Uploader 接口 + NewUploader 工厂
  s3.go         # minio-go 实现：Put/List/Delete/Test
  webdav.go     # gowebdav 实现：Put(自动 MkdirAll)/List(递归)/Delete/Test
  scheduler.go  # robfig/cron 调度；RunBackup 作业；Reload/RunNow/Stop；防重入
internal/manager/
  meta.go       # Meta.backup 持久化 + metaStore 增删改 + clone
  manager.go    # Backup* 包装方法 + BuildBackupZip(io.Writer)（抽自 ExportAll）+ ImportMeta 还原
internal/api/backup.go      # BackupHandler（CRUD/test/run/runs）
internal/api/server.go      # 路由 + Deps.Scheduler
cmd/kwrtmgrd/main.go        # 构造 scheduler、AutoStart 后 Start、defer Stop
web/src/api/backup.ts       # 类型 + axios 封装
web/src/pages/Backup.tsx    # 渠道 / 计划 / 历史 三区 + 表单弹窗 + 测试/立即备份
```

调度器解耦：`scheduler` 依赖三个接口（`Store` 取 channels/schedules、`Source` = `BuildBackupZip`、`Recorder` 记录 Run），均由 `Manager` 结构化满足 → 无 `manager ↔ backup` 循环依赖（`manager → backup` 单向，仅取模型类型）。
