package netcfg

// IPv6 领域类型（爱快 IPv6 菜单全套）。与 IPv4 的 NetIface 写路径解耦，自成一套
// ipv6_* 文件。所有 JSON 一律 snake_case，与项目既有约定一致；旁车 netcfg.json
// 持久 WANv6/LANv6/PrefixStaticV6/ACLv6（运行态 LeaseV6/NeighborV6/LineV6 不入旁车）。

// IPv6 接入方式（WANv6.Proto）。
const (
	ProtoDHCPv6  = "dhcpv6"  // DHCPv6 客户端（动态获取，odhcp6c）
	ProtoStatic6 = "static6" // 静态 IPv6
	Proto6in4    = "6in4"    // 6in4 隧道（依赖 6in4 包）
	Proto6to4    = "6to4"    // 6to4 隧道（依赖 6to4 包）
	Proto6rd     = "6rd"     // 6rd 隧道（依赖 6rd 包）
)

// LANv6 配置类型 / DHCPv6 模式。
const (
	ConfigTypeAuto   = "auto"   // 自动获取（从委派前缀分配）
	ConfigTypeStatic = "static" // 静态指定 IPv6

	DHCPv6Stateless    = "stateless"     // 无状态：ra_management='0'（仅 SLAAC + 可选 DNS）
	DHCPv6Stateful     = "stateful"      // 有状态混合：ra_management='1'（DHCPv6 分配 + SLAAC，最兼容，默认荐）
	DHCPv6StatefulOnly = "stateful_only" // 纯有状态：ra_management='2'
)

// DHCPv6 接入控制（黑白名单）底层方法 + 模式。
const (
	ACLv6MethodDUID = "duid"  // 按 DUID 拒发（odhcpd host hostid='ignore'，可靠原生）
	ACLv6MethodL2   = "l2mac" // 按 MAC L2 拦截（nftables/ebtables，实验，SLAAC 可绕过）
)

// WANv6 是一条 IPv6 外网线路（爱快「IPv6 外网配置」）。落到 OpenWrt 的
// `config interface`（odhcp6c 客户端侧 / 静态 / 隧道）。
type WANv6 struct {
	ID       string `json:"id"`        // UCI 接口名（wan6 / wan_6 ...）
	Name     string `json:"name"`      // 显示名（默认同 ID）
	WANIface string `json:"wan_iface"` // 绑定的 IPv4 外网接口（用于 device '@<wan>' 跟随）
	Device   string `json:"device"`    // 物理设备或 @<iface>
	Proto    string `json:"proto"`     // dhcpv6 | static6 | 6in4 | 6to4 | 6rd
	Enabled  bool   `json:"enabled"`

	// DHCPv6 客户端（proto=dhcpv6）
	ReqPrefix    string `json:"req_prefix"`    // 请求前缀长度："48".."64" / "auto" / "no"
	FixedPrefix  string `json:"fixed_prefix"`  // 尝试固定前缀（如 2001:db8::/60，写 reqprefix）
	ForcePrefix  bool   `json:"force_prefix"`  // 强行获取前缀（reqaddress/reqprefix force）
	ClientID     string `json:"client_id"`     // 客户端 DUID（hex），留空=OpenWrt 默认
	NoRelease    bool   `json:"no_release"`    // norelease（断开不释放）
	PeerDNS      bool   `json:"peer_dns"`      // 用对端下发 DNS（true）；false=手填 dns_*
	DNSPrimary   string `json:"dns_primary"`   // 手填首选 DNS（peer_dns=false 时生效）
	DNSSecondary string `json:"dns_secondary"` // 手填备选 DNS

	// 静态 IPv6（proto=static6）
	StaticIP6 string `json:"static_ip6"`     // ip6addr，如 2001:db8::1/64
	StaticGW  string `json:"static_gateway"` // ip6gw

	// 隧道（proto=6in4/6to4/6rd）通用
	PeerAddr  string `json:"peer_addr"`  // 隧道对端 IPv4
	TunPrefix string `json:"tun_prefix"` // 隧道 ip6prefix（6in4/6rd）

	MTU    int    `json:"mtu"`
	Remark string `json:"remark"`

	// 只读运行态（来自 ubus network.interface.<x> status）
	IP6Address string `json:"ip6_address"` // 全局 IPv6 地址
	IP6Gateway string `json:"ip6_gateway"` // 默认网关（route nexthop 推导）
	IP6Prefix  string `json:"ip6_prefix"`  // 委派前缀
	LocalLink  string `json:"local_link"`  // 本地链接 fe80::
	Up         bool   `json:"up"`

	Managed bool `json:"managed,omitempty"`
}

// LANv6 是一条 IPv6 内网（爱快「IPv6 内网配置」）。落到 `config interface` 的
// ip6assign/ip6class + `config dhcp` 的 odhcpd RA/DHCPv6 服务端。
type LANv6 struct {
	ID              string   `json:"id"`                // = 内网接口名（lan ...）
	Interface       string   `json:"interface"`         // 内网接口
	ConfigType      string   `json:"config_type"`       // auto | static
	BindWAN         string   `json:"bind_wan"`          // 绑定外网线路（WANv6 id），空=自动
	PrefixAssignLen int      `json:"prefix_assign_len"` // ip6assign（60/64）
	PrefixHint      string   `json:"prefix_hint"`       // ip6hint
	StaticIP6       string   `json:"static_ip6"`        // config_type=static 时的 ip6addr
	DHCPv6Enabled   bool     `json:"dhcpv6_enabled"`    // 开启 DHCPv6 服务端（dhcpv6=server + ra=server）
	DHCPv6Mode      string   `json:"dhcpv6_mode"`       // stateless | stateful | stateful_only
	IPv6DNSEnabled  bool     `json:"ipv6_dns_enabled"`  // 下发 IPv6 DNS
	DNSServers      []string `json:"dns_servers"`       // 自定义 IPv6 DNS 列表
	LeaseMinutes    int      `json:"lease_minutes"`     // 租期（分钟）
	RAMTUEnabled    bool     `json:"ra_mtu_enabled"`    // 下发 RA MTU
	RAMTU           int      `json:"ra_mtu"`            // RA MTU 值
	Enabled         bool     `json:"enabled"`
	Remark          string   `json:"remark"`

	// 只读运行态
	IP6Address string `json:"ip6_address"`
	LocalLink  string `json:"local_link"`

	Managed bool `json:"managed,omitempty"`
}

// LeaseV6 是一条 DHCPv6 租约（爱快「DHCPv6 终端」）。只读，不入旁车。
type LeaseV6 struct {
	Hostname     string `json:"hostname"`
	MAC          string `json:"mac"`           // 来自邻居表 lladdr / EUI-64 反推（标注）
	LocalLink    string `json:"local_link"`    // 本地链接 IPv6
	IPv6Addr     string `json:"ipv6_addr"`     // 终端 IPv6 地址
	DUID         string `json:"duid"`          // DHCP 唯一标识
	IAID         string `json:"iaid"`          //
	Interface    string `json:"interface"`     //
	ValidSeconds int64  `json:"valid_seconds"` // 有效时间（秒）
	Static       bool   `json:"static"`        // 命中前缀静态分配
	Remark       string `json:"remark"`
}

// PrefixStaticV6 是一条前缀静态分配（爱快「前缀静态分配」）。落到 odhcpd
// `config host` 的 duid + hostid（只固定 IID，不能固定整段 PD）。
type PrefixStaticV6 struct {
	ID           string `json:"id"`
	LocalLink    string `json:"local_link"`    // 终端本地链接 IPv6 地址（展示标识）
	LANInterface string `json:"lan_interface"` // 内网接口
	WANLine      string `json:"wan_line"`      // 外网线路
	DUID         string `json:"duid"`          // 实际匹配键
	HostID       string `json:"host_id"`       // 固定接口 ID（hex，如 ::1234）
	MAC          string `json:"mac"`           // fallback 匹配键
	Remark       string `json:"remark"`
	Enabled      bool   `json:"enabled"`
	Managed      bool   `json:"managed,omitempty"`
}

// ACLv6Entry 是 DHCPv6 接入控制名单中的一条。
type ACLv6Entry struct {
	ID      string `json:"id"`
	MAC     string `json:"mac"`    // L2 方法用
	DUID    string `json:"duid"`   // DUID 方法用（可靠）
	Method  string `json:"method"` // duid | l2mac
	Remark  string `json:"remark"`
	Enabled bool   `json:"enabled"`
	Managed bool   `json:"managed,omitempty"`
}

// ACLv6 是 DHCPv6 接入控制（爱快「DHCPv6 黑白名单」，OpenWrt 原生受限，详见设计文档）。
type ACLv6 struct {
	Mode    string       `json:"mode"` // blacklist | whitelist
	Entries []ACLv6Entry `json:"entries"`
}

// NeighborV6 是一条 NDP 邻居（爱快「邻居列表」）。只读。
type NeighborV6 struct {
	MAC       string `json:"mac"`
	IPv6      string `json:"ipv6"`
	Interface string `json:"interface"`
	State     string `json:"state"`  // REACHABLE | STALE | DELAY | PROBE | FAILED | PERMANENT | NOARP
	Router    bool   `json:"router"` // 该邻居是路由器
	Remark    string `json:"remark"`
}

// LineV6 是一条 IPv6 线路的实时统计（爱快「IPv6 线路详情」）。只读。
type LineV6 struct {
	Line        string `json:"line"`
	Connections int    `json:"connections"`
	UpBps       int64  `json:"up_bps"`
	DownBps     int64  `json:"down_bps"`
	TotalUp     int64  `json:"total_up"`
	TotalDown   int64  `json:"total_down"`
}

// DHCPv6SvcInfo 报告 odhcpd / ip-full 等 IPv6 相关组件的安装状态（一键安装入口）。
type DHCPv6SvcInfo struct {
	OdhcpdInstalled bool   `json:"odhcpd_installed"` // /etc/init.d/odhcpd 存在
	OdhcpdRunning   bool   `json:"odhcpd_running"`
	IPFull          bool   `json:"ip_full"`       // ip 支持 neigh 子命令
	PkgManager      string `json:"pkg_manager"`   // opkg | apk | ""
	LanServerOn     bool   `json:"lan_server_on"` // 是否已有 LAN 开启 dhcpv6=server
}
