package netcfg

// store 后端的 IPv6 实现：旁车 CRUD + 确定性模拟运行态，使全部 IPv6 页面在
// 非 OpenWrt 主机（开发/CI/Windows）端到端可跑。uci 后端通过嵌入 *storeBackend
// 复用这里的旁车 CRUD，并 override 运行态读为真实 ubus/ip 调用。

// ---- 旁车 CRUD ----

func (b *storeBackend) WANv6s() ([]WANv6, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]WANv6(nil), b.st.WANv6s...), nil
}

func (b *storeBackend) SaveWANv6s(list []WANv6) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.WANv6s = append([]WANv6(nil), list...)
	return b.flushLocked()
}

func (b *storeBackend) LANv6s() ([]LANv6, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]LANv6(nil), b.st.LANv6s...), nil
}

func (b *storeBackend) SaveLANv6s(list []LANv6) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.LANv6s = append([]LANv6(nil), list...)
	return b.flushLocked()
}

func (b *storeBackend) PrefixStaticsV6() ([]PrefixStaticV6, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]PrefixStaticV6(nil), b.st.PrefixStaticsV6...), nil
}

func (b *storeBackend) SavePrefixStaticsV6(list []PrefixStaticV6) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.PrefixStaticsV6 = append([]PrefixStaticV6(nil), list...)
	return b.flushLocked()
}

func (b *storeBackend) ACLv6() (ACLv6, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	mode := b.st.ACLv6.Mode
	if mode == "" {
		mode = ACLBlacklist
	}
	return ACLv6{Mode: mode, Entries: append([]ACLv6Entry(nil), b.st.ACLv6.Entries...)}, nil
}

func (b *storeBackend) SaveACLv6(acl ACLv6) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.ACLv6 = ACLv6{Mode: acl.Mode, Entries: append([]ACLv6Entry(nil), acl.Entries...)}
	return b.flushLocked()
}

// ---- 模拟运行态（store 后端） ----

// LeasesV6 合成一张可信的 DHCPv6 终端表（仅当存在已开 DHCPv6 服务端的 LANv6 时），
// 否则返回空（对齐真机「LAN 未开 v6 server 则无租约」的行为）。
func (b *storeBackend) LeasesV6() ([]LeaseV6, error) {
	b.mu.Lock()
	lans := append([]LANv6(nil), b.st.LANv6s...)
	b.mu.Unlock()
	on := false
	iface := "lan"
	for _, l := range lans {
		if l.DHCPv6Enabled && l.Enabled {
			on = true
			iface = l.Interface
			break
		}
	}
	if !on {
		return []LeaseV6{}, nil
	}
	return []LeaseV6{
		{Hostname: "Z590-A-Computer", MAC: "04:42:1a:2b:66:57", LocalLink: "fe80::1fc:212e:a640:15b9", IPv6Addr: "2408:822f:22a2:a3f1::c53", DUID: "0001000128fc56f1086ac5230d7f", IAID: "a8b1c2d3", Interface: iface, ValidSeconds: 6824},
		{Hostname: "rthink-server", MAC: "00:0c:29:67:4e:e8", LocalLink: "fe80::49f1:3b3b:17e:36b9", IPv6Addr: "2408:822f:22a2:a3f1::73e", DUID: "0004c72da9c855a781fca1022afcfbab", IAID: "11223344", Interface: iface, ValidSeconds: 6201},
		{Hostname: "iPad-Air13", MAC: "32:17:b3:ed:f9:5b", LocalLink: "fe80::cec:1aaa:8a36:d6f6", IPv6Addr: "2408:822f:22a2:a3f1::cc6", DUID: "000300013217b3edf95b", IAID: "55667788", Interface: iface, ValidSeconds: 7044},
	}, nil
}

// NeighborsV6 合成一张 NDP 邻居表（开发演示用）。
func (b *storeBackend) NeighborsV6() ([]NeighborV6, error) {
	return []NeighborV6{
		{MAC: "f4:2d:06:4f:e3:fb", IPv6: "fe80::5", Interface: "lan", State: "STALE", Router: true},
		{MAC: "00:0c:29:9c:4f:49", IPv6: "fe80::20c:29ff:fe9c:4f49", Interface: "lan", State: "REACHABLE"},
		{MAC: "00:0c:29:06:da:3a", IPv6: "2408:822f:22a2:a3f1::d0a", Interface: "lan", State: "REACHABLE"},
		{MAC: "e8:61:1f:11:2e:80", IPv6: "fe80::ea61:1fff:fe11:2e80", Interface: "lan", State: "REACHABLE", Remark: "【ESXI0】私有云管理口"},
		{MAC: "16:cd:03:ca:2b:30", IPv6: "fe80::1c34:6cba:4fa1:7d99", Interface: "lan", State: "STALE", Remark: "IPM14-慕容-5G"},
		{MAC: "ec:41:18:6c:d9:42", IPv6: "fe80::ee41:18ff:fe6c:d942", Interface: "lan", State: "REACHABLE", Remark: "小爱音箱2"},
	}, nil
}

func (b *storeBackend) DeleteNeighborV6(addr, dev string) error {
	if b.log != nil {
		b.log.Info("netcfg(store): DeleteNeighborV6 (no-op)", "addr", addr, "dev", dev)
	}
	return nil
}

func (b *storeBackend) FlushNeighborsV6(dev string) error {
	if b.log != nil {
		b.log.Info("netcfg(store): FlushNeighborsV6 (no-op)", "dev", dev)
	}
	return nil
}

// LinesV6 合成每条 IPv6 外网线路的统计。
func (b *storeBackend) LinesV6() ([]LineV6, error) {
	b.mu.Lock()
	wans := append([]WANv6(nil), b.st.WANv6s...)
	b.mu.Unlock()
	if len(wans) == 0 {
		return []LineV6{{Line: "wan6", Connections: 1221, UpBps: 5500, DownBps: 1800, TotalUp: 2_600_000_000, TotalDown: 8_400_000_000}}, nil
	}
	out := make([]LineV6, 0, len(wans))
	for i, w := range wans {
		// 显式 int64 运算：字面量在 32 位架构(386/armv7/mips)的 int 上下文会溢出。
		out = append(out, LineV6{
			Line: w.Name, Connections: 1221 - i*37,
			UpBps: int64(5500 - i*900), DownBps: int64(1800 + i*400),
			TotalUp: int64(2_600_000_000) - int64(i)*100_000_000, TotalDown: int64(8_400_000_000) - int64(i)*200_000_000,
		})
	}
	return out, nil
}

func (b *storeBackend) DHCPv6ServiceInfo() (DHCPv6SvcInfo, error) {
	b.mu.Lock()
	lans := append([]LANv6(nil), b.st.LANv6s...)
	b.mu.Unlock()
	on := false
	for _, l := range lans {
		if l.DHCPv6Enabled && l.Enabled {
			on = true
			break
		}
	}
	return DHCPv6SvcInfo{OdhcpdInstalled: true, OdhcpdRunning: true, IPFull: true, PkgManager: "opkg", LanServerOn: on}, nil
}

func (b *storeBackend) TransitionPkg(proto string) (bool, string, error) {
	// 开发后端假定隧道包已装。
	return true, transitionPkgName(proto), nil
}

// transitionPkgName 返回某接入方式所需的 OpenWrt 协议包名。
func transitionPkgName(proto string) string {
	switch proto {
	case Proto6in4:
		return "6in4"
	case Proto6to4:
		return "6to4"
	case Proto6rd:
		return "6rd"
	default:
		return ""
	}
}

// ---- IPv6 seed（开发演示数据） ----

func seedIPv6() ([]WANv6, []LANv6, []PrefixStaticV6, ACLv6) {
	wan := []WANv6{{
		ID: "wan6", Name: "wan6", WANIface: "wan", Device: "eth0", Proto: ProtoDHCPv6, Enabled: true,
		ReqPrefix: "60", ForcePrefix: true, PeerDNS: true, NoRelease: true, MTU: 1492,
		IP6Address: "2408:822e:227a:61d9:a04f:be:786b:6f91/64", IP6Gateway: "fe80::ce1a:faff:feec:fca0",
		IP6Prefix: "2408:822f:22a2:a3f0::/60", LocalLink: "fe80::8439:bddd:66a6:be3", Up: true,
		Remark: "默认外网 IPv6（DHCPv6 客户端）", Managed: true,
	}}
	lan := []LANv6{{
		ID: "lan", Interface: "lan", ConfigType: ConfigTypeAuto, BindWAN: "wan6", PrefixAssignLen: 64,
		DHCPv6Enabled: true, DHCPv6Mode: DHCPv6Stateful, IPv6DNSEnabled: false, DNSServers: []string{},
		LeaseMinutes: 120, Enabled: true, IP6Address: "2408:822f:22a2:a3f1::1001/64",
		LocalLink: "fe80::7e2b:e1ff:fe13:e45a", Remark: "默认内网 IPv6", Managed: true,
	}}
	return wan, lan, []PrefixStaticV6{}, ACLv6{Mode: ACLBlacklist, Entries: []ACLv6Entry{}}
}
