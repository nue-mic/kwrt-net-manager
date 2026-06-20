package netutil

import (
	_ "embed"
	"strings"
	"sync"
)

// OUI 厂商识别：由 MAC 前 3 字节（OUI）查厂商名。
//
// 数据来自精选子集 oui_data.txt（虚拟化 + 常见消费/网络/IoT 厂商），
// 未知前缀返回空字符串。数据文件可后续替换为完整 IEEE MA-L 注册表。

//go:embed oui_data.txt
var ouiRaw string

var (
	ouiOnce  sync.Once
	ouiTable map[string]string
)

func loadOUI() {
	ouiTable = make(map[string]string, 512)
	for _, line := range strings.Split(ouiRaw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 第一段是前缀，其余是厂商名。
		sp := strings.IndexAny(line, " \t")
		if sp < 0 {
			continue
		}
		prefix := strings.ToUpper(strings.TrimSpace(line[:sp]))
		vendor := strings.TrimSpace(line[sp+1:])
		if len(prefix) != 6 || vendor == "" || !isHex6(prefix) {
			continue
		}
		ouiTable[prefix] = vendor
	}
}

func isHex6(s string) bool {
	if len(s) != 6 {
		return false
	}
	for i := 0; i < 6; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// Vendor 返回 mac 对应的厂商名，未知返回 ""。大小写/分隔符不敏感。
func Vendor(mac string) string {
	ouiOnce.Do(loadOUI)
	norm := NormalizeMAC(mac) // 形如 AA:BB:CC:DD:EE:FF（大写），非法则 ""
	if norm == "" {
		return ""
	}
	// 取前 3 段拼成 6 位前缀。
	prefix := strings.ToUpper(strings.ReplaceAll(norm, ":", ""))
	if len(prefix) < 6 {
		return ""
	}
	return ouiTable[prefix[:6]]
}
