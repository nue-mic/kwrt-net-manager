package netcfg

// DNS 领域的 store(开发/CI) 后端：旁车 JSON CRUD + 模拟运行态。

func (b *storeBackend) DNSSettings() (DNSSettings, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.st.DNS, nil
}

func (b *storeBackend) SaveDNSSettings(s DNSSettings) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	// 保留旁车内部簿记（前端不回传 SavedStock/Prev*，避免被清空）。
	s.SavedStock = b.st.DNS.SavedStock
	s.PrevServers = b.st.DNS.PrevServers
	s.PrevAddrs = b.st.DNS.PrevAddrs
	b.st.DNS = s
	return b.flushLocked()
}

func (b *storeBackend) DNSDoH() (DNSDoH, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.st.DNSDoH, nil
}

func (b *storeBackend) SaveDNSDoH(d DNSDoH) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.DNSDoH = d
	return b.flushLocked()
}

func (b *storeBackend) DNSRecords() ([]DNSRecord, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]DNSRecord(nil), b.st.DNSRecords...), nil
}

func (b *storeBackend) SaveDNSRecords(list []DNSRecord) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.DNSRecords = append([]DNSRecord(nil), list...)
	return b.flushLocked()
}

func (b *storeBackend) DNSDomainRoutes() ([]DNSDomainRoute, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]DNSDomainRoute(nil), b.st.DNSDomainRoutes...), nil
}

func (b *storeBackend) SaveDNSDomainRoutes(list []DNSDomainRoute) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.DNSDomainRoutes = append([]DNSDomainRoute(nil), list...)
	return b.flushLocked()
}

// DNSCacheStats 在 store 后端返回确定性模拟数据（开发/CI 端到端用）。
func (b *storeBackend) DNSCacheStats() (DNSCacheStats, error) {
	hits, misses := int64(39690), int64(7786)
	return DNSCacheStats{
		Supported: true, CacheSize: 8000, Insertions: 12345, Evictions: 321,
		Hits: hits, Misses: misses, HitRatio: float64(hits) / float64(hits+misses),
	}, nil
}

func (b *storeBackend) FlushDNSCache() error { return nil }

func (b *storeBackend) DNSServiceInfo() (DNSSvcInfo, error) {
	return DNSSvcInfo{Backend: KindStore, FilterAAAASupported: true, DoHInstalled: true, PkgManager: "opkg", CanInstall: true}, nil
}

func (b *storeBackend) InstallDoH() (string, error) {
	return "store 后端：模拟已安装 https-dns-proxy（无操作）", nil
}

// saveDNSBookkeeping 持久化 applyDNS 计算出的内部簿记（SavedStock 快照 + 上次写入的
// server/address 精确值），供下次 apply 安全回滚 / 只删自己写过的值。
func (b *storeBackend) saveDNSBookkeeping(savedStock map[string]string, prevServers, prevAddrs []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.DNS.SavedStock = savedStock
	b.st.DNS.PrevServers = prevServers
	b.st.DNS.PrevAddrs = prevAddrs
	return b.flushLocked()
}
