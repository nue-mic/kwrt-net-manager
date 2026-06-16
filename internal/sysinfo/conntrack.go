package sysinfo

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Flow 是一条内核 conntrack 连接（仿爱快「连接详情」逐条明细）。
type Flow struct {
	Family  string `json:"family"`  // ipv4 | ipv6
	Proto   string `json:"proto"`   // tcp | udp | icmp | ...
	Src     string `json:"src"`     // 源 ip[:port]
	Dst     string `json:"dst"`     // 目标 ip[:port]
	Packets uint64 `json:"packets"` // 双向总包数
	Bytes   uint64 `json:"bytes"`   // 双向总字节（需开启 nf_conntrack_acct）
}

// ConnFlowResult 是连接明细查询结果。
type ConnFlowResult struct {
	Flows         []Flow `json:"flows"`
	Total         int    `json:"total"`          // 当前 conntrack 总条数
	AcctAvailable bool   `json:"acct_available"` // 是否有字节计数（未开启 acct 则 false）
}

var knownProtos = map[string]bool{
	"tcp": true, "udp": true, "icmp": true, "icmpv6": true, "udplite": true,
	"sctp": true, "dccp": true, "gre": true, "unknown": true,
}

// ConnFlows 解析 /proc/net/nf_conntrack，返回按流量(字节)降序的前 limit 条连接。
// OpenWrt(做 NAT/防火墙)原生即有 conntrack；字节/包计数需 net.netfilter.nf_conntrack_acct=1。
// 非 Linux 或无该文件时返回空（不报错），便于开发/CI。
func ConnFlows(limit int) (ConnFlowResult, error) {
	res := ConnFlowResult{Flows: []Flow{}}
	f, err := os.Open("/proc/net/nf_conntrack")
	if err != nil {
		return res, nil // 文件不存在（非 Linux / 模块未载）→ 空结果，不视为错误
	}
	defer f.Close()

	var all []Flow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		fl, ok := parseConntrackLine(sc.Text())
		if !ok {
			continue
		}
		all = append(all, fl)
		if fl.Bytes > 0 {
			res.AcctAvailable = true
		}
	}
	res.Total = len(all)
	// 按字节降序；无 acct 时退化为按包数降序。
	sort.Slice(all, func(i, j int) bool {
		if all[i].Bytes != all[j].Bytes {
			return all[i].Bytes > all[j].Bytes
		}
		return all[i].Packets > all[j].Packets
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	res.Flows = all
	return res, nil
}

// parseConntrackLine 解析一行 nf_conntrack，例如：
//
//	ipv4 2 tcp 6 431999 ESTABLISHED src=192.168.1.219 dst=192.168.1.12 sport=1254 dport=8443 \
//	  packets=415 bytes=931000 src=192.168.1.12 dst=192.168.1.219 sport=8443 dport=1254 \
//	  packets=300 bytes=50000 [ASSURED] mark=0 use=1
//
// 取「原始方向」的 src/dst/sport/dport 作展示，packets/bytes 累加双向。
func parseConntrackLine(line string) (Flow, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return Flow{}, false
	}
	var fl Flow
	// 家族：首字段为 ipv4/ipv6（部分内核省略，回退按地址推断）。
	if fields[0] == "ipv4" || fields[0] == "ipv6" {
		fl.Family = fields[0]
	}
	// 协议名：扫描已知协议名（通常在第 3 段）。
	for _, t := range fields {
		if knownProtos[t] {
			fl.Proto = t
			break
		}
	}
	var sip, dip, sport, dport string
	for _, t := range fields {
		eq := strings.IndexByte(t, '=')
		if eq <= 0 {
			continue
		}
		k, v := t[:eq], t[eq+1:]
		switch k {
		case "src":
			if sip == "" {
				sip = v
			}
		case "dst":
			if dip == "" {
				dip = v
			}
		case "sport":
			if sport == "" {
				sport = v
			}
		case "dport":
			if dport == "" {
				dport = v
			}
		case "packets":
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				fl.Packets += n
			}
		case "bytes":
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				fl.Bytes += n
			}
		}
	}
	if sip == "" || dip == "" {
		return Flow{}, false
	}
	if fl.Family == "" {
		if strings.Contains(sip, ":") {
			fl.Family = "ipv6"
		} else {
			fl.Family = "ipv4"
		}
	}
	fl.Src = joinHostPort(sip, sport)
	fl.Dst = joinHostPort(dip, dport)
	return fl, true
}

func joinHostPort(ip, port string) string {
	if port == "" {
		return ip
	}
	if strings.Contains(ip, ":") { // IPv6 加方括号
		return "[" + ip + "]:" + port
	}
	return ip + ":" + port
}
