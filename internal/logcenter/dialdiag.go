package logcenter

import (
	"regexp"
	"strings"
)

// 拨号诊断三态（与接口运行态 connected/connecting 语义对齐）。
const (
	dialConnecting = "connecting"
	dialConnected  = "connected"
	dialFailed     = "failed"
)

// dialPattern 把一条拨号日志的特征子串映射成诊断。按 dialPatterns 声明顺序匹配，
// 命中第一个即采用——务必「先具体后泛化」（成功标志最先，泛化信息态最后）。
type dialPattern struct {
	sub       string
	phase     string
	state     string
	severity  string
	diagnosis string
	advice    string
}

// dialPatterns 是 pppd/pppoe/netifd 拨号全生命周期的诊断表。子串按真实 pppd 2.5.1 文案给出。
var dialPatterns = []dialPattern{
	// —— 成功标志（最优先：拿到 IP 即判定已连接，覆盖此前的失败行）——
	{"local  IP address", "ipcp", dialConnected, "success",
		"已分配到本端 IP，IPCP 协商完成，拨号成功。",
		"若仍无法上网，转查 DNS / 路由 / NAT（防火墙 WAN 区）。"},
	{"local IP address", "ipcp", dialConnected, "success",
		"已分配到本端 IP，IPCP 协商完成，拨号成功。",
		"若仍无法上网，转查 DNS / 路由 / NAT（防火墙 WAN 区）。"},
	// —— 认证（成功在前、失败在后）——
	{"CHAP authentication succeeded", "auth", dialConnecting, "info",
		"CHAP 认证通过，账号密码正确。", ""},
	{"PAP authentication succeeded", "auth", dialConnecting, "info",
		"PAP 认证通过，账号密码正确。", ""},
	{"CHAP authentication failed", "auth", dialFailed, "error",
		"CHAP 认证失败，账号或密码不正确。",
		"逐字核对宽带账号（含 @后缀/区号）与密码；确认未欠费、未被锁定。"},
	{"PAP authentication failed", "auth", dialFailed, "error",
		"PAP 认证失败，账号或密码不正确。",
		"逐字核对宽带账号与密码；确认未欠费、未被锁定。"},
	{"peer refused to authenticate", "auth", dialFailed, "error",
		"对端拒绝认证，多为账号状态异常（欠费 / 被锁 / 被他设备占用）。",
		"确认账号未欠费、未被锁、未在他处登录；必要时联系运营商。"},
	{"Failed to authenticate ourselves to peer", "auth", dialFailed, "error",
		"向对端认证失败，账号密码被拒。",
		"核对账号密码后缀与大小写；确认线路已开通。"},
	{"Authentication failed", "auth", dialFailed, "error",
		"PPP 认证未通过，拨号被拒。",
		"检查账号密码、是否欠费 / 锁定；允许 PAP/CHAP 自适应。"},
	// —— 发现阶段（PPPoE Discovery：PADI/PADO/PADR/PADS）——
	{"Timeout waiting for PADO", "discovery", dialFailed, "error",
		"广播 PADI 后超时未收到任何 PADO 应答，线路上探测不到 PPPoE 服务器。",
		"依次排查：光猫是否桥接 / 在线、网线与 WAN 口、运营商 VLAN；可先切 DHCP 验证物理链路。"},
	{"Timeout waiting for PADS", "discovery", dialFailed, "error",
		"收到 PADO 并发出 PADR，但局端不回 PADS，PPPoE 会话未建立。",
		"多为 Service-Name 不匹配或局端拒绝 / 会话满；清空服务名(service) / AC 后重拨。"},
	{"Unable to complete PPPoE Discovery", "discovery", dialFailed, "error",
		"PPPoE 发现阶段四步握手未完成，链路建立前即终止。",
		"对端无 PPPoE 服务器 / VLAN 不对 / Service-Name 不符；先用 DHCP 验证物理链路。"},
	// —— LCP / IPCP 协商超时 ——
	{"LCP: timeout sending Config-Requests", "auth", dialFailed, "error",
		"LCP 链路协商超时，对端无回应。",
		"可能 MRU 过大或对端会话异常；尝试把 MTU 设为 1492 后重拨。"},
	{"IPCP: timeout sending Config-Requests", "ipcp", dialFailed, "error",
		"认证已通过但 IPCP 拿不到 IP 地址。",
		"局端会话异常或 IP 池耗尽；稍后重拨，频繁出现请联系运营商。"},
	{"Peer is not authorized to use remote address", "ipcp", dialFailed, "error",
		"局端拒绝本端请求的固定地址。",
		"移除接口里写死的固定 IP，改用对端动态下发。"},
	// —— 掉线 / 拆链 ——
	{"No response to", "teardown", dialFailed, "warning",
		"多次 LCP echo 无响应，链路疑似中断。",
		"检查线路稳定性 / 光衰；netifd 通常会自动重拨。"},
	{"LCP terminated by peer", "teardown", dialFailed, "warning",
		"对端主动终止链路（强制下线 / 账号他处登录 / 欠费）。",
		"确认账号是否被他处占用、是否欠费或被定时踢线。"},
	{"Modem hangup", "teardown", dialFailed, "warning",
		"底层链路挂断，连接中断。",
		"检查物理线路 / 光猫；netifd 通常会自动重拨。"},
	{"Serial link appears to be disconnected", "teardown", dialFailed, "warning",
		"链路看起来已断开，pppd 将退出重试。",
		"检查网线 / 光猫与运营商线路。"},
	{"Connection terminated", "teardown", dialFailed, "warning",
		"PPP 连接已终止。",
		"查看上文具体原因；netifd 通常会自动重拨。"},
	// —— 进行中的信息态（泛化，放最后）——
	{"Connect: ppp", "established", dialConnecting, "info",
		"PPP 链路已建立，进入协商阶段…", ""},
	{"Send PPPOE Discovery", "discovery", dialConnecting, "info",
		"正在广播 PADI 探测 PPPoE 服务器…", ""},
	{"PADS", "discovery", dialConnecting, "info",
		"收到 PADS，PPPoE 会话已建立。", ""},
	{"started by", "other", dialConnecting, "info",
		"pppd 已启动，开始拨号…", ""},
}

// classifyDial 富化一条拨号 Entry：按模式表命中第一个子串，填 phase/state/severity/diagnosis/advice。
// 未命中诊断表时，按 syslog 级别粗判 severity（供前端「仅看错误」过滤），其余字段留空。
func classifyDial(e *Entry) {
	for _, p := range dialPatterns {
		if strings.Contains(e.Message, p.sub) {
			e.Phase, e.DialState, e.Severity, e.Diagnosis, e.Advice = p.phase, p.state, p.severity, p.diagnosis, p.advice
			return
		}
	}
	switch e.Level {
	case "err", "crit", "alert", "emerg":
		e.Severity = "error"
	case "warn", "warning":
		e.Severity = "warning"
	}
}

// reDialIface 抽接口名：pppoe-wan / ppp0 等设备名。
var reDialIface = regexp.MustCompile(`\b(pppoe-\w+|ppp\d+)\b`)

// reIfaceQuoted / reIfaceProc 抽 netifd 行里的逻辑接口名：
// "Interface 'wan1' is now up" / "wan1 (7949): ..."。
var (
	reIfaceQuoted = regexp.MustCompile(`Interface '([^']+)'`)
	reIfaceProc   = regexp.MustCompile(`^(\w+) \(\d+\):`)
)

// extractDialIface best-effort 从 message/proc 抽出拨号接口名（设备名优先，其次 netifd 逻辑名）。
func extractDialIface(e *Entry) {
	if e.Iface != "" {
		return
	}
	if m := reDialIface.FindString(e.Message); m != "" {
		e.Iface = m
		return
	}
	if m := reIfaceQuoted.FindStringSubmatch(e.Message); m != nil {
		e.Iface = m[1]
		return
	}
	if m := reIfaceProc.FindStringSubmatch(e.Message); m != nil {
		e.Iface = m[1]
	}
}

// enrichDial 是 dialup 源 / 实时拨号流的统一富化入口：诊断 + 接口名。
func enrichDial(e *Entry) {
	classifyDial(e)
	extractDialIface(e)
}

// DialDiagnosis 是「拨号结论横幅」聚合，供前端打开拨号日志时立即给出当前结论。
type DialDiagnosis struct {
	Iface     string `json:"iface,omitempty"`
	DialState string `json:"dial_state"` // connecting|connected|failed|unknown
	Phase     string `json:"phase,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Headline  string `json:"headline"`
	Diagnosis string `json:"diagnosis,omitempty"`
	Advice    string `json:"advice,omitempty"`
	MatchLine string `json:"matched_line,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// Diagnose 扫描最近的拨号日志快照给出结论：最近一条 success 覆盖一切（已连接），
// 否则取最近一条 error/warning；都没有则按最近 connecting 显示「拨号中」，再无则 unknown。
// 非 OpenWrt(无 logread)下快照为空 → 返回 unknown，由前端实时流补足。
func (c *Center) Diagnose(iface string) DialDiagnosis {
	entries := c.readSyslog(SourceDialup) // 已 enrichDial，旧→新顺序
	var connecting *Entry
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if iface != "" && e.Iface != "" && !strings.Contains(e.Iface, iface) {
			continue
		}
		if e.Diagnosis != "" && (e.Severity == "success" || e.Severity == "error" || e.Severity == "warning") {
			return dialConcl(e)
		}
		if connecting == nil && e.DialState == dialConnecting {
			ec := e
			connecting = &ec
		}
	}
	if connecting != nil {
		return DialDiagnosis{
			Iface: connecting.Iface, DialState: dialConnecting, Phase: connecting.Phase,
			Severity: "info", Headline: "拨号进行中…", Diagnosis: connecting.Diagnosis, UpdatedAt: connecting.Time,
		}
	}
	return DialDiagnosis{DialState: "unknown", Headline: "暂无拨号记录"}
}

func dialConcl(e Entry) DialDiagnosis {
	var head string
	switch e.Severity {
	case "success":
		head = "拨号成功：已获取 IP，连接已建立"
	case "error":
		head = "拨号失败：" + firstClause(e.Diagnosis)
	case "warning":
		head = "连接异常：" + firstClause(e.Diagnosis)
	default:
		head = e.Diagnosis
	}
	state := e.DialState
	if state == "" {
		state = "unknown"
	}
	return DialDiagnosis{
		Iface: e.Iface, DialState: state, Phase: e.Phase, Severity: e.Severity,
		Headline: head, Diagnosis: e.Diagnosis, Advice: e.Advice, MatchLine: e.Message, UpdatedAt: e.Time,
	}
}

// firstClause 取首个分句（到第一个句号/逗号），让横幅标题简短。
func firstClause(s string) string {
	for _, sep := range []string{"。", "，", ","} {
		if i := strings.Index(s, sep); i > 0 {
			return s[:i]
		}
	}
	return s
}
