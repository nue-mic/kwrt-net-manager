package netcfg

// 策略路由（ip rule / config rule）领域服务。规则把匹配的流量导向指定路由表，
// 配合「带 table 的静态路由」实现按源地址/入接口分流到某条线路。
// 沿用 Route 的 mutate + publish 模式。

import (
	"errors"
	"net"
	"strconv"
	"strings"

	"github.com/nue-mic/kwrt-net-manager/internal/eventbus"
)

func (s *Service) ListPolicyRules() ([]PolicyRule, error) { return s.be.PolicyRules() }

func (s *Service) CreatePolicyRule(in PolicyRule) (PolicyRule, error) {
	if err := validatePolicyRule(&in); err != nil {
		return PolicyRule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.PolicyRules()
	if err != nil {
		return PolicyRule{}, err
	}
	in.ID = s.idFn("prule")
	in.Managed = true
	list = append(list, in)
	if err := s.be.SavePolicyRules(list); err != nil {
		return PolicyRule{}, err
	}
	s.publish(eventbus.TypeRouteChanged, "create", len(list))
	return in, nil
}

func (s *Service) UpdatePolicyRule(id string, in PolicyRule) (PolicyRule, error) {
	if err := validatePolicyRule(&in); err != nil {
		return PolicyRule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.PolicyRules()
	if err != nil {
		return PolicyRule{}, err
	}
	idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
	if idx < 0 {
		return PolicyRule{}, ErrNotFound
	}
	in.ID = id
	in.Managed = true
	list[idx] = in
	if err := s.be.SavePolicyRules(list); err != nil {
		return PolicyRule{}, err
	}
	s.publish(eventbus.TypeRouteChanged, "update", len(list))
	return in, nil
}

func (s *Service) DeletePolicyRule(id string) error {
	return s.mutatePolicyRules("delete", func(list []PolicyRule) ([]PolicyRule, error) {
		out, removed := dropByID(list, func(x PolicyRule) string { return x.ID }, id)
		if !removed {
			return nil, ErrNotFound
		}
		return out, nil
	})
}

func (s *Service) SetPolicyRuleEnabled(id string, on bool) error {
	return s.mutatePolicyRules("toggle", func(list []PolicyRule) ([]PolicyRule, error) {
		idx := indexByID(len(list), func(i int) string { return list[i].ID }, id)
		if idx < 0 {
			return nil, ErrNotFound
		}
		list[idx].Enabled = on
		list[idx].Managed = true
		return list, nil
	})
}

func (s *Service) BatchPolicyRules(action string, ids []string) error {
	set := toSet(ids)
	return s.mutatePolicyRules(action, func(list []PolicyRule) ([]PolicyRule, error) {
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

func (s *Service) mutatePolicyRules(action string, fn func([]PolicyRule) ([]PolicyRule, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.be.PolicyRules()
	if err != nil {
		return err
	}
	next, err := fn(list)
	if err != nil {
		return err
	}
	if err := s.be.SavePolicyRules(next); err != nil {
		return err
	}
	s.publish(eventbus.TypeRouteChanged, action, len(next))
	return nil
}

func validatePolicyRule(r *PolicyRule) error {
	switch r.Family {
	case "", FamilyIPv4:
		r.Family = FamilyIPv4
	case FamilyIPv6:
	default:
		return errors.New("协议栈必须是 ipv4 或 ipv6")
	}
	r.Src = strings.TrimSpace(r.Src)
	r.Dest = strings.TrimSpace(r.Dest)
	r.InIface = strings.TrimSpace(r.InIface)
	r.Lookup = strings.TrimSpace(r.Lookup)
	if r.Lookup == "" {
		return errors.New("必须指定要查询的路由表号（配合带「表」的静态路由）")
	}
	if n, err := strconv.Atoi(r.Lookup); err != nil || n < 1 || n > 255 {
		return errors.New("路由表号必须是 1-255 的数字")
	}
	if r.Priority < 0 || r.Priority > 65535 {
		return errors.New("优先级范围 0-65535")
	}
	for _, c := range []string{r.Src, r.Dest} {
		if c != "" && !looksLikeIPOrCIDR(c) {
			return errors.New("源/目标地址必须是 IP 或网段（如 192.168.1.0/24）")
		}
	}
	return nil
}

func looksLikeIPOrCIDR(s string) bool {
	if _, _, err := net.ParseCIDR(s); err == nil {
		return true
	}
	return net.ParseIP(s) != nil
}
