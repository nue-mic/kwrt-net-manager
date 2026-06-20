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
	Code  int    `json:"code"`           // DHCP option number, e.g. 43, 66, 121
	Value string `json:"value"`          // option value (string / IPv4 / hex per code)
	Type  string `json:"type,omitempty"` // "string" | "ip" | "hex" (presentation hint)
}

// DHCPServer is one DHCP address pool bound to an interface (iKuai DHCP服务端).
// Maps to an OpenWrt `config dhcp` section.
type DHCPServer struct {
	ID            string         `json:"id"`
	Interface     string         `json:"interface"`      // 服务接口（lan/lan1/...）
	Enabled       bool           `json:"enabled"`        // 状态（启用/停用）
	IPStart       string         `json:"ip_start"`       // 客户端地址-起
	IPEnd         string         `json:"ip_end"`         // 客户端地址-止
	Force         bool           `json:"force"`          // 强制下发：即便探测到本网段已有 DHCP 服务器也强制服务（默认开，旁路由必备）
	Netmask       string         `json:"netmask"`        // 子网掩码
	Gateway       string         `json:"gateway"`        // 网关
	DNSPrimary    string         `json:"dns_primary"`    // 首选/主 DNS
	DNSSecondary  string         `json:"dns_secondary"`  // 备选/次 DNS
	LeaseMinutes  int            `json:"lease_minutes"`  // 租期（分钟）
	Exclude       []string       `json:"exclude"`        // 排除地址（每行一条，原生经占位 host 保留实现）
	CustomOptions []CustomOption `json:"custom_options"` // 自定义 DHCP 选项
	Remaining     int            `json:"remaining"`      // 剩余地址（只读，计算值）
	// Managed 标记该项是否由本工具管理（写入 UCI）。导入的存量项为 false（仅显示），
	// 用户创建/编辑后置 true。uci 后端只把 Managed=true 的项投射到 /etc/config。
	Managed bool `json:"managed,omitempty"`
}

// StaticLease is a DHCP reservation binding a MAC to a fixed IP (iKuai DHCP静态分配).
// Maps to an OpenWrt `config host` section.
type StaticLease struct {
	ID           string `json:"id"`
	Hostname     string `json:"hostname"`  // 主机名称（可空）
	IP           string `json:"ip"`        // 绑定 IP
	MAC          string `json:"mac"`       // 绑定 MAC
	Gateway      string `json:"gateway"`   // 网关（每条可不同，dnsmasq 经 tag 下发）
	Interface    string `json:"interface"` // 绑定接口
	DNSPrimary   string `json:"dns_primary"`
	DNSSecondary string `json:"dns_secondary"`
	Remark       string `json:"remark"`  // 备注
	Enabled      bool   `json:"enabled"` // 状态
	// RoutePush：在 RoutePushMode=="tagged"（仅指定设备）模式下，是否把已标记推送的静态
	// 路由经 tag 下发给「这台」设备。RoutePushMode=="all" 时该字段无意义（全员下发）。
	RoutePush bool `json:"route_push"`
	Managed   bool `json:"managed,omitempty"`
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
	Vendor           string `json:"vendor,omitempty"`  // OUI 厂商识别（只读，按 MAC 前缀查）
}

// ACLEntry is one MAC entry in the DHCP black/white list.
type ACLEntry struct {
	ID      string `json:"id"`
	MAC     string `json:"mac"`
	Remark  string `json:"remark"`
	Enabled bool   `json:"enabled"`
	Managed bool   `json:"managed,omitempty"`
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
	// Type：路由类型。""/"unicast"=正常单播路由；"blackhole"=黑洞(静默丢包)；
	// "unreachable"=不可达(回 ICMP)；"prohibit"=禁止(回 ICMP)。后三者无下一跳，不需网关。
	Type    string `json:"type"`
	MTU     int    `json:"mtu"`     // 路由 MTU（0=不设）
	Remark  string `json:"remark"`  // 备注
	Enabled bool   `json:"enabled"` // 状态
	// PushToClients：把本路由经 DHCP option 121/249 下发给客户端，让"网关指向主路由"的
	// 设备也能把该网段流量引到本旁路由（仅 IPv4 有效；受 State.RoutePushMode 总开关控制）。
	PushToClients bool `json:"push_to_clients"`
	Managed       bool `json:"managed,omitempty"`
}

// Route families.
const (
	FamilyIPv4 = "ipv4"
	FamilyIPv6 = "ipv6"
)

// RoutePushMode values — how PushToClients routes are delivered via DHCP.
const (
	RoutePushOff    = "off"    // 不下发
	RoutePushAll    = "all"    // 给池内全部客户端下发（pool 级 option 121/249）
	RoutePushTagged = "tagged" // 仅给 RoutePush 的静态分配设备下发（host tag 级）
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
	Backend        string `json:"backend"`         // "uci" | "store"
	DHCPOK         bool   `json:"dhcp_ok"`         // DHCP 基础设施健康：dnsmasq/odhcpd 存在且无 pending
	EnabledServers int    `json:"enabled_servers"` // 已启用的 DHCP 服务端池数量（0=虽服务在跑但无池下发地址）
	Pending        bool   `json:"pending"`         // 有已保存未生效（committed 未 reload）的变更
	Message        string `json:"message"`
}

// State is the full managed network configuration — the unit of export/import
// and the store backend's persisted document.
type State struct {
	DHCPServers []DHCPServer  `json:"dhcp_servers"`
	Statics     []StaticLease `json:"statics"`
	ARPBind     bool          `json:"arp_bind"`
	ACL         ACL           `json:"acl"`
	Routes      []Route       `json:"routes"`
	// RoutePushMode 控制把已标记 PushToClients 的静态路由经 DHCP 下发给客户端：
	// "off"（默认）不下发；"all" 给所在池的全部客户端下发；"tagged" 仅给静态分配中
	// RoutePush=true 的设备下发。空串视为 "off"。
	RoutePushMode string `json:"route_push_mode,omitempty"`
	// NetIfaces holds LAN/WAN configs for the store (dev) backend; the uci
	// backend reads/writes /etc/config/network directly and ignores this field.
	NetIfaces []NetIface `json:"net_ifaces,omitempty"`
	// IPv6 sidecar (authoritative on the uci backend too; runtime LeaseV6/
	// NeighborV6/LineV6 are NOT persisted — read live on each request).
	WANv6s          []WANv6          `json:"wan_v6,omitempty"`
	LANv6s          []LANv6          `json:"lan_v6,omitempty"`
	PrefixStaticsV6 []PrefixStaticV6 `json:"prefix_statics_v6,omitempty"`
	ACLv6           ACLv6            `json:"acl_v6,omitempty"`
	// DNS（爱快 DNS 设置 / 多线路DNS 的 OpenWrt 落地）。
	DNS             DNSSettings      `json:"dns,omitempty"`
	DNSDoH          DNSDoH           `json:"dns_doh,omitempty"`
	DNSRecords      []DNSRecord      `json:"dns_records,omitempty"`
	DNSDomainRoutes []DNSDomainRoute `json:"dns_domain_routes,omitempty"`
	// LeaseNotes 是动态租约的自定义备注（MAC→备注），纯旁车元数据，无 UCI 投射。
	// 静态分配的备注走 StaticLease.Remark，不在此处。
	LeaseNotes map[string]string `json:"lease_notes,omitempty"`
}

// CloneState returns a deep copy of s (slices are freshly allocated).
func CloneState(s State) State {
	out := State{ARPBind: s.ARPBind, RoutePushMode: s.RoutePushMode}
	out.DHCPServers = append([]DHCPServer(nil), s.DHCPServers...)
	for i := range out.DHCPServers {
		out.DHCPServers[i].Exclude = append([]string(nil), s.DHCPServers[i].Exclude...)
		out.DHCPServers[i].CustomOptions = append([]CustomOption(nil), s.DHCPServers[i].CustomOptions...)
	}
	out.Statics = append([]StaticLease(nil), s.Statics...)
	out.Routes = append([]Route(nil), s.Routes...)
	out.ACL = ACL{Mode: s.ACL.Mode, Entries: append([]ACLEntry(nil), s.ACL.Entries...)}
	out.NetIfaces = append([]NetIface(nil), s.NetIfaces...)
	for i := range out.NetIfaces {
		src := s.NetIfaces[i]
		out.NetIfaces[i].Ports = append([]string(nil), src.Ports...)
		out.NetIfaces[i].ExtraAddrs = append([]IfaceAddr(nil), src.ExtraAddrs...)
		out.NetIfaces[i].PeerDNS = cloneBoolPtr(src.PeerDNS)
		out.NetIfaces[i].ForceLink = cloneBoolPtr(src.ForceLink)
		out.NetIfaces[i].Auto = cloneBoolPtr(src.Auto)
		out.NetIfaces[i].PPPoEv6 = cloneBoolPtr(src.PPPoEv6)
	}
	out.WANv6s = append([]WANv6(nil), s.WANv6s...)
	out.LANv6s = append([]LANv6(nil), s.LANv6s...)
	for i := range out.LANv6s {
		out.LANv6s[i].DNSServers = append([]string(nil), s.LANv6s[i].DNSServers...)
	}
	out.PrefixStaticsV6 = append([]PrefixStaticV6(nil), s.PrefixStaticsV6...)
	out.ACLv6 = ACLv6{Mode: s.ACLv6.Mode, Entries: append([]ACLv6Entry(nil), s.ACLv6.Entries...)}
	out.DNS = s.DNS
	out.DNS.SavedStock = cloneStrMap(s.DNS.SavedStock)
	out.DNS.PrevServers = append([]string(nil), s.DNS.PrevServers...)
	out.DNS.PrevAddrs = append([]string(nil), s.DNS.PrevAddrs...)
	out.DNS.RebindDomains = append([]string(nil), s.DNS.RebindDomains...)
	out.DNS.PrevRebindDomains = append([]string(nil), s.DNS.PrevRebindDomains...)
	out.DNSDoH = s.DNSDoH
	out.DNSRecords = append([]DNSRecord(nil), s.DNSRecords...)
	out.DNSDomainRoutes = append([]DNSDomainRoute(nil), s.DNSDomainRoutes...)
	out.LeaseNotes = cloneStrMap(s.LeaseNotes)
	return out
}

func cloneStrMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// cloneBoolPtr 深拷贝一个 *bool（nil 仍 nil），避免快照间共享同一底层 bool。
func cloneBoolPtr(p *bool) *bool {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
