package netcfg

// store-backend implementations of the NIC / LAN-WAN surface. Everything is
// simulated + persisted in the sidecar State so every page works on a dev box.

func cloneIface(x NetIface) NetIface {
	x.Ports = append([]string(nil), x.Ports...)
	x.ExtraAddrs = append([]IfaceAddr(nil), x.ExtraAddrs...)
	x.PeerDNS = cloneBoolPtr(x.PeerDNS)
	x.ForceLink = cloneBoolPtr(x.ForceLink)
	x.Auto = cloneBoolPtr(x.Auto)
	return x
}

// NICs returns a plausible physical-NIC inventory for development.
func (b *storeBackend) NICs() ([]NIC, error) {
	return []NIC{
		{Name: "eth0", MAC: "7C:2B:E1:13:E4:59", Up: true, Running: true, SpeedMb: 1000, Duplex: "full", MTU: 1500, Kind: NICPhysical, Bound: "wan", Role: RoleWAN, RxBytes: 90123456, TxBytes: 12345678},
		{Name: "eth1", MAC: "7C:2B:E1:13:E4:5A", Up: true, Running: true, SpeedMb: 1000, Duplex: "full", MTU: 1500, Kind: NICPhysical, Bound: "lan", Role: RoleLAN, RxBytes: 45678901, TxBytes: 78901234},
		{Name: "eth2", MAC: "7C:2B:E1:13:E4:5B", Up: false, Running: false, SpeedMb: 0, MTU: 1500, Kind: NICPhysical, Bound: "", Role: ""},
		{Name: "br-lan", MAC: "7C:2B:E1:13:E4:5A", Up: true, Running: true, SpeedMb: 0, MTU: 1500, Kind: NICBridge, Bound: "lan", Role: RoleLAN, RxBytes: 45678901, TxBytes: 78901234},
	}, nil
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
