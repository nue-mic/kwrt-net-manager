package netcfg

import "strings"

// store-backend implementations of the NIC / LAN-WAN surface. Everything is
// simulated + persisted in the sidecar State so every page works on a dev box.

func cloneIface(x NetIface) NetIface {
	x.Ports = append([]string(nil), x.Ports...)
	x.ExtraAddrs = append([]IfaceAddr(nil), x.ExtraAddrs...)
	x.PeerDNS = cloneBoolPtr(x.PeerDNS)
	x.ForceLink = cloneBoolPtr(x.ForceLink)
	x.Auto = cloneBoolPtr(x.Auto)
	x.PPPoEv6 = cloneBoolPtr(x.PPPoEv6)
	return x
}

// NICs returns a plausible physical-NIC inventory for development.
func (b *storeBackend) NICs() ([]NIC, error) {
	return []NIC{
		{Name: "eth0", MAC: "7C:2B:E1:13:E4:59", Up: true, Running: true, SpeedMb: 1000, Duplex: "full", MTU: 1500, Kind: NICPhysical, Bound: "wan", Role: RoleWAN, RxBytes: 90123456, TxBytes: 12345678, IPAddrs: []string{"100.64.0.2/30"}},
		{Name: "eth1", MAC: "7C:2B:E1:13:E4:5A", Up: true, Running: true, SpeedMb: 1000, Duplex: "full", MTU: 1500, Kind: NICPhysical, Bound: "lan", Role: RoleLAN, RxBytes: 45678901, TxBytes: 78901234},
		{Name: "eth2", MAC: "7C:2B:E1:13:E4:5B", Up: false, Running: false, SpeedMb: 0, MTU: 1500, Kind: NICPhysical, Bound: "", Role: ""},
		{Name: "br-lan", MAC: "7C:2B:E1:13:E4:5A", Up: true, Running: true, SpeedMb: 0, MTU: 1500, Kind: NICBridge, Bound: "lan", Role: RoleLAN, RxBytes: 45678901, TxBytes: 78901234, IPAddrs: []string{"192.168.1.1/24", "fd00::1/64"}},
	}, nil
}

// NICDetail returns a plausible simulated detail for a NIC in the dev inventory,
// so the 网卡详情页 renders end-to-end on Windows/CI without real hardware.
func (b *storeBackend) NICDetail(name string) (NICDetail, error) {
	nics, _ := b.NICs()
	idx := 1
	for i, n := range nics {
		if n.Name != name {
			continue
		}
		d := NICDetail{
			NIC:             n,
			IfIndex:         idx + i, // 2,3,4…（lo 占 1）
			TxQueueLen:      1000,
			IfAlias:         "",
			BridgePorts:     []string{},
			Addrs:           []NICAddr{},
			Driver:          "virtio_net",
			DriverVersion:   "1.0.0",
			Firmware:        "",
			BusInfo:         "virtio0",
			Autoneg:         "on",
			Port:            "Twisted Pair",
			SupportedModes:  []string{"1000baseT/Full", "100baseT/Full", "10baseT/Full"},
			AdvertisedModes: []string{"1000baseT/Full"},
		}
		if n.Up {
			d.Operstate, d.Carrier, d.CarrierChanges = "up", true, 1
		} else {
			d.Operstate, d.Carrier, d.CarrierChanges = "down", false, 0
		}
		// bridge devices in the dev inventory list their members as "ports".
		if n.Kind == NICBridge {
			d.BridgePorts = []string{"eth1", "eth2"}
			d.PermMAC = n.MAC
		} else {
			d.PermMAC = n.MAC
		}
		d.Stats = NICStats{
			RxBytes: n.RxBytes, TxBytes: n.TxBytes,
			RxPackets: n.RxBytes / 1000, TxPackets: n.TxBytes / 1000,
		}
		// Synthesize structured addrs from the simulated IPAddrs (CIDR strings).
		for _, cidr := range n.IPAddrs {
			if a, ok := parseStoreCIDR(cidr); ok {
				d.Addrs = append(d.Addrs, a)
			}
		}
		return d, nil
	}
	return NICDetail{}, ErrNotFound
}

// parseStoreCIDR splits a simulated "addr/prefix" into a NICAddr (scope=global).
func parseStoreCIDR(cidr string) (NICAddr, bool) {
	if cidr == "" {
		return NICAddr{}, false
	}
	a := NICAddr{Address: cidr, Prefix: 32, Scope: "global", Family: FamilyIPv4}
	if i := strings.IndexByte(cidr, '/'); i >= 0 {
		a.Address = cidr[:i]
		a.Prefix = atoiSafe(cidr[i+1:])
	}
	if strings.IndexByte(a.Address, ':') >= 0 {
		a.Family = FamilyIPv6
		if a.Prefix == 32 {
			a.Prefix = 64
		}
	}
	return a, true
}

func (b *storeBackend) NetIfaces() ([]NetIface, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]NetIface, len(b.st.NetIfaces))
	for i, x := range b.st.NetIfaces {
		out[i] = cloneIface(x)
	}
	return out, nil
}

func (b *storeBackend) SaveNetIface(in NetIface) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.st.NetIfaces {
		if b.st.NetIfaces[i].ID == in.ID {
			b.st.NetIfaces[i] = cloneIface(in)
			return b.flushLocked()
		}
	}
	b.st.NetIfaces = append(b.st.NetIfaces, cloneIface(in))
	return b.flushLocked()
}

func (b *storeBackend) DeleteNetIface(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.st.NetIfaces[:0:0]
	for _, x := range b.st.NetIfaces {
		if x.ID != id {
			out = append(out, x)
		}
	}
	b.st.NetIfaces = out
	return b.flushLocked()
}

func (b *storeBackend) WANAction(id, action string) error {
	if b.log != nil {
		b.log.Info("netcfg(store): WANAction (no-op simulation)", "id", id, "action", action)
	}
	return nil
}

func (b *storeBackend) DHCPServiceInfo() (DHCPSvcInfo, error) {
	// Dev box: pretend dnsmasq is present so the install banner stays hidden.
	return DHCPSvcInfo{Daemon: "dnsmasq", DnsmasqInstalled: true, CanInstall: false}, nil
}

func (b *storeBackend) InstallDHCP() (string, error) {
	return "store 后端（开发/模拟）无需安装 dnsmasq", nil
}
