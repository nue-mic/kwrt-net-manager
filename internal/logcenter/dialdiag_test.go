package logcenter

import "testing"

// 拨号诊断模式表：喂真机原文行，断言 phase/state/severity/diagnosis 富化正确。
func TestClassifyDial(t *testing.T) {
	cases := []struct {
		name             string
		level, msg       string
		wantState        string
		wantSeverity     string
		wantPhase        string
		wantDiagNonEmpty bool
	}{
		{"PADO 超时", "warn", "Timeout waiting for PADO packets", dialFailed, "error", "discovery", true},
		{"发现阶段失败", "err", "Unable to complete PPPoE Discovery phase 1", dialFailed, "error", "discovery", true},
		{"PADS 超时", "err", "Timeout waiting for PADS packets", dialFailed, "error", "discovery", true},
		{"CHAP 认证失败", "err", "CHAP authentication failed", dialFailed, "error", "auth", true},
		{"PAP 认证失败", "err", "PAP authentication failed", dialFailed, "error", "auth", true},
		{"对端拒绝认证", "err", "peer refused to authenticate", dialFailed, "error", "auth", true},
		{"IPCP 超时", "err", "IPCP: timeout sending Config-Requests", dialFailed, "error", "ipcp", true},
		{"对端掉线", "warn", "LCP terminated by peer", dialFailed, "warning", "teardown", true},
		{"拿到本端 IP=成功", "notice", "local  IP address 100.64.12.34", dialConnected, "success", "ipcp", true},
		{"CHAP 认证成功", "info", "CHAP authentication succeeded", dialConnecting, "info", "auth", false},
		{"pppd 启动", "notice", "pppd 2.5.1 started by root, uid 0", dialConnecting, "info", "other", false},
		{"广播 PADI", "info", "Send PPPOE Discovery V1T1 PADI packet", dialConnecting, "info", "discovery", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := Entry{Level: c.level, Message: c.msg}
			classifyDial(&e)
			if e.DialState != c.wantState {
				t.Errorf("state=%q want %q", e.DialState, c.wantState)
			}
			if e.Severity != c.wantSeverity {
				t.Errorf("severity=%q want %q", e.Severity, c.wantSeverity)
			}
			if e.Phase != c.wantPhase {
				t.Errorf("phase=%q want %q", e.Phase, c.wantPhase)
			}
			if c.wantDiagNonEmpty && e.Diagnosis == "" {
				t.Errorf("diagnosis 为空，期望有诊断文案")
			}
		})
	}
}

// 优先级冲突：成功标志(local IP) 必须先于任何认证/发现行命中；succeeded 必须先于 failed 的泛化。
func TestClassifyDialPriority(t *testing.T) {
	// "CHAP authentication succeeded" 不能被泛化的 "Authentication failed" 抢匹配。
	e := Entry{Message: "CHAP authentication succeeded"}
	classifyDial(&e)
	if e.Severity != "info" || e.DialState != dialConnecting {
		t.Fatalf("succeeded 被误判为失败：severity=%q state=%q", e.Severity, e.DialState)
	}
	// 未命中诊断表但级别为 err → severity=error（供「仅看错误」过滤），无诊断文案。
	e2 := Entry{Level: "err", Message: "sent [LCP ConfReq id=0x1]"}
	classifyDial(&e2)
	if e2.Severity != "error" || e2.Diagnosis != "" {
		t.Errorf("未分类错误行：severity=%q diagnosis=%q want error/空", e2.Severity, e2.Diagnosis)
	}
}

func TestExtractDialIface(t *testing.T) {
	cases := []struct{ msg, want string }{
		{"Connect: ppp0 <--> eth1", "ppp0"},
		{"Interface 'wan1' is now up", "wan1"},
		{"wan1 (7949): uci: Entry not found", "wan1"},
		{"Timeout waiting for PADO packets", ""}, // 纯 pppd 发现行无接口名
	}
	for _, c := range cases {
		e := Entry{Message: c.msg}
		extractDialIface(&e)
		if e.Iface != c.want {
			t.Errorf("msg=%q iface=%q want %q", c.msg, e.Iface, c.want)
		}
	}
}
