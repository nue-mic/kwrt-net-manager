package ddns

// device 条目的运行面：解析到的 GUA 写进缓存文件，生成给 ddns-scripts 用的 ip_script，
// 后台 poller 定时刷新。IPv6 主线尚无 lease ubus 事件，故用轮询（见调研结论）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultDeviceDir = "/var/run/kwrtmgrd-ddns"

func (s *Service) deviceDir() string {
	if s.scriptDir == "" {
		return defaultDeviceDir
	}
	return s.scriptDir
}

func (s *Service) deviceIPPath(id string) string     { return filepath.Join(s.deviceDir(), id+".ip") }
func (s *Service) deviceScriptPath(id string) string { return filepath.Join(s.deviceDir(), id+".sh") }

// ensureDeviceScript 生成/更新该条目的 ip_script：一个只把缓存 IP 文件 cat 出来的可执行脚本。
// 智能解析在 Go 侧（refreshDevice），shell 只负责 cat，保证 ddns-scripts ip_source='script' 可读。
func (s *Service) ensureDeviceScript(id string) error {
	if err := os.MkdirAll(s.deviceDir(), 0o755); err != nil {
		return err
	}
	content := "#!/bin/sh\n" +
		"# 由 kwrt-net-manager 生成：输出目标 LAN 终端当前稳定 IPv6(GUA)，供 ddns-scripts ip_source='script' 读取。\n" +
		"cat \"" + s.deviceIPPath(id) + "\" 2>/dev/null\n"
	return os.WriteFile(s.deviceScriptPath(id), []byte(content), 0o755)
}

// refreshDevice 解析一条 device 条目的当前 GUA 并写入缓存文件。
// 解析失败或未发现地址时，**不覆盖**已有缓存（设备临时离线不应清空记录，避免 DDNS 抖动）。
// 返回 (ip, changed, err)。
func (s *Service) refreshDevice(e Entry) (string, bool, error) {
	ip, _, err := resolveDeviceGUA(s.run, e.MAC)
	if err != nil || ip == "" {
		return "", false, err
	}
	p := s.deviceIPPath(e.ID)
	if old, _ := os.ReadFile(p); strings.TrimSpace(string(old)) == ip {
		return ip, false, nil
	}
	if err := os.MkdirAll(s.deviceDir(), 0o755); err != nil {
		return ip, false, err
	}
	if err := os.WriteFile(p, []byte(ip+"\n"), 0o644); err != nil {
		return ip, false, err
	}
	return ip, true, nil
}

// RefreshDevices 刷新全部已启用的 device 条目（poller 与保存时各调一次）。
func (s *Service) RefreshDevices() {
	s.mu.Lock()
	items := s.load()
	s.mu.Unlock()
	for _, e := range items {
		if e.Enabled && e.IPSource == "device" {
			_ = s.ensureDeviceScript(e.ID)
			_, _, _ = s.refreshDevice(e)
		}
	}
}

// StartDevicePoller 起后台轮询：启动即刷一次，之后每 interval 刷一次，直到 ctx 取消。
func (s *Service) StartDevicePoller(ctx context.Context, interval time.Duration) {
	go func() {
		s.RefreshDevices()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.RefreshDevices()
			}
		}
	}()
}

// ListDevices 返回当前可见 LAN 终端（供前端「选择目标设备」）。
func (s *Service) ListDevices() []Device {
	return listDevices(s.run, "/tmp/dhcp.leases")
}
