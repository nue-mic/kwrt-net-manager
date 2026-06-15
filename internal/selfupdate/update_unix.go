//go:build !windows

package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// spawnUpdater launches the detached updater on Unix-like systems.
//
// On systemd, `systemctl restart` (KillMode=control-group) would terminate a
// plain child process mid-update because it lives in the service's cgroup.
// We therefore launch via `systemd-run`, which runs the command in its own
// transient unit outside our cgroup so the restart can't reach it. OpenRC,
// launchd and bare hosts have no such cgroup semantics, so a `setsid` detach
// is sufficient.
func spawnUpdater(u *Updater, mode Mode, targetVersion string) error {
	shellCmd := buildUnixUpdateCmd(u, targetVersion)

	if mode == ModeSystemd && hasExec("systemd-run") {
		// `--collect` (systemd ≥ 236) garbage-collects the transient unit even
		// when it fails. Older systemd (CentOS/RHEL 7 = 219, Ubuntu 16.04 = 229 …)
		// rejects the flag with "unrecognized option '--collect'"; add it only
		// when supported.
		collect := systemdRunSupportsCollect()
		run := func(unit string) ([]byte, error) {
			args := make([]string, 0, 6)
			if collect {
				args = append(args, "--collect")
			}
			args = append(args, "--unit", unit, "/bin/sh", "-c", shellCmd)
			return exec.Command("systemd-run", args...).CombinedOutput()
		}

		// A previous self-update may have left the fixed-name transient unit
		// behind — still running/stuck, or failed — in which case systemd-run
		// aborts with "Unit kwrtmgrd-selfupdate.service already exists" (--collect
		// only reaps a unit AFTER it exits, never frees one that already exists
		// at create time). Free the name first: `stop` kills a running/stuck one,
		// `reset-failed` clears a failed one. `stop` is bounded so a process that
		// ignores SIGTERM can't hang the update request.
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = exec.CommandContext(stopCtx, "systemctl", "stop", "kwrtmgrd-selfupdate.service").Run()
		cancel()
		_ = exec.Command("systemctl", "reset-failed", "kwrtmgrd-selfupdate.service").Run()

		out, err := run("kwrtmgrd-selfupdate")
		if err != nil && strings.Contains(string(out), "already exists") {
			// Cleanup didn't free the name (e.g. a process ignoring SIGTERM):
			// fall back to a unique unit name that can never collide.
			out, err = run(fmt.Sprintf("kwrtmgrd-selfupdate-%d", time.Now().UnixNano()))
		}
		if err != nil {
			return fmt.Errorf("systemd-run failed: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	cmd := exec.Command("/bin/sh", "-c", shellCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if f, err := os.OpenFile(u.logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
		defer f.Close()
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn updater failed: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

// systemdRunSupportsCollect reports whether the local systemd-run understands
// the --collect flag (added in systemd v236). It probes `systemd-run --help`
// once at update time, which exits 0 and lists every supported option.
func systemdRunSupportsCollect() bool {
	out, err := exec.Command("systemd-run", "--help").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "--collect")
}

func buildUnixUpdateCmd(u *Updater, targetVersion string) string {
	args := "--update --force"
	if v := strings.TrimSpace(targetVersion); v != "" {
		args += " -v " + shellQuote(v)
	}
	url := shellQuote(u.cfg.InstallShURL)
	log := shellQuote(u.logPath())
	// `sleep 2` lets the HTTP 202 response flush before we tear ourselves
	// down; fetch install.sh via curl (falling back to wget) and pipe it into
	// `sh --update`, which swaps the binary and restarts the service.
	return fmt.Sprintf(
		`sleep 2; { if command -v curl >/dev/null 2>&1; then curl -fsSL %s; else wget -qO- %s; fi; } | sh -s -- %s >> %s 2>&1`,
		url, url, args, log,
	)
}

// shellQuote single-quotes a string for safe interpolation into /bin/sh -c.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
