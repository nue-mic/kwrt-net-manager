//go:build linux

package conntrack

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// tcpEstablished is the hex code for ESTABLISHED in /proc/net/tcp.
const tcpEstablished = "01"

func platformGet(ports []uint16) (Counts, error) {
	want := make(map[uint16]struct{}, len(ports))
	for _, p := range ports {
		want[p] = struct{}{}
	}
	out := make(Counts, len(ports))
	for _, p := range ports {
		out[p] = 0
	}

	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		if err := scan(file, want, out); err != nil && !os.IsNotExist(err) {
			return out, err
		}
	}
	return out, nil
}

func scan(file string, want map[uint16]struct{}, out Counts) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	first := true
	for s.Scan() {
		if first {
			first = false
			continue
		}
		line := s.Text()
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		// fields[1] is "local_address:port" with hex port
		laddr := fields[1]
		state := fields[3]
		if state != tcpEstablished {
			continue
		}
		i := strings.LastIndex(laddr, ":")
		if i < 0 {
			continue
		}
		portHex := laddr[i+1:]
		port, err := strconv.ParseUint(portHex, 16, 16)
		if err != nil {
			continue
		}
		p := uint16(port)
		if _, ok := want[p]; ok {
			out[p]++
		}
	}
	return s.Err()
}
