package netcfg

// DNS 领域类型（爱快「网络设置 > DNS 设置 / 多线路DNS」的 OpenWrt 落地）。
// 设计见 docs/superpowers/specs/2026-06-16-dns-full-design.md。
// 独立托管标记，与 IPv4(managedMarker) / IPv6(managedMarkerV6) 互不删除。
const managedMarkerDNS = "kwrt-net-manager-dns"

// DNSSettings 是全局 DNS 配置（单例，对应爱快 DNS 设置页上半部）。Enabled=false 时
// applyDNS 不碰任何 stock 配置（部署默认态 = 零行为改动）。
type DNSSettings struct {
	Enabled       bool     `json:"enabled"`           // 是否由本工具托管 DNS 设置
	DNSPrimary    string   `json:"dns_primary"`       // 首选上游 DNS
	DNSSecondary  string   `json:"dns_secondary"`     // 备选上游 DNS
	NoResolv      bool     `json:"no_resolv"`         // 仅用上面的上游，不读运营商下发（开前必须有可达上游）
	FilterAAAA    bool     `json:"filter_aaaa"`       // 禁止 AAAA(IPv6) 解析
	CacheSize     int      `json:"cache_size"`        // DNS 缓存大小（条；0=关闭缓存；<0 视为不设）
	LocalTTL      int      `json:"local_ttl"`         // 本地记录 TTL（秒）
	MinCacheTTL   int      `json:"min_cache_ttl"`     // 最小缓存 TTL（秒，dnsmasq 默认上限 3600）
	MaxCacheTTL   int      `json:"max_cache_ttl"`     // 最大缓存 TTL（秒，dnsmasq 默认上限 3600）
	ForceProxy    bool     `json:"force_proxy"`       // 强制客户端 DNS 代理（firewall 劫持 53 到本机）
	DNSSEC        bool     `json:"dnssec"`            // 启用 DNSSEC 校验（防 DNS 投毒；dnsmasq 须编译支持）
	RebindProtect bool     `json:"rebind_protection"` // 防 DNS 重绑定攻击（默认 stock 开）
	AllServers    bool     `json:"all_servers"`       // 并发查询所有上游（最快者优先；关=按序）
	RebindDomains []string `json:"rebind_domains"`    // 防重绑定白名单域名（这些域允许解析到内网 IP）

	// ---- 旁车内部簿记（不在 openapi/前端暴露，仅用于安全回滚 stock 段）----
	SavedStock        map[string]string `json:"saved_stock,omitempty"`         // 改 @dnsmasq[0] 标量前的旧值快照
	PrevServers       []string          `json:"prev_servers,omitempty"`        // 上次写入 @dnsmasq[0].server 的精确值
	PrevAddrs         []string          `json:"prev_addrs,omitempty"`          // 上次写入 @dnsmasq[0].address 的精确值
	PrevRebindDomains []string          `json:"prev_rebind_domains,omitempty"` // 上次写入 @dnsmasq[0].rebind_domain 的精确值
}

// DNSDoH 是 DNS over HTTPS 加速配置（单例）。经 https-dns-proxy 包实现，默认关闭。
type DNSDoH struct {
	Enabled      bool   `json:"enabled"`       // 是否启用 DoH（需先安装 https-dns-proxy）
	ResolverURL  string `json:"resolver_url"`  // DoH 请求地址，如 https://dns.alidns.com/dns-query
	ListenPort   int    `json:"listen_port"`   // 本机代理监听端口（默认 5053，必须 ≠ 53）
	BootstrapDNS string `json:"bootstrap_dns"` // 引导 DNS（解析 DoH 域名用），如 223.5.5.5
}

// DNS 自定义解析（反向代理）记录类型。
const (
	DNSRecordA    = "A"    // 精确域 → IPv4（host-record）
	DNSRecordAAAA = "AAAA" // 精确域 → IPv6（host-record）
)

// DNSRecord 是一条自定义解析记录（爱快 DNS 反向代理 / 自定义解析）。
// 精确域用 config hostrecord 具名节（自动 PTR，隔离安全）；通配域(*.x) 用
// @dnsmasq[0] 的 list address（精确值可回滚）。
type DNSRecord struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`       // 域名；以 *. 开头表示通配（含所有子域）
	RecordType string `json:"record_type"`  // A | AAAA（由解析地址族决定）
	Address    string `json:"address"`      // 解析地址（IPv4/IPv6）
	Wildcard   bool   `json:"wildcard"`     // 是否通配（*.domain）
	SrcIPScope string `json:"src_ip_scope"` // 作用IP段（展示用；单 dnsmasq 不支持按源分应答→全局生效）
	Remark     string `json:"remark"`
	Enabled    bool   `json:"enabled"`
	Managed    bool   `json:"managed,omitempty"`
}

// DNSDomainRoute 是一条「域名分流 DNS」（多线路DNS 的 OpenWrt 可行降级）：
// 指定域名走指定上游 DNS（可选绑定出接口）。投射为 @dnsmasq[0] 的
// list server '/域名/上游[@iface]'。
type DNSDomainRoute struct {
	ID       string `json:"id"`
	Domain   string `json:"domain"`    // 域名（该域走下面的上游）
	Server   string `json:"server"`    // 上游 DNS（ip 或 ip#port）
	OutIface string `json:"out_iface"` // 可选：强制出接口（@iface）
	Remark   string `json:"remark"`
	Enabled  bool   `json:"enabled"`
	Managed  bool   `json:"managed,omitempty"`
}

// DNSCacheStats 是只读的 dnsmasq 缓存运行态（自 dnsmasq 启动以来的累计值）。
type DNSCacheStats struct {
	Supported  bool    `json:"supported"`  // 是否成功读到（CHAOS 查询可用）
	CacheSize  int64   `json:"cache_size"` // 缓存容量
	Insertions int64   `json:"insertions"` // 插入数
	Evictions  int64   `json:"evictions"`  // 淘汰数
	Hits       int64   `json:"hits"`       // 命中数
	Misses     int64   `json:"misses"`     // 未命中数
	HitRatio   float64 `json:"hit_ratio"`  // 命中率 = hits/(hits+misses)
}

// DNSSvcInfo 报告 DNS 相关能力探测结果（驱动前端置灰/一键安装）。
type DNSSvcInfo struct {
	Backend             string `json:"backend"`               // uci | store
	FilterAAAASupported bool   `json:"filter_aaaa_supported"` // 本机 dnsmasq 是否支持 --filter-AAAA
	DoHInstalled        bool   `json:"doh_installed"`         // 是否已装 https-dns-proxy
	PkgManager          string `json:"pkg_manager"`           // opkg | apk | ""
	CanInstall          bool   `json:"can_install"`           // 是否可一键安装 DoH
}
