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
}

// NIC kinds.
const (
	NICPhysical = "physical"
	NICBridge   = "bridge"
	NICVLAN     = "vlan"
	NICWifi     = "wifi"
	NICVirtual  = "virtual"
)

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

	// Runtime (read-only): is the interface up and what address it actually got.
	Up        bool   `json:"up"`
	RuntimeIP string `json:"runtime_ip"`
}

// Net interface roles / protos.
const (
	RoleLAN = "lan"
	RoleWAN = "wan"

	ProtoStatic = "static"
	ProtoDHCP   = "dhcp"
	ProtoPPPoE  = "pppoe"
)

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
