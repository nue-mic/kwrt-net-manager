# 设计文档：install 智能代理下载（按优先级 fallback）

> 日期：2026-06-05
> 状态：已批准
> 作者：rthink + Claude

## 1. 背景与目标

`scripts/install.sh:369` 与 `scripts/install.ps1:297` 当前直连 `https://github.com/.../releases/download/...` 下载二进制压缩包；
国内/弱网用户经常超时或慢得难受。

**目标**：内置一份按优先级排好的 GitHub release 代理候选数组，安装/升级时挨个试，第一个能成功**下载并解开**的就采用；
全部代理失败再回落直连，直连也挂才报错。两端（sh + ps1）行为对称。

## 2. 候选代理（用户指定顺序：公开 4 家在前，自建 6 家在后）

> 用户决策：公开代理放前面优先消耗，自建放后面作为保底（保护自建服务器带宽）。
> 实测速度仅作参考，不决定顺序。

| 优先级 | 代理 | 实测速度 (7.3 MB) | 来源 |
|:-:|---|---|---|
| 1 | `https://gh-proxy.com/` | 1.43s | 公开 |
| 2 | `https://ghfast.top/` | 1.93s | 公开 |
| 3 | `https://github.tbedu.top/` | 2.66s | 公开 |
| 4 | `https://gh.idayer.com/` | 2.76s | 公开 |
| 5 | `https://docker.srv1.qzz.io/` | 0.613s | 用户自建 |
| 6 | `https://dk-proxy.srv1.qzz.io/` | 0.616s | 用户自建 |
| 7 | `https://dk-proxy.966788.xyz/` | 0.619s | 用户自建 |
| 8 | `https://dk-proxy.srv0.qzz.io/` | 0.620s | 用户自建 |
| 9 | `https://docker.srv0.qzz.io/` | 1.379s | 用户自建 |
| 10 | `https://docker.966788.xyz/` | 1.440s | 用户自建 |

**URL 拼装格式**（统一）：`${PROXY}https://github.com/USER/REPO/releases/download/...`
（即把完整 GitHub URL 当作 path 拼到代理域名后；moeyy 风格 `/https/...` 不支持。）

**剔除**：
- `dk-proxy.988669.xyz` 实测 404，待用户检查该 server 的路由后再决定是否回收
- 其它实测不通的公开代理（mirror.ghproxy.com / ghps.cc / gh.con.sh 等）不入

## 3. 下载流程伪码

```
for proxy in 候选 1..10:
    target_url = ${proxy}${github_url}
    curl -fsSL --max-time 30 -o $tmp $target_url
    if 失败: 继续
    if !验证($tmp): 继续         # 见 §4
    return $tmp                  # ✅ 命中
回落: 直连 $github_url, 同样验证
全部失败: die "下载失败, 请手动下载 $github_url"
```

每个候选独立超时 30s；全 10 个代理 + 1 次直连最坏 ~5 分钟，但正常路径首家就命中（< 1s）。

## 4. 文件验证（防"伪 200"）

代理服务器有时返回 HTTP 200 但 body 是 HTML 错误页（实测 ghproxy.com 返回 1797 字节那种）。
**仅靠 HTTP 状态码或 Content-Length 不够准**。验证方式：

- Linux/macOS（install.sh，下载 tar.gz）：`tar -tzf "$file" >/dev/null 2>&1`
- Windows（install.ps1，下载 zip）：`Expand-Archive -Path $file -DestinationPath $tmp -Force` 试解到临时目录

能解开 → 真包；解不开 → 伪 200，丢弃 + 试下一个。

## 5. 用户覆盖通道

| 接口 | 行为 |
|---|---|
| 环境变量 `FRPCMGR_DOWNLOAD_PROXY=https://my.mirror/` | 强制只用这一个候选（私有代理 / 调试） |
| 环境变量 `FRPCMGR_NO_PROXY=1` | 跳过所有代理，直连 GitHub |
| 命令行 `--proxy URL` | 等价于设 `FRPCMGR_DOWNLOAD_PROXY` |
| 命令行 `--no-proxy` | 等价于 `FRPCMGR_NO_PROXY=1` |

优先级：命令行 > 环境变量 > 内置数组。

## 6. 影响面

- **`scripts/install.sh`**
  - 顶部声明候选数组 `DL_PROXIES`
  - 新增辅助函数 `try_download <github_url> <dest>` 实现遍历 + tar 验证
  - `download_and_install`（约 line 366）改用 `try_download` 替代 `fetch_file`
  - `parse_args` 增加 `--proxy` / `--no-proxy` 解析（line 104）
- **`scripts/install.ps1`**
  - 顶部声明 `$DlProxies`
  - `Install-Binary` 改用对应的 try-download 循环 + `Expand-Archive` 验证
  - 参数 `-Proxy <URL>` / `-NoProxy` 透传
- **`fmc` 命令面板**：不动（按用户决策，不加 test-mirrors 子命令）
- **README / About.tsx 文档**：在"快速部署"段补一条说明"下载源自动选择最快代理，可用 `FRPCMGR_DOWNLOAD_PROXY` 强制指定"
- **update 路径自动受益**：`fmc update` / `--update` 都走 `download_and_install`，无需额外改动

## 7. 范围之外（YAGNI）

- 不做安装时并行测速（用户已选写死优先级 + 自动 fallback）
- 不做远程拉代理列表（避免对某第三方端点的硬依赖）
- 不加 `fmc test-mirrors` 子命令（用户保留以后再说）
- 不做镜像延迟统计/反馈机制

## 8. 验证策略

四个场景：

1. **主代理通**（正常）：插管运行 install.sh，确认日志显示首家命中、用时 < 1s
2. **首家挂，二家通**：临时 hosts 文件把 `docker.srv1.qzz.io` 指到 127.0.0.1，确认自动跳过到 `dk-proxy.srv1.qzz.io`
3. **全代理挂**：把 10 个域名都 hosts 到 127.0.0.1，确认最终回落直连 github.com
4. **`FRPCMGR_NO_PROXY=1`**：确认完全跳过代理段，直接走 github.com
5. **伪 200 验证**：把 `FRPCMGR_DOWNLOAD_PROXY=https://example.com/`（这家肯定返回 HTML 200），确认 tar 验证失败、丢弃、回落到下一家或直连

另：`sh -n scripts/install.sh` 通过；`install.ps1` 头三字节仍是 `ef bb bf`（UTF-8 BOM）；前端 `tsc -b && vite build` 通过（如果有 About.tsx 文案改动）。

## 9. 维护性

候选数组顶部加注释，说明：
- 数据来源（2026-06-05 实测）
- 后续怎么补测（一行 `bash .claude/skills/...` 或者文档里贴的 oneliner）
- 待回收：`dk-proxy.988669.xyz` 404 已知，修复后追加进数组
