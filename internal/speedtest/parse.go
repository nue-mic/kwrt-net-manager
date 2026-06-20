package speedtest

// speedtest-go 输出解析（真机 v1.7.8 实测格式锁定，见 speedtest_test.go）。

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// reServerLine 匹配 `speedtest-go --list` 的一行节点：
//
//	[43752]    461.09km 20ms 	Beijing (China) by BJ Unicom
//	[ 5396]    855.62km Timeout 	Suzhou (China) by China Telecom JiangSu 5G
var reServerLine = regexp.MustCompile(`^\[\s*(\d+)\]\s+([\d.]+)km\s+(\S+)\s+(.+?)\s+by\s+(.+?)$`)

// reISP 匹配 `✓ ISP: 1.2.3.4 (China Unicom) [..]` 取括号内运营商。
var reISP = regexp.MustCompile(`(?i)\bISP:[^(]*\(([^)]+)\)`)

// parseServerList 解析 --list 输出为节点列表。Timeout 节点标记为不可达。
func parseServerList(out string) []Server {
	var list []Server
	for _, line := range strings.Split(out, "\n") {
		m := reServerLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		dist, _ := strconv.ParseFloat(m[2], 64)
		ping, reach := -1.0, false
		if strings.HasSuffix(m[3], "ms") {
			if v, err := strconv.ParseFloat(strings.TrimSuffix(m[3], "ms"), 64); err == nil {
				ping, reach = v, true
			}
		}
		list = append(list, Server{
			ID:         m[1],
			DistanceKm: dist,
			PingMs:     ping,
			Reachable:  reach,
			Name:       strings.TrimSpace(m[4]),
			Sponsor:    strings.TrimSpace(m[5]),
		})
	}
	return list
}

// parseISP 从 --list 输出抽取用户自身运营商。
func parseISP(out string) string {
	if m := reISP.FindStringSubmatch(out); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// parseNodeJSON 解析 `speedtest-go --server <id> --json` 的单节点结果。
// 单位换算：dl_speed/ul_speed 为 bps（÷1e6=Mbps）；latency/jitter 为纳秒（÷1e6=ms）。
func parseNodeJSON(out string) (NodeResult, error) {
	var raw struct {
		Servers []struct {
			ID       string  `json:"id"`
			Name     string  `json:"name"`
			Sponsor  string  `json:"sponsor"`
			Distance float64 `json:"distance"`
			Latency  int64   `json:"latency"`
			Jitter   int64   `json:"jitter"`
			DLSpeed  float64 `json:"dl_speed"`
			ULSpeed  float64 `json:"ul_speed"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); err != nil {
		return NodeResult{}, err
	}
	if len(raw.Servers) == 0 {
		return NodeResult{}, errors.New("无服务器结果")
	}
	sv := raw.Servers[0]
	return NodeResult{
		ID:           sv.ID,
		Name:         sv.Name,
		Sponsor:      sv.Sponsor,
		DistanceKm:   sv.Distance,
		DownloadMbps: round2(sv.DLSpeed / 1e6),
		UploadMbps:   round2(sv.ULSpeed / 1e6),
		PingMs:       round2(float64(sv.Latency) / 1e6),
		JitterMs:     round2(float64(sv.Jitter) / 1e6),
	}, nil
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// pickRecommended 在 servers 上标记智能默认勾选：优先国内三大运营商（联通/电信/移动，
// 各取就近一个，覆盖主流线路），不足 n 再用剩余可达节点里**延迟最低**的补（而非单纯最近，
// 避免推出高延迟的境外节点）。直接改入参元素的 Recommended。
func pickRecommended(servers []Server, n int) {
	reach := make([]int, 0, len(servers))
	for i := range servers {
		servers[i].Recommended = false
		if servers[i].Reachable {
			reach = append(reach, i)
		}
	}
	byDist := append([]int(nil), reach...)
	sort.SliceStable(byDist, func(a, b int) bool {
		return servers[byDist[a]].DistanceKm < servers[byDist[b]].DistanceKm
	})
	used := make(map[int]bool, n)
	picked := 0
	pick := func(i int) {
		used[i] = true
		servers[i].Recommended = true
		picked++
	}
	// pass1：国内三大运营商桶，各取就近一个。
	for _, bucket := range []string{"unicom", "telecom", "mobile"} {
		if picked >= n {
			break
		}
		for _, i := range byDist {
			if !used[i] && ispBucket(servers[i].Sponsor) == bucket {
				pick(i)
				break
			}
		}
	}
	// pass2：不足则补——优先「本国」（取最近节点所在国家）节点，再按延迟最低。
	if picked < n {
		home := ""
		if len(byDist) > 0 {
			home = countryOf(servers[byDist[0]].Name)
		}
		rem := make([]int, 0, len(reach))
		for _, i := range reach {
			if !used[i] {
				rem = append(rem, i)
			}
		}
		sort.SliceStable(rem, func(a, b int) bool {
			ha := home != "" && countryOf(servers[rem[a]].Name) == home
			hb := home != "" && countryOf(servers[rem[b]].Name) == home
			if ha != hb {
				return ha // 本国优先
			}
			return servers[rem[a]].PingMs < servers[rem[b]].PingMs
		})
		for _, i := range rem {
			if picked >= n {
				break
			}
			pick(i)
		}
	}
}

// countryOf 从节点名 "Beijing (China)" 提取国家 "China"；无括号返回 ""。
func countryOf(name string) string {
	l := strings.LastIndex(name, "(")
	r := strings.LastIndex(name, ")")
	if l >= 0 && r > l {
		return strings.TrimSpace(name[l+1 : r])
	}
	return ""
}

// ispBucket 把 sponsor 归到运营商桶（中国三大 + 其它按名分桶）。
func ispBucket(sponsor string) string {
	s := strings.ToLower(sponsor)
	switch {
	case strings.Contains(s, "unicom") || strings.Contains(sponsor, "联通"):
		return "unicom"
	case strings.Contains(s, "telecom") || strings.Contains(sponsor, "电信"):
		return "telecom"
	case strings.Contains(s, "mobile") || strings.Contains(s, "cmcc") || strings.Contains(sponsor, "移动"):
		return "mobile"
	default:
		return "other:" + s
	}
}
