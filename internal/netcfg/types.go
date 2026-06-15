// Package netcfg is the network-configuration domain: DHCP servers, static DHCP
// reservations, active leases, a DHCP MAC access-control list, and static
// routes. It exposes a Service with validation + event publishing on top of a
// pluggable Backend — a real "uci" backend on OpenWrt and a "store" backend
// (JSON file + simulated leases) for development, CI and non-OpenWrt hosts.
//
// All JSON is snake_case, matching the project's Snapshot/system convention and
// deliberately avoiding the frp-era camelCase pitfalls.
package netcfg

// Backend kind identifiers.
const (
	KindUCI   = "uci"
	KindStore = "store"
)

// CustomOption is one custom DHCP option (iKuai 自定义DHCP选项).
type CustomOption struct {
	Code  int    `json:"code"`            // DHCP option number, e.g. 43, 66, 121
	Value string `json:"value"`           // option value (string / IPv4 / hex per code)
	Type  string `json:"type,omitempty"`  // "string" | "ip" | "hex" (presentation hint)
}

// DHCPServer is one DHCP address pool bound to an interface (iKuai DHCP服务端).
// Maps to an OpenWrt `config dhcp` section.
type DHCPServer struct {
	ID            string         `json:"id"`
	Interface     string         `json:"interface"`            // 服务接口（lan/lan1/...）
	Enabled       bool           `json:"enabled"`              // 状态（启用/停用）
	IPStart       string         `json:"ip_start"`             // 客户端地址-起
	IPEnd         string         `json:"ip_end"`               // 客户端地址-止
	Netmask       string         `json:"netmask"`              // 子网掩码
	Gateway       string         `json:"gateway"`              // 网关
	DNSPrimary    string         `json:"dns_primary"`          // 首选/主 DNS
	DNSSecondary  string         `json:"dns_secondary"`        // 备选/次 DNS
	LeaseMinutes  int            `json:"lease_minutes"`        // 租期（分钟）
	Exclude       []string       `json:"exclude"`              // 排除地址（每行一条）
	ExpiredKeepHours int         `json:"expired_keep_hours"`   // 过期地址保留时间（小时）
	CheckIP       bool           `json:"check_ip"`             // 检查接口 IP 有效性
	RelayOnly     bool           `json:"relay_only"`           // 只应用于 DHCP 中继
	AssocInterface string        `json:"assoc_interface"`      // 关联接口（默认 all/全部线路）
	CustomOptions []CustomOption `json:"custom_options"`       // 自定义 DHCP 选项
	Remaining     int            `json:"remaining"`            // 剩余地址（只读，计算值）
}

// StaticLease is a DHCP reservation binding a MAC to a fixed IP (iKuai DHCP静态分配).
// Maps to an OpenWrt `config host` section.
type StaticLease struct {
	ID           string `json:"id"`
	Hostname     string `json:"hostname"`       // 主机名称（可空）
	IP           string `json:"ip"`             // 绑定 IP
	MAC          string `json:"mac"`            // 绑定 MAC
	Gateway      string `json:"gateway"`        // 网关（每条可不同，dnsmasq 经 tag 下发）
	Interface    string `json:"interface"`      // 绑定接口
	DNSPrimary   string `json:"dns_primary"`
	DNSSecondary string `json:"dns_secondary"`
	Remark       string `json:"remark"`         // 备注
	Enabled      bool   `json:"enabled"`        // 状态
}

// Lease is one active DHCP lease (iKuai DHCP终端列表). Read-only; the source is
// the dnsmasq lease file / ubus on the uci backend, simulated on the store one.
type Lease struct {
	Hostname         string `json:"hostname"`
	IP               string `json:"ip"`
	MAC              string `json:"mac"`
	Expiry           int64  `json:"expiry"`            // epoch seconds (0 = infinite/static)
	RemainingSeconds int64  `json:"remaining_seconds"` // 有效时间
	Interface        string `json:"interface"`         // 绑定接口（按网段推断）
	Static           bool   `json:"static"`            // 状态：true=静态分配 false=动态分配
	Remark           string `json:"remark"`            // 命中保留时的备注
}

// ACLEntry is one MAC entry in the DHCP black/white list.
type ACLEntry struct {
	ID      string `json:"id"`
	MAC     string `json:"mac"`
	Remark  string `json:"remark"`
	Enabled bool   `json:"enabled"`
}

// ACL is the DHCP MAC access-control list (iKuai DHCP黑白名单).
type ACL struct {
	Mode    string     `json:"mode"` // "blacklist" | "whitelist"
	Entries []ACLEntry `json:"entries"`
}

// ACL modes.
const (
	ACLBlacklist = "blacklist"
	ACLWhitelist = "whitelist"
)

// Route is a static route (iKuai 静态路由). Maps to an OpenWrt
// `config route` / `config route6` section.
type Route struct {
	ID        string `json:"id"`
	Family    string `json:"family"`    // "ipv4" | "ipv6"
	Interface string `json:"interface"` // 线路："auto" 或具体接口名
	Target    string `json:"target"`    // 目的地址
	Netmask   string `json:"netmask"`   // 子网掩码（IPv4；IPv6 用 prefix）
	Prefix    int    `json:"prefix"`    // CIDR 前缀长度
	Gateway   string `json:"gateway"`   // 网关
	Metric    int    `json:"metric"`    // 优先级（越小越优先）
	Remark    string `json:"remark"`    // 备注
	Enabled   bool   `json:"enabled"`   // 状态
}

// Route families.
const (
	FamilyIPv4 = "ipv4"
	FamilyIPv6 = "ipv6"
)

// RouteEntry is one row of the live kernel routing table (iKuai 当前路由表).
type RouteEntry struct {
	Interface string `json:"interface"`
	Target    string `json:"target"`
	Netmask   string `json:"netmask"`
	Gateway   string `json:"gateway"`
	Metric    int    `json:"metric"`
}

// Interface is an L3 interface usable as a DHCP/route target (dropdown source).
type Interface struct {
	Name    string `json:"name"`
	IPv4    string `json:"ipv4"`
	Netmask string `json:"netmask"`
	Prefix  int    `json:"prefix"`
	Up      bool   `json:"up"`
}

// Status summarizes the network-config service health for the UI header.
type Status struct {
	Backend string `json:"backend"` // "uci" | "store"
	DHCPOK  bool   `json:"dhcp_ok"` // DHCP 服务端状态：服务正常
	Pending bool   `json:"pending"` // 有已保存未生效（committed 未 reload）的变更
	Message string `json:"message"`
}

// State is the full managed network configuration — the unit of export/import
// and the store backend's persisted document.
type State struct {
	DHCPServers []DHCPServer  `json:"dhcp_servers"`
	Statics     []StaticLease `json:"statics"`
	ARPBind     bool          `json:"arp_bind"`
	ACL         ACL           `json:"acl"`
	Routes      []Route       `json:"routes"`
}

// CloneState returns a deep copy of s (slices are freshly allocated).
func CloneState(s State) State {
	out := State{ARPBind: s.ARPBind}
	out.DHCPServers = append([]DHCPServer(nil), s.DHCPServers...)
	for i := range out.DHCPServers {
		out.DHCPServers[i].Exclude = append([]string(nil), s.DHCPServers[i].Exclude...)
		out.DHCPServers[i].CustomOptions = append([]CustomOption(nil), s.DHCPServers[i].CustomOptions...)
	}
	out.Statics = append([]StaticLease(nil), s.Statics...)
	out.Routes = append([]Route(nil), s.Routes...)
	out.ACL = ACL{Mode: s.ACL.Mode, Entries: append([]ACLEntry(nil), s.ACL.Entries...)}
	return out
}
