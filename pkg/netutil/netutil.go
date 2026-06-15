// Package netutil holds the pure helper functions the network-config domain
// needs: IPv4 <-> integer conversion, netmask <-> CIDR prefix, MAC
// normalization, address-range arithmetic, DHCP exclude-line parsing and
// dnsmasq lease-line parsing. Everything here is side-effect free and unit
// tested, so the netcfg backends can rely on it without re-deriving the math.
package netutil

import (
	"net"
	"strconv"
	"strings"
)

// IPv4ToUint32 parses a dotted-quad IPv4 string into its 32-bit value.
// ok is false for anything that is not a valid IPv4 address (incl. IPv6).
func IPv4ToUint32(s string) (uint32, bool) {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return 0, false
	}
	v4 := ip.To4()
	if v4 == nil {
		return 0, false
	}
	return uint32(v4[0])<<24 | uint32(v4[1])<<16 | uint32(v4[2])<<8 | uint32(v4[3]), true
}

// Uint32ToIPv4 renders a 32-bit value as a dotted-quad string.
func Uint32ToIPv4(u uint32) string {
	return net.IPv4(byte(u>>24), byte(u>>16), byte(u>>8), byte(u)).To4().String()
}

// IsIPv4 reports whether s is a valid dotted-quad IPv4 address.
func IsIPv4(s string) bool {
	_, ok := IPv4ToUint32(s)
	return ok
}

// IsIPv6 reports whether s is a valid IPv6 address (and not an IPv4).
func IsIPv6(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	return ip != nil && ip.To4() == nil
}

// IsIP reports whether s is any valid IP address.
func IsIP(s string) bool {
	return net.ParseIP(strings.TrimSpace(s)) != nil
}

// NormalizeMAC upper-cases and colon-joins a MAC address. It accepts colon- or
// hyphen-separated input (e.g. "aa-bb-cc-dd-ee-ff"). Returns "" when s is not a
// 6-octet MAC.
func NormalizeMAC(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	t = strings.ReplaceAll(t, "-", ":")
	hw, err := net.ParseMAC(t)
	if err != nil || len(hw) != 6 {
		return ""
	}
	parts := make([]string, 6)
	for i, b := range hw {
		parts[i] = strings.ToUpper(byteHex(b))
	}
	return strings.Join(parts, ":")
}

func byteHex(b byte) string {
	const hex = "0123456789abcdef"
	return string([]byte{hex[b>>4], hex[b&0x0f]})
}

// IsMAC reports whether s is a valid 6-octet MAC address.
func IsMAC(s string) bool { return NormalizeMAC(s) != "" }

// MaskToPrefix converts a dotted netmask ("255.255.255.0") to its CIDR prefix
// length (24). ok is false if the mask is not a valid contiguous IPv4 netmask.
func MaskToPrefix(mask string) (int, bool) {
	u, ok := IPv4ToUint32(mask)
	if !ok {
		return 0, false
	}
	ones, allOnesSeen := 0, true
	for i := 31; i >= 0; i-- {
		bit := (u >> uint(i)) & 1
		if bit == 1 {
			if !allOnesSeen {
				return 0, false // a 1 after a 0 → not contiguous
			}
			ones++
		} else {
			allOnesSeen = false
		}
	}
	return ones, true
}

// PrefixToMask converts a CIDR prefix length (0-32) to a dotted netmask.
// Returns "" for an out-of-range prefix.
func PrefixToMask(prefix int) string {
	if prefix < 0 || prefix > 32 {
		return ""
	}
	if prefix == 0 {
		return "0.0.0.0"
	}
	mask := uint32(0xFFFFFFFF) << uint(32-prefix)
	return Uint32ToIPv4(mask)
}

// IsValidNetmask reports whether mask is a valid contiguous IPv4 netmask.
func IsValidNetmask(mask string) bool {
	_, ok := MaskToPrefix(mask)
	return ok
}

// NetworkBase returns the network address for ip under mask (ip AND mask), as a
// dotted-quad string. ok is false if either input is invalid.
func NetworkBase(ip, mask string) (string, bool) {
	iu, ok1 := IPv4ToUint32(ip)
	mu, ok2 := IPv4ToUint32(mask)
	if !ok1 || !ok2 {
		return "", false
	}
	return Uint32ToIPv4(iu & mu), true
}

// SameSubnet reports whether a and b share the network under mask.
func SameSubnet(a, b, mask string) bool {
	na, ok1 := NetworkBase(a, mask)
	nb, ok2 := NetworkBase(b, mask)
	return ok1 && ok2 && na == nb
}

// RangeCount returns the inclusive count of IPv4 addresses in [start, end].
// ok is false if either bound is invalid or end < start.
func RangeCount(start, end string) (int, bool) {
	su, ok1 := IPv4ToUint32(start)
	eu, ok2 := IPv4ToUint32(end)
	if !ok1 || !ok2 || eu < su {
		return 0, false
	}
	return int(eu-su) + 1, true
}

// IPInRange reports whether ip falls within [start, end] inclusive.
func IPInRange(ip, start, end string) bool {
	iu, ok0 := IPv4ToUint32(ip)
	su, ok1 := IPv4ToUint32(start)
	eu, ok2 := IPv4ToUint32(end)
	if !ok0 || !ok1 || !ok2 {
		return false
	}
	return iu >= su && iu <= eu
}

// DHCPStartLimit computes the dnsmasq/UCI "start" (host offset from the network
// base) and "limit" (address count) for an iKuai-style absolute start–end pool
// on an interface whose IP/mask are given. ok is false on invalid input or when
// the pool is not inside the interface subnet.
func DHCPStartLimit(ifaceIP, mask, poolStart, poolEnd string) (start, limit int, ok bool) {
	base, ok0 := NetworkBase(ifaceIP, mask)
	bu, _ := IPv4ToUint32(base)
	su, ok1 := IPv4ToUint32(poolStart)
	eu, ok2 := IPv4ToUint32(poolEnd)
	if !ok0 || !ok1 || !ok2 || eu < su || su < bu {
		return 0, 0, false
	}
	if !SameSubnet(poolStart, ifaceIP, mask) || !SameSubnet(poolEnd, ifaceIP, mask) {
		return 0, 0, false
	}
	return int(su - bu), int(eu-su) + 1, true
}

// ParseExcludeLine parses one line of an iKuai DHCP "exclude addresses" box.
// Accepted forms: "192.168.1.5" (single) or "192.168.1.5-192.168.1.10" (range).
// Returns the inclusive [startIP, endIP]; for a single IP both are equal.
// ok is false for malformed input.
func ParseExcludeLine(line string) (startIP, endIP string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" {
		return "", "", false
	}
	if i := strings.IndexByte(t, '-'); i >= 0 {
		a := strings.TrimSpace(t[:i])
		b := strings.TrimSpace(t[i+1:])
		au, ok1 := IPv4ToUint32(a)
		bu, ok2 := IPv4ToUint32(b)
		if !ok1 || !ok2 || bu < au {
			return "", "", false
		}
		return a, b, true
	}
	if !IsIPv4(t) {
		return "", "", false
	}
	return t, t, true
}

// ParsedLease is one decoded dnsmasq lease-file line.
type ParsedLease struct {
	Expiry   int64  // epoch seconds; 0 means infinite/static
	MAC      string // normalized upper-case colon form
	IP       string
	Hostname string // "" when the file had "*"
	ClientID string // "" when the file had "*"
}

// ParseLeaseLine parses one line of /tmp/dhcp.leases. The dnsmasq format is:
//
//	<expiry-epoch> <mac> <ip> <hostname|*> <client-id|*>
//
// ok is false for a blank/short/malformed line.
func ParseLeaseLine(line string) (ParsedLease, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 3 {
		return ParsedLease{}, false
	}
	expiry, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return ParsedLease{}, false
	}
	mac := NormalizeMAC(fields[1])
	if mac == "" {
		return ParsedLease{}, false
	}
	if !IsIP(fields[2]) {
		return ParsedLease{}, false
	}
	pl := ParsedLease{Expiry: expiry, MAC: mac, IP: fields[2]}
	if len(fields) >= 4 && fields[3] != "*" {
		pl.Hostname = fields[3]
	}
	if len(fields) >= 5 && fields[4] != "*" {
		pl.ClientID = fields[4]
	}
	return pl, true
}
