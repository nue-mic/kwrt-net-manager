# frpc-manager 全面对称改名 + fmc upgrade-legacy 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把本仓库所有标识从中性 `frp*` 改为客户端专属 `frpc*`（二进制 `frpcmgrd`、env 前缀 `FRPCMGR_`、module `…/frpc-manager`），与姊妹仓库 `frps-manager` 严格对称；并新增独立幂等的 `fmc upgrade-legacy` 一键迁移旧 `frpmgrd` 部署。

**Architecture:** 三轮机械批量替换（module → 二进制/服务名 → env 前缀），每轮一个 commit 且必须编译/测试通过；随后在 install.sh / install.ps1 各自生成的 `fmc` 脚本里新增 `upgrade-legacy` 子命令；最后同步文档并 push 触发 CI 发布验证。install/update 主流程**不感知**旧部署。

**Tech Stack:** Go 1.25、POSIX sh（install.sh 内嵌 fmc）、PowerShell 5.1（install.ps1 内嵌 fmc.ps1 + nssm）、goreleaser、GitHub Actions。

**统一约束（每个 Task 都适用）:**
- 批量替换一律排除：`docs/superpowers/`、`CHANGELOG.md`、`bin/`、`tmp/`、`*.exe`、`.git/`、`node_modules/`（历史归档与产物不动）。
- `install.ps1` 含中文，任何编辑后必须复核头三字节为 `ef bb bf`（UTF-8 BOM）。
- 验证以事实为准：编译/测试/grep 实际跑过再勾选。
- Windows 环境用 bash（git-bash）执行 git/go/rg/sed。

---

## Task 1: Go module 改名 `frp-manager-server → frpc-manager`

**Files:**
- Modify: `go.mod`（module 声明）
- Modify: 所有含该字符串的 `.go`（约 30 个：`internal/**`、`pkg/**`、`services/**`、`cmd/**`）
- Modify: `Makefile`、`.goreleaser.yml`、`deploy/Dockerfile`（ldflags `-X …/pkg/version.*`）

字符串 `github.com/mia-clark/frp-manager-server` 在 import 与 ldflags 中完全一致，一条替换全覆盖。

- [ ] **Step 1: 批量替换**

```bash
cd "d:/Github_Codes_mia-clark/frpc-manager"
rg -l "frp-manager-server" -g '!docs/superpowers' -g '!CHANGELOG.md' -g '!bin' -g '!tmp' -g '!*.exe' \
  | tr -d '\r' | xargs sed -i 's#frp-manager-server#frpc-manager#g'
```

- [ ] **Step 2: 同步依赖图并验证编译**

```bash
go mod tidy
go build ./...
```
Expected: 无错误（module 声明与所有 import 一致）。

- [ ] **Step 3: 静态检查 + 测试**

```bash
go vet ./...
go test ./...
```
Expected: PASS。

- [ ] **Step 4: 确认 ldflags 已同步、无残留**

```bash
grep -n "pkg/version" Makefile .goreleaser.yml deploy/Dockerfile
rg "frp-manager-server" -g '!docs/superpowers' -g '!CHANGELOG.md' -g '!bin' -g '!tmp' -g '!*.exe'
```
Expected: 第一条全部显示 `…/frpc-manager/pkg/version`；第二条无输出。

- [ ] **Step 5: 验证版本号注入未坏（关键）**

```bash
go build -ldflags "-X github.com/mia-clark/frpc-manager/pkg/version.Number=test123" -o /tmp/vt ./cmd/frpmgrd
/tmp/vt version | grep -q test123 && echo OK
```
Expected: `OK`（证明 module 改名后 ldflags symbol path 正确，版本能注入）。

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor(module): Go module 改名 frp-manager-server → frpc-manager（与 frps-manager 对称）"
```

---

## Task 2: 二进制 / 服务名 `frpmgrd → frpcmgrd`

**Files:**
- Rename: `cmd/frpmgrd/` → `cmd/frpcmgrd/`
- Modify: `Makefile`、`.goreleaser.yml`、`deploy/Dockerfile`、`deploy/entrypoint.sh`、`deploy/docker-compose.yml`、`deploy/docker-compose.standalone.yml`
- Modify: `scripts/install.sh`、`scripts/install.ps1`（`SERVICE_NAME`/`BIN_NAME`/`$ServiceName`/`$BinName`/目录派生 + fmc 内嵌脚本里的标题/日志文件名）
- Modify: `internal/api/docs.go`、`internal/sysinfo/sysinfo.go`（标题/默认字符串）

`frpmgrd` 是独立词，`frpcmgrd` 不含 `frpmgrd` 子串，全局替换不会二次命中。

- [ ] **Step 1: 重命名 cmd 目录（保留 git 历史）**

```bash
cd "d:/Github_Codes_mia-clark/frpc-manager"
git mv cmd/frpmgrd cmd/frpcmgrd
```

- [ ] **Step 2: 批量替换二进制/服务名**

```bash
rg -l "frpmgrd" -g '!docs/superpowers' -g '!CHANGELOG.md' -g '!bin' -g '!tmp' -g '!*.exe' \
  | tr -d '\r' | xargs sed -i 's#frpmgrd#frpcmgrd#g'
```
这会覆盖：Makefile 的 `./cmd/frpcmgrd` 与产物名、.goreleaser `project_name`/`binary`/`main`、Dockerfile、entrypoint、compose、install 脚本的 `SERVICE_NAME="frpcmgrd"` / `$ServiceName = 'frpcmgrd'`（→ 派生 unit/init/plist/数据目录/日志全部跟随）、fmc 内嵌脚本里的 `frpcmgrd 运行信息` 标题与 `frpcmgrd.log`。

- [ ] **Step 3: 验证编译（cmd 路径已变）**

```bash
go build ./...
go vet ./...
go test ./...
```
Expected: PASS（`.goreleaser`/`Makefile` 的 `main: ./cmd/frpcmgrd` 与新目录一致）。

- [ ] **Step 4: 校验脚本语法 + BOM + 残留**

```bash
sh -n scripts/install.sh && echo "install.sh OK"
head -c 3 scripts/install.ps1 | od -An -tx1   # 必须 ef bb bf
rg "frpmgrd" -g '!docs/superpowers' -g '!CHANGELOG.md' -g '!bin' -g '!tmp' -g '!*.exe'
```
Expected: install.sh OK；BOM 为 `ef bb bf`；无残留输出。

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(cli): 二进制/服务名 frpmgrd → frpcmgrd（cmd 目录、构建、服务、安装脚本）"
```

---

## Task 3: 环境变量前缀 `FRPMGR_ → FRPCMGR_`

**Files:**
- Modify: `internal/appcfg/appcfg.go`（读取键）、`cmd/frpcmgrd/main.go`
- Modify: `scripts/install.sh`、`scripts/install.ps1`（写 env / 读 env / `FRPMGR_INSTALL_URL` 镜像变量 / fmc 里 `env_get FRPMGR_*` 与正则 `^FRPMGR_*`）
- Modify: `deploy/Dockerfile`、`deploy/entrypoint.sh`、`deploy/docker-compose.yml`、`deploy/docker-compose.standalone.yml`
- Modify: `web/e2e/fixtures/daemon.ts`
- Modify: `README.md`、`docs/API.zh-CN.md`、`docs/README-server.md`、`CLAUDE.md`（前缀出现处）

`FRPMGR_` 是唯一前缀串，替换安全；含 `FRPMGR_INSTALL_URL`、`FRPMGR_HTTP_ADDR`、`FRPMGR_API_TOKEN`、`FRPMGR_DATA_DIR`、`FRPMGR_LOG_LEVEL`、`FRPMGR_CORS_ORIGINS`、`FRPMGR_DOCS_ENABLED`。

- [ ] **Step 1: 批量替换前缀**

```bash
cd "d:/Github_Codes_mia-clark/frpc-manager"
rg -l "FRPMGR_" -g '!docs/superpowers' -g '!CHANGELOG.md' -g '!bin' -g '!tmp' -g '!*.exe' \
  | tr -d '\r' | xargs sed -i 's#FRPMGR_#FRPCMGR_#g'
```

- [ ] **Step 2: 验证编译 + 测试（appcfg 读取键已变）**

```bash
go build ./...
go vet ./...
go test ./...
```
Expected: PASS（appcfg 读 `FRPCMGR_*`，main.go 一致）。

- [ ] **Step 3: 校验脚本语法 + BOM + 残留**

```bash
sh -n scripts/install.sh && echo "install.sh OK"
head -c 3 scripts/install.ps1 | od -An -tx1   # 必须 ef bb bf
rg "FRPMGR_" -g '!docs/superpowers' -g '!CHANGELOG.md' -g '!bin' -g '!tmp' -g '!*.exe'
```
Expected: OK；BOM `ef bb bf`；无残留。

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor(config): 环境变量前缀 FRPMGR_ → FRPCMGR_"
```

---

## Task 4: 新增 `fmc upgrade-legacy` 迁移命令

迁移把旧 `frpmgrd` 部署搬到新 `frpcmgrd`。**前提**：用户已用新脚本装好 `frpcmgrd`（新 `fmc` 才带此命令）。命令幂等：无旧部署时友好退出。

### 4A — Linux/macOS：install.sh 内嵌 fmc（`FMC_EOF` 单引号 heredoc 内）

**Files:** Modify `scripts/install.sh`

- [ ] **Step 1: 在 `cmd_uninstall() { … }` 之后、`usage() {` 之前插入迁移函数**

定位：`scripts/install.sh` 中 `cmd_uninstall()` 函数结束的 `}` 行（约 754 行）与 `usage() {`（约 756 行）之间。插入：

```sh
# ----------------------------------------------------------------------------
# 一键迁移: 把旧版 frpmgrd 部署 (服务/数据/配置) 迁到新 frpcmgrd
#   独立命令, 与 install/update 解耦; 幂等, 无旧部署则直接返回
# ----------------------------------------------------------------------------
cmd_upgrade_legacy() {
    OLD_SVC="frpmgrd"
    OLD_BIN="${INSTALL_DIR}/frpmgrd"
    OLD_ENV="/etc/frpmgrd/frpmgrd.env"
    OLD_PLIST="/Library/LaunchDaemons/com.miaclark.frpmgrd.plist"
    case "$(uname -s 2>/dev/null)" in
        Darwin)  OLD_DATA="/usr/local/var/frpmgrd" ;;
        FreeBSD) OLD_DATA="/var/db/frpmgrd" ;;
        *)       OLD_DATA="/var/lib/frpmgrd" ;;
    esac
    _init="$(detect_init)"

    # 1. 探测旧部署 (服务/数据/配置/二进制 任一存在即需迁移)
    _need=0
    [ -e "$OLD_ENV" ] && _need=1
    [ -d "$OLD_DATA" ] && _need=1
    [ -e "$OLD_BIN" ] && _need=1
    case "$_init" in
        systemd) [ -f "/etc/systemd/system/${OLD_SVC}.service" ] && _need=1 ;;
        openrc)  [ -f "/etc/init.d/${OLD_SVC}" ] && _need=1 ;;
        launchd) [ -f "$OLD_PLIST" ] && _need=1 ;;
    esac
    if [ "$_need" -eq 0 ]; then
        ok "未检测到旧版 frpmgrd 部署, 无需迁移"
        exit 0
    fi

    info "检测到旧版 frpmgrd, 开始迁移到 frpcmgrd ..."

    # 2. 停两侧服务 (新服务避免占用数据目录; 旧服务停并禁用)
    case "$_init" in
        systemd)
            priv systemctl stop "$SERVICE_NAME" 2>/dev/null || true
            priv systemctl stop "$OLD_SVC" 2>/dev/null || true
            priv systemctl disable "$OLD_SVC" 2>/dev/null || true ;;
        openrc)
            priv rc-service "$SERVICE_NAME" stop 2>/dev/null || true
            priv rc-service "$OLD_SVC" stop 2>/dev/null || true
            priv rc-update del "$OLD_SVC" default 2>/dev/null || true ;;
        launchd)
            priv launchctl unload "$PLIST" 2>/dev/null || true
            priv launchctl unload -w "$OLD_PLIST" 2>/dev/null || true ;;
    esac

    # 3. 迁移数据目录 (仅当新目录没有真实隧道配置时才迁, 绝不覆盖)
    if [ -d "$OLD_DATA" ]; then
        if ls "$DATA_DIR"/profiles/*.toml >/dev/null 2>&1; then
            warn "新数据目录已有隧道配置, 跳过数据迁移 (旧数据保留在 $OLD_DATA)"
        else
            priv rm -rf "$DATA_DIR" 2>/dev/null || true
            priv mkdir -p "$(dirname "$DATA_DIR")"
            priv mv "$OLD_DATA" "$DATA_DIR"
            ok "数据目录已迁移: $OLD_DATA -> $DATA_DIR"
        fi
    fi

    # 4. 迁移配置 (前缀 FRPMGR_ -> FRPCMGR_, 并把 DATA_DIR 指向新路径)
    if [ -f "$OLD_ENV" ]; then
        if [ -f "$ENV_FILE" ]; then
            warn "新配置已存在, 跳过配置迁移 (旧配置保留在 $OLD_ENV)"
        else
            priv mkdir -p "$(dirname "$ENV_FILE")"
            sed -e 's/^FRPMGR_/FRPCMGR_/' \
                -e "s#^FRPCMGR_DATA_DIR=.*#FRPCMGR_DATA_DIR=${DATA_DIR}#" \
                "$OLD_ENV" | priv tee "$ENV_FILE" >/dev/null
            ok "配置已迁移: $OLD_ENV -> $ENV_FILE (前缀 FRPMGR_ -> FRPCMGR_)"
        fi
    fi

    # 5. 清理旧服务单元与二进制
    case "$_init" in
        systemd) priv rm -f "/etc/systemd/system/${OLD_SVC}.service"; priv systemctl daemon-reload 2>/dev/null || true ;;
        openrc)  priv rm -f "/etc/init.d/${OLD_SVC}" ;;
        launchd) priv rm -f "$OLD_PLIST" ;;
    esac
    priv rm -f "$OLD_BIN" 2>/dev/null || true
    priv rmdir "/etc/frpmgrd" 2>/dev/null || true
    ok "旧服务与二进制已清理"

    # 6. 启动新服务并展示信息
    cmd_start || warn "新服务启动失败, 请手动执行 fmc start 检查"
    ok "迁移完成 ✅ 当前运行的是 frpcmgrd"
}
```

- [ ] **Step 2: 在 `usage()` 的"安装维护"段加一行**

定位 `usage()` 里 `  uninstall        卸载` 之后，加：

```sh
  upgrade-legacy   迁移旧版 frpmgrd 部署到 frpcmgrd (服务/数据/配置)
```

- [ ] **Step 3: 在 case 分发里加分支**

定位 `case "${1:-help}" in` 块中 `uninstall)  shift; cmd_uninstall "$@"; exit 0 ;;` 之后，加：

```sh
    upgrade-legacy|upgrade_legacy) shift; cmd_upgrade_legacy "$@"; exit 0 ;;
```

- [ ] **Step 4: 校验 install.sh 语法**

```bash
sh -n scripts/install.sh && echo "install.sh OK"
```
Expected: `install.sh OK`。

- [ ] **Step 5: 抽取内嵌 fmc 脚本单独验证语法**

```bash
awk '/^    cat >> "\$_tmp_cli" <<.FMC_EOF.$/{f=1;next} /^FMC_EOF$/{f=0} f' scripts/install.sh > /tmp/fmc.sh
sh -n /tmp/fmc.sh && echo "fmc body OK"
```
Expected: `fmc body OK`（确认生成的 fmc 脚本本身语法正确）。

### 4B — Windows：install.ps1 内嵌 fmc.ps1（`$body` 单引号 here-string 内）

**Files:** Modify `scripts/install.ps1`

- [ ] **Step 6: 在 `function Do-Version { … }` 之后插入迁移函数**

定位 `$body` here-string 内 `function Do-Version { & $BinPath version }` 行之后插入：

```powershell
function Do-UpgradeLegacy {
    Need-Admin
    $oldSvc     = 'frpmgrd'
    $oldData    = Join-Path $env:ProgramData 'frpmgrd\data'
    $oldLogs    = Join-Path $env:ProgramData 'frpmgrd\logs'
    $oldInstall = Join-Path $env:ProgramFiles 'frpmgrd'
    $oldNssm    = Join-Path $oldInstall 'nssm.exe'

    $oldSvcObj = Get-Service -Name $oldSvc -ErrorAction SilentlyContinue
    if (-not ($oldSvcObj -or (Test-Path $oldData) -or (Test-Path $oldInstall))) {
        Write-Host '[+] 未检测到旧版 frpmgrd 部署, 无需迁移' -ForegroundColor Green
        exit 0
    }
    Write-Host '[*] 检测到旧版 frpmgrd, 开始迁移到 frpcmgrd ...' -ForegroundColor Blue

    # 1. 删除旧服务前, 先读出旧环境变量 (FRPMGR_*)
    $oldEnv = @()
    if (Test-Path $oldNssm) { $oldEnv = @(& $oldNssm get $oldSvc AppEnvironmentExtra 2>$null) }

    # 2. 停新服务 + 停删旧服务
    if (Use-Nssm) { & $NssmPath stop $ServiceName 2>$null | Out-Null }
    if ($oldSvcObj) {
        if (Test-Path $oldNssm) {
            & $oldNssm stop   $oldSvc       2>$null | Out-Null
            & $oldNssm remove $oldSvc confirm 2>$null | Out-Null
        } else {
            & sc.exe stop   $oldSvc 2>$null | Out-Null
            & sc.exe delete $oldSvc 2>$null | Out-Null
        }
        Write-Host '[+] 旧服务 frpmgrd 已停止并移除' -ForegroundColor Green
    }

    # 3. 迁移数据目录 (新目录无隧道配置时才迁, 绝不覆盖)
    if (Test-Path $oldData) {
        $newProfiles = Join-Path $DataDir 'profiles'
        $hasNew = (Test-Path $newProfiles) -and (Get-ChildItem -Path $newProfiles -Filter *.toml -ErrorAction SilentlyContinue)
        if ($hasNew) {
            Write-Host "[!] 新数据目录已有隧道配置, 跳过数据迁移 (旧数据保留在 $oldData)" -ForegroundColor Yellow
        } else {
            if (Test-Path $DataDir) { Remove-Item -Recurse -Force $DataDir -ErrorAction SilentlyContinue }
            New-Item -ItemType Directory -Force -Path (Split-Path $DataDir -Parent) | Out-Null
            Move-Item -Force $oldData $DataDir
            Write-Host "[+] 数据目录已迁移: $oldData -> $DataDir" -ForegroundColor Green
        }
    }

    # 4. 把旧环境变量 (FRPMGR_*) 以 FRPCMGR_ 前缀重设到新服务, DATA_DIR 指向新路径
    if ($oldEnv.Count -gt 0 -and (Use-Nssm)) {
        $newEnv = foreach ($line in $oldEnv) {
            if ($line -match '^\s*FRPMGR_(.+)$') {
                $kv = $Matches[1]
                if ($kv -match '^DATA_DIR=') { "FRPCMGR_DATA_DIR=$DataDir" } else { "FRPCMGR_$kv" }
            }
        }
        $newEnv = @($newEnv | Where-Object { $_ })
        if ($newEnv.Count -gt 0) {
            & $NssmPath set $ServiceName AppEnvironmentExtra $newEnv | Out-Null
            Write-Host '[+] 配置已迁移到新服务 (前缀 FRPMGR_ -> FRPCMGR_)' -ForegroundColor Green
        }
    }

    # 5. 清理旧二进制目录
    if (Test-Path $oldInstall) { Remove-Item -Recurse -Force $oldInstall -ErrorAction SilentlyContinue }
    if ((Test-Path $oldLogs) -and -not (Test-Path $LogDir)) { Move-Item -Force $oldLogs $LogDir }

    # 6. 启动新服务
    if (Use-Nssm) { & $NssmPath start $ServiceName 2>$null | Out-Null } else { & sc.exe start $ServiceName 2>$null | Out-Null }
    Write-Host '[+] 迁移完成, 当前运行的是 frpcmgrd' -ForegroundColor Green
}
```

- [ ] **Step 7: 在 `Show-Usage` 的"安装维护"段加一行**

定位 `Show-Usage` here-string 内 `  uninstall        卸载` 之后，加：

```
  upgrade-legacy   迁移旧版 frpmgrd 部署到 frpcmgrd (服务/数据/配置)
```

- [ ] **Step 8: 在 `switch ($Cmd.ToLower())` 里加分支**

定位 `uninstall` 分支闭合 `}` 之后、`default {` 之前，加：

```powershell
    'upgrade-legacy' { Do-UpgradeLegacy; exit 0 }
    'upgrade_legacy' { Do-UpgradeLegacy; exit 0 }
```

- [ ] **Step 9: 复核 BOM**

```bash
head -c 3 scripts/install.ps1 | od -An -tx1
```
Expected: `ef bb bf`。

- [ ] **Step 10: Commit**

```bash
git add scripts/install.sh scripts/install.ps1
git commit -m "feat(cli): 新增 fmc upgrade-legacy 一键迁移旧版 frpmgrd 部署"
```

---

## Task 5: 文档同步 + CHANGELOG + 发布验证

**Files:**
- Modify: `README.md`、`CLAUDE.md`、`docs/API.zh-CN.md`、`docs/README-server.md`、`web/README.md`、`web/e2e/README.md`（残余 `frpmgrd`/`FRPMGR_` 说明文字——Task 2/3 的 sed 已覆盖大部分；此处人工通读确认语义通顺，尤其新增 `fmc upgrade-legacy` 用法说明）
- Modify: `CHANGELOG.md`（**新增**条目，不改旧条目）

- [ ] **Step 1: 在 README 的 fmc 命令清单里补充 upgrade-legacy**

定位 `README.md` 中 `fmc uninstall    # 卸载` 之后，加：

```
fmc upgrade-legacy  # 迁移旧版 frpmgrd 部署到 frpcmgrd
```

- [ ] **Step 2: CHANGELOG 顶部新增条目**

在 `CHANGELOG.md` 最新版本区块上方插入（不改历史）：

```markdown
## Unreleased

### Changed
- 全面对称改名：二进制/服务名 `frpmgrd` → `frpcmgrd`、环境变量前缀 `FRPMGR_` → `FRPCMGR_`、Go module → `github.com/mia-clark/frpc-manager`，与服务端 `frps-manager` 区分。

### Added
- `fmc upgrade-legacy`：一键把旧版 `frpmgrd` 部署（服务/数据/配置）迁移到新 `frpcmgrd`，幂等，可随时执行。

### Migration
- 旧部署升级后请执行一次 `fmc upgrade-legacy` 完成迁移；旧 `FRPMGR_*` 配置会自动转为 `FRPCMGR_*`。
```

- [ ] **Step 3: 全仓最终残留核对**

```bash
cd "d:/Github_Codes_mia-clark/frpc-manager"
rg "frpmgrd|FRPMGR_|frp-manager-server" -g '!docs/superpowers' -g '!CHANGELOG.md' -g '!bin' -g '!tmp' -g '!*.exe'
```
Expected: 仅剩 `fmc upgrade-legacy` 实现中**有意保留**的旧名（迁移目标 `frpmgrd` 的服务名/路径常量）；无其它残留。逐条确认每个命中都是迁移逻辑里的"旧名引用"。

- [ ] **Step 4: 全量验证**

```bash
go vet ./... && go test ./...
sh -n scripts/install.sh && echo "sh OK"
head -c 3 scripts/install.ps1 | od -An -tx1   # ef bb bf
( cd web && npm run -s build ) && echo "web OK"
```
Expected: 全部通过；BOM `ef bb bf`；前端 tsc+build 通过。

- [ ] **Step 5: Commit 文档**

```bash
git add -A
git commit -m "docs: 同步 frpc 改名说明 + CHANGELOG + fmc upgrade-legacy 用法"
```

- [ ] **Step 6: Push 触发 CI 发布并验证**

```bash
git push origin main
```
随后查 Actions：确认 Release 工作流 goreleaser 以 `project_name=frpcmgrd` 出包，release 资产名形如 `frpcmgrd_1.2.26_linux_amd64.tar.gz`，Docker 镜像 `ghcr.io/mia-clark/frpc-manager:v1.2.26` 推送成功。

- [ ] **Step 7: 迁移命令端到端验证（人工）**

在装有旧 `frpmgrd` 的真实环境：先用新脚本装 `frpcmgrd` → 执行 `fmc upgrade-legacy` → 确认旧服务消失、数据/隧道与令牌保留、新 `frpcmgrd` 正常监听、`fmc status`/`fmc info` 正确。

---

## Self-Review（已核对）

- **Spec 覆盖**：命名映射 5 维度 → Task 1（module）/Task 2（二进制·服务·目录）/Task 3（env）；迁移命令 → Task 4（两平台）；install 不碰旧部署 → 设计未改主流程，仅新增独立命令；文档/CHANGELOG → Task 5。全部有对应。
- **占位符**：无 TBD/TODO；upgrade-legacy 给出完整可运行脚本。
- **命名一致**：函数 `cmd_upgrade_legacy`（sh）/`Do-UpgradeLegacy`（ps1）；子命令字面量 `upgrade-legacy`（含下划线别名）；常量沿用现有 `SERVICE_NAME/DATA_DIR/ENV_FILE/PLIST`（sh）与 `ServiceName/DataDir/LogDir`（ps1）。
- **易漏点**：Task 1 Step 5 专门验证 ldflags 注入；每轮校验 BOM 与脚本语法。
