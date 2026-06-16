package netcfg

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/mia-clark/kwrt-net-manager/internal/eventbus"
)

// Service 的 IPv6 领域 API。沿用 IPv4 的 mutex+idFn+publish 模式：每个写操作
// 串行化、校验、保存到后端（uci 投射 / store 旁车）、发 TypeIPv6Changed 事件。

func (s *Service) publishV6(action string, count int) {
	s.publish(eventbus.TypeIPv6Changed, action, count)
}

// nextV6ID 在 existing 之外取 prefix / prefix2 / prefix3 … 第一个空位。
func nextV6ID(existing []string, prefix string) string {
	used := map[string]bool{}
	for _, e := range existing {
		used[e] = true
	}
	if !used[prefix] {
		return prefix
	}
	for i := 2; ; i++ {
		cand := prefix + itoa(i)
		if !used[cand] {
			return cand
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// generateDUID 生成一个随机 DUID-UUID（RFC6355，type 4）：0004 + 16 字节随机。
func generateDUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "0004" + hex.EncodeToString(b[:])
}

// GenerateDUID 暴露给「重新生成 DUID」动作。
func (s *Service) GenerateDUID() string { return generateDUID() }

// ================= WANv6（IPv6 外网） =================

func (s *Service) ListWANv6() ([]WANv6, error) { return s.be.WANv6s() }

func (s *Service) GetWANv6(id string) (WANv6, error) {
	list, err := s.be.WANv6s()
	if err != nil {
		return WANv6{}, err
	}
	for _, x := range list {
		if x.ID == id {
			return x, nil
		}
	}
	return WANv6{}, ErrNotFound
}

func (s *Service) CreateWANv6(in WANv6) (WANv6, error) {
	if err := validateWANv6(&in); err != nil {
		return WANv6{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.WANv6s()
	if err != nil {
		return WANv6{}, err
	}
	ids := idsOfWANv6(list)
	in.ID = uciName(orV6(in.ID, nextV6ID(ids, "wan6")))
	if in.Name == "" {
		in.Name = in.ID
	}
	in.Managed = true
	list = append(list, in)
	if err := s.be.SaveWANv6s(list); err != nil {
		return WANv6{}, err
	}
	s.publishV6("create", len(list))
	return in, nil
}

func (s *Service) UpdateWANv6(id string, in WANv6) (WANv6, error) {
	if err := validateWANv6(&in); err != nil {
		return WANv6{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.WANv6s()
	if err != nil {
		return WANv6{}, err
	}
	idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
	if idx < 0 {
		return WANv6{}, ErrNotFound
	}
	in.ID = id
	if in.Name == "" {
		in.Name = id
	}
	in.Managed = true
	list[idx] = in
	if err := s.be.SaveWANv6s(list); err != nil {
		return WANv6{}, err
	}
	s.publishV6("update", len(list))
	return in, nil
}

func (s *Service) DeleteWANv6(id string) error {
	return s.mutateWANv6("delete", func(list []WANv6) ([]WANv6, error) {
		out, removed := dropByID(list, func(x WANv6) string { return x.ID }, id)
		if !removed {
			return nil, ErrNotFound
		}
		return out, nil
	})
}

func (s *Service) SetWANv6Enabled(id string, on bool) error {
	return s.mutateWANv6("toggle", func(list []WANv6) ([]WANv6, error) {
		idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
		if idx < 0 {
			return nil, ErrNotFound
		}
		list[idx].Enabled = on
		list[idx].Managed = true
		return list, nil
	})
}

func (s *Service) BatchWANv6(action string, ids []string) error {
	set := toSet(ids)
	return s.mutateWANv6(action, func(list []WANv6) ([]WANv6, error) {
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

// RegenWANv6DUID 给一条 WANv6 重新生成随机 DUID 并保存。
func (s *Service) RegenWANv6DUID(id string) (WANv6, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.WANv6s()
	if err != nil {
		return WANv6{}, err
	}
	idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
	if idx < 0 {
		return WANv6{}, ErrNotFound
	}
	list[idx].ClientID = generateDUID()
	list[idx].Managed = true
	if err := s.be.SaveWANv6s(list); err != nil {
		return WANv6{}, err
	}
	s.publishV6("update", len(list))
	return list[idx], nil
}

func (s *Service) mutateWANv6(action string, fn func([]WANv6) ([]WANv6, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.WANv6s()
	if err != nil {
		return err
	}
	next, err := fn(list)
	if err != nil {
		return err
	}
	if err := s.be.SaveWANv6s(next); err != nil {
		return err
	}
	s.publishV6(action, len(next))
	return nil
}

func idsOfWANv6(list []WANv6) []string {
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = x.ID
	}
	return out
}

// ================= LANv6（IPv6 内网） =================

func (s *Service) ListLANv6() ([]LANv6, error) { return s.be.LANv6s() }

func (s *Service) GetLANv6(id string) (LANv6, error) {
	list, err := s.be.LANv6s()
	if err != nil {
		return LANv6{}, err
	}
	for _, x := range list {
		if x.ID == id {
			return x, nil
		}
	}
	return LANv6{}, ErrNotFound
}

func (s *Service) CreateLANv6(in LANv6) (LANv6, error) {
	if err := validateLANv6(&in); err != nil {
		return LANv6{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.LANv6s()
	if err != nil {
		return LANv6{}, err
	}
	in.ID = uciName(in.Interface)
	for _, x := range list {
		if x.ID == in.ID {
			return LANv6{}, errors.New("该内网接口已存在 IPv6 配置，请直接编辑")
		}
	}
	in.Managed = true
	list = append(list, in)
	if err := s.be.SaveLANv6s(list); err != nil {
		return LANv6{}, err
	}
	s.publishV6("create", len(list))
	return in, nil
}

func (s *Service) UpdateLANv6(id string, in LANv6) (LANv6, error) {
	if err := validateLANv6(&in); err != nil {
		return LANv6{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.LANv6s()
	if err != nil {
		return LANv6{}, err
	}
	idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
	if idx < 0 {
		return LANv6{}, ErrNotFound
	}
	in.ID = id
	in.Managed = true
	list[idx] = in
	if err := s.be.SaveLANv6s(list); err != nil {
		return LANv6{}, err
	}
	s.publishV6("update", len(list))
	return in, nil
}

func (s *Service) DeleteLANv6(id string) error {
	return s.mutateLANv6("delete", func(list []LANv6) ([]LANv6, error) {
		out, removed := dropByID(list, func(x LANv6) string { return x.ID }, id)
		if !removed {
			return nil, ErrNotFound
		}
		return out, nil
	})
}

func (s *Service) SetLANv6Enabled(id string, on bool) error {
	return s.mutateLANv6("toggle", func(list []LANv6) ([]LANv6, error) {
		idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
		if idx < 0 {
			return nil, ErrNotFound
		}
		list[idx].Enabled = on
		list[idx].Managed = true
		return list, nil
	})
}

func (s *Service) BatchLANv6(action string, ids []string) error {
	set := toSet(ids)
	return s.mutateLANv6(action, func(list []LANv6) ([]LANv6, error) {
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

func (s *Service) mutateLANv6(action string, fn func([]LANv6) ([]LANv6, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.LANv6s()
	if err != nil {
		return err
	}
	next, err := fn(list)
	if err != nil {
		return err
	}
	if err := s.be.SaveLANv6s(next); err != nil {
		return err
	}
	s.publishV6(action, len(next))
	return nil
}

// ================= DHCPv6 终端（只读） =================

// ListLeasesV6 返回 DHCPv6 租约，按接口/关键字过滤。
func (s *Service) ListLeasesV6(f LeaseFilter) ([]LeaseV6, error) {
	leases, err := s.be.LeasesV6()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(f.Query))
	out := leases[:0:0]
	for _, l := range leases {
		if f.Interface != "" && l.Interface != f.Interface {
			continue
		}
		if q != "" && !leaseV6Matches(l, q) {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

func leaseV6Matches(l LeaseV6, q string) bool {
	return strings.Contains(strings.ToLower(l.Hostname), q) ||
		strings.Contains(strings.ToLower(l.MAC), q) ||
		strings.Contains(strings.ToLower(l.IPv6Addr), q) ||
		strings.Contains(strings.ToLower(l.LocalLink), q) ||
		strings.Contains(strings.ToLower(l.DUID), q)
}

// ================= 前缀静态分配 =================

func (s *Service) ListPrefixStaticsV6() ([]PrefixStaticV6, error) { return s.be.PrefixStaticsV6() }

func (s *Service) CreatePrefixStaticV6(in PrefixStaticV6) (PrefixStaticV6, error) {
	if err := validatePrefixStaticV6(&in); err != nil {
		return PrefixStaticV6{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.PrefixStaticsV6()
	if err != nil {
		return PrefixStaticV6{}, err
	}
	in.ID = s.idFn("ps6")
	in.Managed = true
	list = append(list, in)
	if err := s.be.SavePrefixStaticsV6(list); err != nil {
		return PrefixStaticV6{}, err
	}
	s.publishV6("create", len(list))
	return in, nil
}

func (s *Service) UpdatePrefixStaticV6(id string, in PrefixStaticV6) (PrefixStaticV6, error) {
	if err := validatePrefixStaticV6(&in); err != nil {
		return PrefixStaticV6{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.PrefixStaticsV6()
	if err != nil {
		return PrefixStaticV6{}, err
	}
	idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
	if idx < 0 {
		return PrefixStaticV6{}, ErrNotFound
	}
	in.ID = id
	in.Managed = true
	list[idx] = in
	if err := s.be.SavePrefixStaticsV6(list); err != nil {
		return PrefixStaticV6{}, err
	}
	s.publishV6("update", len(list))
	return in, nil
}

func (s *Service) DeletePrefixStaticV6(id string) error {
	return s.mutatePrefixV6("delete", func(list []PrefixStaticV6) ([]PrefixStaticV6, error) {
		out, removed := dropByID(list, func(x PrefixStaticV6) string { return x.ID }, id)
		if !removed {
			return nil, ErrNotFound
		}
		return out, nil
	})
}

func (s *Service) SetPrefixStaticV6Enabled(id string, on bool) error {
	return s.mutatePrefixV6("toggle", func(list []PrefixStaticV6) ([]PrefixStaticV6, error) {
		idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
		if idx < 0 {
			return nil, ErrNotFound
		}
		list[idx].Enabled = on
		list[idx].Managed = true
		return list, nil
	})
}

func (s *Service) BatchPrefixStaticsV6(action string, ids []string) error {
	set := toSet(ids)
	return s.mutatePrefixV6(action, func(list []PrefixStaticV6) ([]PrefixStaticV6, error) {
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

func (s *Service) mutatePrefixV6(action string, fn func([]PrefixStaticV6) ([]PrefixStaticV6, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.PrefixStaticsV6()
	if err != nil {
		return err
	}
	next, err := fn(list)
	if err != nil {
		return err
	}
	if err := s.be.SavePrefixStaticsV6(next); err != nil {
		return err
	}
	s.publishV6(action, len(next))
	return nil
}

// ================= DHCPv6 接入控制（黑白名单） =================

func (s *Service) GetACLv6() (ACLv6, error) {
	acl, err := s.be.ACLv6()
	if err != nil {
		return ACLv6{}, err
	}
	if acl.Mode == "" {
		acl.Mode = ACLBlacklist
	}
	if acl.Entries == nil {
		acl.Entries = []ACLv6Entry{}
	}
	return acl, nil
}

func (s *Service) SetACLv6Mode(mode string) (ACLv6, error) {
	if mode != ACLBlacklist && mode != ACLWhitelist {
		return ACLv6{}, errors.New("模式必须是 blacklist 或 whitelist")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acl, err := s.GetACLv6()
	if err != nil {
		return ACLv6{}, err
	}
	acl.Mode = mode
	if err := s.be.SaveACLv6(acl); err != nil {
		return ACLv6{}, err
	}
	s.publishV6("update", len(acl.Entries))
	return acl, nil
}

func (s *Service) AddACLv6Entry(in ACLv6Entry) (ACLv6Entry, error) {
	if err := validateACLv6Entry(&in); err != nil {
		return ACLv6Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acl, err := s.GetACLv6()
	if err != nil {
		return ACLv6Entry{}, err
	}
	in.ID = s.idFn("aclv6")
	in.Managed = true
	acl.Entries = append(acl.Entries, in)
	if err := s.be.SaveACLv6(acl); err != nil {
		return ACLv6Entry{}, err
	}
	s.publishV6("create", len(acl.Entries))
	return in, nil
}

func (s *Service) UpdateACLv6Entry(id string, in ACLv6Entry) (ACLv6Entry, error) {
	if err := validateACLv6Entry(&in); err != nil {
		return ACLv6Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acl, err := s.GetACLv6()
	if err != nil {
		return ACLv6Entry{}, err
	}
	idx := indexByID(len(acl.Entries), func(i int) string { return acl.Entries[i].ID }, id)
	if idx < 0 {
		return ACLv6Entry{}, ErrNotFound
	}
	in.ID = id
	in.Managed = true
	acl.Entries[idx] = in
	if err := s.be.SaveACLv6(acl); err != nil {
		return ACLv6Entry{}, err
	}
	s.publishV6("update", len(acl.Entries))
	return in, nil
}

func (s *Service) DeleteACLv6Entry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	acl, err := s.GetACLv6()
	if err != nil {
		return err
	}
	out, removed := dropByID(acl.Entries, func(x ACLv6Entry) string { return x.ID }, id)
	if !removed {
		return ErrNotFound
	}
	acl.Entries = out
	if err := s.be.SaveACLv6(acl); err != nil {
		return err
	}
	s.publishV6("delete", len(acl.Entries))
	return nil
}

func (s *Service) ToggleACLv6Entry(id string) (ACLv6Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acl, err := s.GetACLv6()
	if err != nil {
		return ACLv6Entry{}, err
	}
	idx := indexByID(len(acl.Entries), func(i int) string { return acl.Entries[i].ID }, id)
	if idx < 0 {
		return ACLv6Entry{}, ErrNotFound
	}
	acl.Entries[idx].Enabled = !acl.Entries[idx].Enabled
	acl.Entries[idx].Managed = true
	if err := s.be.SaveACLv6(acl); err != nil {
		return ACLv6Entry{}, err
	}
	s.publishV6("toggle", len(acl.Entries))
	return acl.Entries[idx], nil
}

// ================= 邻居列表 / 线路详情 / 服务信息（只读 + 动作） =================

func (s *Service) ListNeighborsV6() ([]NeighborV6, error) { return s.be.NeighborsV6() }

func (s *Service) DeleteNeighborV6(addr, dev string) error {
	if strings.TrimSpace(addr) == "" {
		return errors.New("缺少邻居地址")
	}
	if err := s.be.DeleteNeighborV6(addr, dev); err != nil {
		return err
	}
	s.publishV6("delete", 0)
	return nil
}

func (s *Service) FlushNeighborsV6(dev string) error {
	if err := s.be.FlushNeighborsV6(dev); err != nil {
		return err
	}
	s.publishV6("apply", 0)
	return nil
}

func (s *Service) ListLinesV6() ([]LineV6, error) { return s.be.LinesV6() }

func (s *Service) DHCPv6ServiceInfo() (DHCPv6SvcInfo, error) { return s.be.DHCPv6ServiceInfo() }

func (s *Service) TransitionPkg(proto string) (bool, string, error) {
	return s.be.TransitionPkg(proto)
}

func orV6(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
