package netcfg

// NIC is one physical (or virtual) network device on the box — the 网卡列表.
// Read from /sys/class/net so it works on any Linux/OpenWrt without iproute2 -j.
type NIC struct {
	Name    string `json:"name"`     // eth0 / br-lan / eth0.10
	MAC     string `json:"mac"`      // upper-case colon form
	Up      bool   `json:"up"`       // carrier (link) present
	Running bool   `json:"running"`  // operstate == up
	SpeedMb int    `json:"speed_mb"` // link speed in Mb/s, 0 if unknown/down
	Duplex  string `json:"duplex"`   // full | half | ""
	MTU     int    `json:"mtu"`
	Kind    string `json:"kind"`  // physical | bridge | vlan | wifi | virtual
	Bound   string `json:"bound"` // interface (lan/wan/...) using this NIC, or ""
	Role    string `json:"role"`  // "lan" | "wan" | "" (from the bound interface)
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
	// IPAddrs 是该网卡上的全部地址（IPv4+IPv6，CIDR 形式），来自 `ip -o addr show`。
	IPAddrs []string `json:"ip_addrs"`
}

// NIC kinds.
const (
	NICPhysical = "physical"
	NICBridge   = "bridge"
	NICVLAN     = "vlan"
	NICWifi     = "wifi"
	NICVirtual  = "virtual"
)

// NICAddr 是网卡上的一个地址（比 NIC.IPAddrs 的纯 CIDR 字符串更结构化），
// 来自 `ip -o addr show dev <name>`，供详情页逐条展示。
type NICAddr struct {
	Family  string `json:"family"`  // ipv4 | ipv6
	Address string `json:"address"` // 不含前缀，如 192.168.1.1
	Prefix  int    `json:"prefix"`  // CIDR 位数
	Scope   string `json:"scope"`   // global | link | host
}

// NICStats 是 /sys/class/net/<name>/statistics 的收发计数明细。
type NICStats struct {
	RxBytes    uint64 `json:"rx_bytes"`
	TxBytes    uint64 `json:"tx_bytes"`
	RxPackets  uint64 `json:"rx_packets"`
	TxPackets  uint64 `json:"tx_packets"`
	RxErrors   uint64 `json:"rx_errors"`
	TxErrors   uint64 `json:"tx_errors"`
	RxDropped  uint64 `json:"rx_dropped"`
	TxDropped  uint64 `json:"tx_dropped"`
	Multicast  uint64 `json:"multicast"`
	Collisions uint64 `json:"collisions"`
}

// NICDetail 是单块网卡的综合详情（网卡详情页）。内嵌 NIC（提升 name/mac/up/…/ip_addrs），
// 再叠加 sysfs 链路/统计、网桥从属关系、VLAN、以及尽力而为的 ethtool 驱动/链路能力信息。
type NICDetail struct {
	NIC                     // 内嵌：name/mac/up/running/speed_mb/duplex/mtu/kind/bound/role/rx_bytes/tx_bytes/ip_addrs
	IfIndex        int      `json:"ifindex"`
	Operstate      string   `json:"operstate"`       // /sys .../operstate（up/down/lowerlayerdown/…）
	Carrier        bool     `json:"carrier"`         // /sys .../carrier == "1"
	CarrierChanges int      `json:"carrier_changes"` // /sys .../carrier_changes
	TxQueueLen     int      `json:"tx_queue_len"`
	IfAlias        string   `json:"ifalias"`
	Master         string   `json:"master"`       // 所属网桥（被 enslave 时），读 /sys .../master 符号链接 basename
	BridgePorts    []string `json:"bridge_ports"` // 若自身是网桥：/sys .../brif/ 列表
	VlanID         int      `json:"vlan_id,omitempty"`
	VlanProto      string   `json:"vlan_proto,omitempty"`

	// 以下 ethtool 尽力而为（未安装或解析失败留空，绝不报错）。
	Driver          string   `json:"driver"`
	DriverVersion   string   `json:"driver_version"`
	Firmware        string   `json:"firmware"`
	BusInfo         string   `json:"bus_info"`
	PermMAC         string   `json:"perm_mac"`
	Autoneg         string   `json:"autoneg"` // on | off | ""
	Port            string   `json:"port"`    // Twisted Pair | Fibre | …
	SupportedModes  []string `json:"supported_modes"`
	AdvertisedModes []string `json:"advertised_modes"`

	Stats NICStats  `json:"stats"`
	Addrs []NICAddr `json:"addrs"` // 比 ip_addrs 更详细（family/prefix/scope）
}

// NetIface is a configured L3 network — a LAN or WAN (内网/外网) entry. Maps to
// a UCI `config interface` (+ its `config device` bridge for LAN). snake_case.
type NetIface struct {
	ID    string `json:"id"`    // uci section name: lan / wan / wan1 / lan2
	Name  string `json:"name"`  // display: lan1 / wan1
	Role  string `json:"role"`  // "lan" | "wan"
	Proto string `json:"proto"` // static | dhcp | pppoe (wan); static (lan)

	// Device / port binding. Device is the primary device (eth0 or a bridge
	// br-lan). Ports are the physical members of a LAN bridge (绑定网卡 + 扩展网卡).
	Device string   `json:"device"`
	Ports  []string `json:"ports"`

	// Static addressing (LAN always; WAN when proto=static).
	IPAddr       string `json:"ipaddr"`
	Netmask      string `json:"netmask"`
	Gateway      string `json:"gateway"`
	DNSPrimary   string `json:"dns_primary"`
	DNSSecondary string `json:"dns_secondary"`

	// PPPoE (WAN proto=pppoe).
	Username string `json:"username"`
	Password string `json:"password"`
	Service  string `json:"service"` // PPPoE service-name (服务器名称)
	AC       string `json:"ac"`      // PPPoE ac-name (AC名称)

	MTU       int    `json:"mtu"`
	DefaultGW bool   `json:"default_gw"` // WAN: provide the default route (默认网关)
	CloneMAC  string `json:"clone_mac"`  // 克隆MAC
	Remark    string `json:"remark"`     // 备注

	// ExtraAddrs 是附加 IP（次地址/管理地址，可同/异子网）。投射为 list ipaddr。
	// per-IP 的 remark 旁车权威（uci 无此字段）。不发 DHCP（见设计 §4.4）。
	ExtraAddrs []IfaceAddr `json:"extra_addrs"`

	// OpenWrt 接口全量对齐（默认折叠在前端「高级」）。
	Metric    int    `json:"metric,omitempty"`     // option metric，多 WAN 优先级（越小越优先）
	PeerDNS   *bool  `json:"peerdns,omitempty"`    // option peerdns（nil=默认）
	Broadcast string `json:"broadcast,omitempty"`  // option broadcast（static 广播地址）
	ForceLink *bool  `json:"force_link,omitempty"` // option force_link（无链路也配地址）
	Auto      *bool  `json:"auto,omitempty"`       // option auto（开机自启，nil/true=默认）
	IP6Assign int    `json:"ip6assign,omitempty"`  // option ip6assign（委派前缀长度，0=不设）
	IP6Hint   string `json:"ip6hint,omitempty"`    // option ip6hint（hex 子前缀 ID）
	IP6Gw     string `json:"ip6gw,omitempty"`      // option ip6gw（IPv6 默认网关）
	// 注：静态 IPv6 统一走 ExtraAddrs(family=ipv6)，投射为 list ip6addr（纯列表无主次）。

	IP6Prefix  string `json:"ip6prefix,omitempty"`  // option ip6prefix（向下游分发的前缀 CIDR）
	IP6IfaceID string `json:"ip6ifaceid,omitempty"` // option ip6ifaceid（接口 ID 后缀）
	Keepalive  string `json:"keepalive,omitempty"`  // PPPoE option keepalive（如 "5 25"）
	PPPoEv6    *bool  `json:"pppoe_ipv6,omitempty"` // PPPoE 上启用 IPv6（option ipv6 '1'）

	// Runtime (read-only): is the interface up and what address it actually got.
	// Status 比 Up 布尔更细：PPPoE 拨号 / DHCP 获取地址过程中 up 仍为 false 但 ubus 的
	// pending 为 true，单凭 up 无法区分「拨号中」与「未连接」，故额外暴露 Status，
	// 让前端能显示「拨号中…/获取地址中…」中间态。
	Up        bool   `json:"up"`
	RuntimeIP string `json:"runtime_ip"`
	Status    string `json:"status"` // connected | connecting | disconnected
}

// IfaceAddr 是接口上的一个附加 IP。落地 OpenWrt `list ipaddr '<address>/<prefix>'`。
type IfaceAddr struct {
	Address string `json:"address"` // 点分 IPv4，如 10.0.0.1
	Prefix  int    `json:"prefix"`  // CIDR 位数，如 24
	Family  string `json:"family"`  // "ipv4" | "ipv6"
	Remark  string `json:"remark"`  // 备注（仅旁车）
	Enabled bool   `json:"enabled"` // 关闭=不投射（本期 UI 不暴露禁用，默认 true）
}

// Net interface roles / protos.
const (
	RoleLAN = "lan"
	RoleWAN = "wan"

	ProtoStatic = "static"
	ProtoDHCP   = "dhcp"
	ProtoPPPoE  = "pppoe"
)

// NetIface.Status 运行态取值（比 Up 布尔更细，能表达「拨号中」）。
const (
	IfStatusConnected    = "connected"    // 已连接（ubus up=true）
	IfStatusConnecting   = "connecting"   // 拨号中/获取地址中（up=false 但 pending=true）
	IfStatusDisconnected = "disconnected" // 未连接
)

// runtimeStatus 把 ubus 的 up/pending 标志映射成粗粒度连接态。
func runtimeStatus(up, pending bool) string {
	switch {
	case up:
		return IfStatusConnected
	case pending:
		return IfStatusConnecting
	default:
		return IfStatusDisconnected
	}
}

// DHCPSvcInfo describes which DHCP daemon is installed/running, powering the
// 一键安装 dnsmasq flow when a box ships without dnsmasq (the preferred backend).
type DHCPSvcInfo struct {
	Daemon           string `json:"daemon"`            // dnsmasq | odhcpd | "" (none)
	DnsmasqInstalled bool   `json:"dnsmasq_installed"` // /etc/init.d/dnsmasq present
	OdhcpdInstalled  bool   `json:"odhcpd_installed"`
	CanInstall       bool   `json:"can_install"` // a package manager is available
	PkgManager       string `json:"pkg_manager"` // opkg | apk | ""
}

// NetOverview is the 内外网设置 dashboard summary.
type NetOverview struct {
	WANCount    int        `json:"wan_count"`
	WANUp       int        `json:"wan_up"`
	Connections int        `json:"connections"` // active conntrack entries
	LANCount    int        `json:"lan_count"`
	LANUp       int        `json:"lan_up"`
	DHCPOn      int        `json:"dhcp_on"`   // enabled DHCP servers
	Terminals   int        `json:"terminals"` // active leases
	FreePorts   int        `json:"free_ports"`
	WANs        []NetIface `json:"wans"`
	LANs        []NetIface `json:"lans"`
}
