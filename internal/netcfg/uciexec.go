package netcfg

import (
	"bytes"
	"os/exec"
	"strings"
)

// runner abstracts command execution so the uci backend can be unit tested with
// a fake that records invocations and returns canned output — no OpenWrt host
// required. stdin is fed to the command (used for `uci batch`).
type runner interface {
	Run(stdin, name string, args ...string) (stdout string, err error)
}

// realRunner shells out for real. Stdout+stderr are merged so error messages
// from uci/ip surface in the returned string.
type realRunner struct{}

func (realRunner) Run(stdin, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// managedSections returns the names of sections in `uci show <config>` output
// that carry our managed-by marker, so apply only ever deletes/recreates the
// sections this daemon owns — never stock or LuCI/operator-authored config.
// This marker-scoped discipline is what keeps the integration upgrade-safe
// across OpenWrt versions.
func managedSections(show, config string) []string {
	return managedSectionsMarker(show, config, managedMarker)
}

// managedSectionsMarker is managedSections with an explicit marker value, so the
// IPv6 projection can track its own sections (managedMarkerV6) independently of
// the IPv4 ones — neither apply deletes the other's sections.
func managedSectionsMarker(show, config, marker string) []string {
	prefix := config + "."
	suffix := "." + managedOpt + "='" + marker + "'"
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(show, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
			continue
		}
		// line: dhcp.<name>.managed_by='kwrt-net-manager'
		rest := strings.TrimPrefix(line, prefix)
		name := rest[:strings.IndexByte(rest, '.')]
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}
