package logcenter

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// busybox logread 行：
//
//	"Tue Jun 16 22:05:20 2026 daemon.err kwrtmgrd[20160]: <msg>"
//
// 组：1=时间  2=facility.level  3=进程标识(proc[pid])  4=消息。
var reSyslog = regexp.MustCompile(`^(\w{3} \w{3} +\d{1,2} \d{2}:\d{2}:\d{2} \d{4}) +(\S+) +([^:]+): (.*)$`)

// readSyslog 跑 logread，按源过滤并解析。
func (c *Center) readSyslog(source string) []Entry {
	out, err := c.run.Run("logread")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	var res []Entry
	for _, line := range strings.Split(out, "\n") {
		e, ok := parseSyslogLine(line, c.loc)
		if !ok {
			continue
		}
		switch source {
		case SourceDHCP:
			if !isDHCPLine(e) {
				continue
			}
			enrichDHCP(&e)
		case SourceDialup:
			if !isDialupLine(e) {
				continue
			}
			enrichDial(&e) // 富化诊断 + 接口名，让历史查询/导出也带「拨号状态/原因/建议」
		}
		res = append(res, e)
	}
	return res
}

func parseSyslogLine(line string, loc *time.Location) (Entry, bool) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return Entry{}, false
	}
	m := reSyslog.FindStringSubmatch(line)
	if m == nil {
		return Entry{}, false // 非标准格式行（如内核 [ 12.3] 前缀）直接跳过，避免误解析
	}
	ts := parseSyslogTime(m[1], loc)
	level := m[2]
	if i := strings.LastIndexByte(level, '.'); i >= 0 {
		level = level[i+1:]
	}
	proc := m[3]
	if i := strings.IndexByte(proc, '['); i >= 0 {
		proc = proc[:i] // 去掉 [pid]
	}
	return Entry{
		Time:    ts.Format("2006-01-02 15:04:05"),
		TS:      ts.Unix(),
		Level:   level,
		Proc:    strings.TrimSpace(proc),
		Message: strings.TrimSpace(m[4]),
	}, true
}

// parseSyslogTime 解析 "Mon Jan _2 15:04:05 2006"（日可能空格补齐），按系统时区。失败回退当前时间。
func parseSyslogTime(s string, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	for _, layout := range []string{"Mon Jan _2 15:04:05 2006", "Mon Jan 2 15:04:05 2006"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t
		}
	}
	return time.Now()
}

func isDHCPLine(e Entry) bool {
	p := e.Proc
	if strings.Contains(p, "dnsmasq-dhcp") || strings.Contains(p, "odhcpd") {
		return true
	}
	return strings.Contains(p, "dnsmasq") && reDHCPType.MatchString(e.Message)
}

var (
	reDHCPType = regexp.MustCompile(`\b(DHCP[A-Z]+|SOLICIT|ADVERTISE|REQUEST|RENEW|REBIND|REPLY|RELEASE|CONFIRM|DECLINE|INFORMATION-REQUEST)\b`)
	reIface    = regexp.MustCompile(`\(([^)]+)\)`)
	reMAC      = regexp.MustCompile(`([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}`)
	reIPv4     = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)
	reIPv6     = regexp.MustCompile(`\b([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}(/\d{1,3})?\b`)
)

// enrichDHCP 从消息里抽出 报文类型/接口/MAC/IP，best-effort。
func enrichDHCP(e *Entry) {
	msg := e.Message
	if m := reDHCPType.FindString(msg); m != "" {
		e.Type = m
	}
	if m := reIface.FindStringSubmatch(msg); m != nil {
		e.Iface = m[1]
	}
	if m := reMAC.FindString(msg); m != "" {
		e.MAC = strings.ToLower(m)
	}
	if m := reIPv4.FindString(msg); m != "" {
		e.IP = m
	} else if m := reIPv6.FindString(msg); m != "" && strings.Contains(m, ":") {
		e.IP = m
	}
}

var reDialup = regexp.MustCompile(`(?i)pppd|pppoe|ip-up|ip-down|udhcpc|netifd`)

func isDialupLine(e Entry) bool {
	return reDialup.MatchString(e.Proc) || reDialup.MatchString(e.Message)
}

// readDDNS 读 ddns-scripts 的 /var/log/ddns/*.log（每文件＝一条 DDNS 配置）。
func (c *Center) readDDNS() []Entry {
	files, _ := filepath.Glob("/var/log/ddns/*.log")
	var res []Entry
	for _, fp := range files {
		name := strings.TrimSuffix(filepath.Base(fp), ".log")
		data, err := os.ReadFile(fp)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			ts, msg := parseDDNSLine(line, c.loc)
			res = append(res, Entry{
				Time:    ts.Format("2006-01-02 15:04:05"),
				TS:      ts.Unix(),
				Proc:    name,
				Message: msg,
			})
		}
	}
	return res
}

// ddns-scripts 日志行形如：
//
//	"20260616 22:00:00  INFO : ..."  或  "Mon Jun 16 22:00:00 CST 2026: ..."
//
// 这里尽量抽出前导时间，抽不到就整行作消息、时间取现在。
var reDDNSDate = regexp.MustCompile(`^(\d{8} \d{2}:\d{2}:\d{2})`)

func parseDDNSLine(line string, loc *time.Location) (time.Time, string) {
	if loc == nil {
		loc = time.Local
	}
	if m := reDDNSDate.FindStringSubmatch(line); m != nil {
		if t, err := time.ParseInLocation("20060102 15:04:05", m[1], loc); err == nil {
			return t, strings.TrimSpace(strings.TrimPrefix(line, m[1]))
		}
	}
	return time.Now(), line
}
