package netcfg

import (
	"errors"

	"github.com/nue-mic/kwrt-net-manager/internal/eventbus"
)

// ================= DNS 全局设置 / DoH（单例） =================

// GetDNSSettings 返回全局 DNS 设置。
func (s *Service) GetDNSSettings() (DNSSettings, error) { return s.be.DNSSettings() }

// SaveDNSSettings 校验并保存全局 DNS 设置。
func (s *Service) SaveDNSSettings(in DNSSettings) (DNSSettings, error) {
	if err := validateDNSSettings(&in); err != nil {
		return DNSSettings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.be.SaveDNSSettings(in); err != nil {
		return DNSSettings{}, err
	}
	s.publish(eventbus.TypeDNSChanged, "update", 0)
	return s.be.DNSSettings()
}

// GetDNSDoH 返回 DoH 配置。
func (s *Service) GetDNSDoH() (DNSDoH, error) { return s.be.DNSDoH() }

// SaveDNSDoH 校验并保存 DoH 配置。
func (s *Service) SaveDNSDoH(in DNSDoH) (DNSDoH, error) {
	if err := validateDNSDoH(&in); err != nil {
		return DNSDoH{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.be.SaveDNSDoH(in); err != nil {
		return DNSDoH{}, err
	}
	s.publish(eventbus.TypeDNSChanged, "update", 0)
	return s.be.DNSDoH()
}

// ================= DNS 自定义解析记录 =================

// ListDNSRecords 返回全部自定义解析记录。
func (s *Service) ListDNSRecords() ([]DNSRecord, error) { return s.be.DNSRecords() }

// CreateDNSRecord 校验 + 新增一条记录。
func (s *Service) CreateDNSRecord(in DNSRecord) (DNSRecord, error) {
	if err := validateDNSRecord(&in); err != nil {
		return DNSRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.DNSRecords()
	if err != nil {
		return DNSRecord{}, err
	}
	in.ID = s.idFn("dns")
	in.Managed = true
	list = append(list, in)
	if err := s.be.SaveDNSRecords(list); err != nil {
		return DNSRecord{}, err
	}
	s.publish(eventbus.TypeDNSChanged, "create", len(list))
	return in, nil
}

// UpdateDNSRecord 校验 + 替换一条记录。
func (s *Service) UpdateDNSRecord(id string, in DNSRecord) (DNSRecord, error) {
	if err := validateDNSRecord(&in); err != nil {
		return DNSRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.DNSRecords()
	if err != nil {
		return DNSRecord{}, err
	}
	idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
	if idx < 0 {
		return DNSRecord{}, ErrNotFound
	}
	in.ID = id
	in.Managed = true
	list[idx] = in
	if err := s.be.SaveDNSRecords(list); err != nil {
		return DNSRecord{}, err
	}
	s.publish(eventbus.TypeDNSChanged, "update", len(list))
	return in, nil
}

// DeleteDNSRecord 删除一条记录。
func (s *Service) DeleteDNSRecord(id string) error {
	return s.mutateDNSRecords("delete", func(list []DNSRecord) ([]DNSRecord, error) {
		out, removed := dropByID(list, func(x DNSRecord) string { return x.ID }, id)
		if !removed {
			return nil, ErrNotFound
		}
		return out, nil
	})
}

// SetDNSRecordEnabled 启停一条记录。
func (s *Service) SetDNSRecordEnabled(id string, on bool) error {
	return s.mutateDNSRecords("toggle", func(list []DNSRecord) ([]DNSRecord, error) {
		idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
		if idx < 0 {
			return nil, ErrNotFound
		}
		list[idx].Enabled = on
		list[idx].Managed = true
		return list, nil
	})
}

// BatchDNSRecords 批量启用/停用/删除。
func (s *Service) BatchDNSRecords(action string, ids []string) error {
	set := toSet(ids)
	return s.mutateDNSRecords(action, func(list []DNSRecord) ([]DNSRecord, error) {
		switch action {
		case "enable", "disable":
			on := action == "enable"
			for i := range list {
				if set[list[i].ID] {
					list[i].Enabled = on
					list[i].Managed = true
				}
			}
			return list, nil
		case "delete":
			out := list[:0:0]
			for _, x := range list {
				if !set[x.ID] {
					out = append(out, x)
				}
			}
			return out, nil
		default:
			return nil, errors.New("不支持的批量操作")
		}
	})
}

func (s *Service) mutateDNSRecords(action string, fn func([]DNSRecord) ([]DNSRecord, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.DNSRecords()
	if err != nil {
		return err
	}
	next, err := fn(list)
	if err != nil {
		return err
	}
	if err := s.be.SaveDNSRecords(next); err != nil {
		return err
	}
	s.publish(eventbus.TypeDNSChanged, action, len(next))
	return nil
}

// ================= DNS 域名分流（多线路DNS 降级） =================

// ListDNSDomainRoutes 返回全部域名分流规则。
func (s *Service) ListDNSDomainRoutes() ([]DNSDomainRoute, error) { return s.be.DNSDomainRoutes() }

// CreateDNSDomainRoute 校验 + 新增。
func (s *Service) CreateDNSDomainRoute(in DNSDomainRoute) (DNSDomainRoute, error) {
	if err := validateDNSDomainRoute(&in); err != nil {
		return DNSDomainRoute{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.DNSDomainRoutes()
	if err != nil {
		return DNSDomainRoute{}, err
	}
	in.ID = s.idFn("dnsr")
	in.Managed = true
	list = append(list, in)
	if err := s.be.SaveDNSDomainRoutes(list); err != nil {
		return DNSDomainRoute{}, err
	}
	s.publish(eventbus.TypeDNSChanged, "create", len(list))
	return in, nil
}

// UpdateDNSDomainRoute 校验 + 替换。
func (s *Service) UpdateDNSDomainRoute(id string, in DNSDomainRoute) (DNSDomainRoute, error) {
	if err := validateDNSDomainRoute(&in); err != nil {
		return DNSDomainRoute{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.DNSDomainRoutes()
	if err != nil {
		return DNSDomainRoute{}, err
	}
	idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
	if idx < 0 {
		return DNSDomainRoute{}, ErrNotFound
	}
	in.ID = id
	in.Managed = true
	list[idx] = in
	if err := s.be.SaveDNSDomainRoutes(list); err != nil {
		return DNSDomainRoute{}, err
	}
	s.publish(eventbus.TypeDNSChanged, "update", len(list))
	return in, nil
}

// DeleteDNSDomainRoute 删除。
func (s *Service) DeleteDNSDomainRoute(id string) error {
	return s.mutateDNSDomainRoutes("delete", func(list []DNSDomainRoute) ([]DNSDomainRoute, error) {
		out, removed := dropByID(list, func(x DNSDomainRoute) string { return x.ID }, id)
		if !removed {
			return nil, ErrNotFound
		}
		return out, nil
	})
}

// SetDNSDomainRouteEnabled 启停。
func (s *Service) SetDNSDomainRouteEnabled(id string, on bool) error {
	return s.mutateDNSDomainRoutes("toggle", func(list []DNSDomainRoute) ([]DNSDomainRoute, error) {
		idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
		if idx < 0 {
			return nil, ErrNotFound
		}
		list[idx].Enabled = on
		list[idx].Managed = true
		return list, nil
	})
}

// BatchDNSDomainRoutes 批量启用/停用/删除。
func (s *Service) BatchDNSDomainRoutes(action string, ids []string) error {
	set := toSet(ids)
	return s.mutateDNSDomainRoutes(action, func(list []DNSDomainRoute) ([]DNSDomainRoute, error) {
		switch action {
		case "enable", "disable":
			on := action == "enable"
			for i := range list {
				if set[list[i].ID] {
					list[i].Enabled = on
					list[i].Managed = true
				}
			}
			return list, nil
		case "delete":
			out := list[:0:0]
			for _, x := range list {
				if !set[x.ID] {
					out = append(out, x)
				}
			}
			return out, nil
		default:
			return nil, errors.New("不支持的批量操作")
		}
	})
}

func (s *Service) mutateDNSDomainRoutes(action string, fn func([]DNSDomainRoute) ([]DNSDomainRoute, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.DNSDomainRoutes()
	if err != nil {
		return err
	}
	next, err := fn(list)
	if err != nil {
		return err
	}
	if err := s.be.SaveDNSDomainRoutes(next); err != nil {
		return err
	}
	s.publish(eventbus.TypeDNSChanged, action, len(next))
	return nil
}

// ================= 运行态 / 探测 / 维护 =================

// DNSCacheStats 返回 dnsmasq 缓存累计统计（只读）。
func (s *Service) DNSCacheStats() (DNSCacheStats, error) { return s.be.DNSCacheStats() }

// FlushDNSCache 清空 dnsmasq 缓存（SIGHUP）。
func (s *Service) FlushDNSCache() error {
	if err := s.be.FlushDNSCache(); err != nil {
		return err
	}
	s.publish(eventbus.TypeDNSChanged, "apply", 0)
	return nil
}

// DNSServiceInfo 返回 DNS 能力探测。
func (s *Service) DNSServiceInfo() (DNSSvcInfo, error) { return s.be.DNSServiceInfo() }

// InstallDoH 一键安装 https-dns-proxy。
func (s *Service) InstallDoH() (string, error) {
	out, err := s.be.InstallDoH()
	if err != nil {
		return out, err
	}
	s.publish(eventbus.TypeDNSChanged, "apply", 0)
	return out, nil
}
