package speedtest

import "testing"

// 真机 speedtest-go v1.7.8 实测输出，锁定解析正则。
const sampleOut = `
    speedtest-go v1.7.8 (git-dev) @showwin

✓ ISP: 119.112.135.194 (China Unicom) [38.9384, 121.6224]
✓ Found 20 Public Servers

✓ Test Server: [43752] 461.09km Beijing (China) by BJ Unicom
✓ Latency: 20.793609ms Jitter: 189.306µs Min: 20.490188ms Max: 21.105573ms
✓ Download: 542.09 Mbps (Used: 656.88MB) (Latency: 17ms Jitter: 0ms Min: 16ms Max: 18ms)
✓ Upload: 54.74 Mbps (Used: 85.02MB) (Latency: 68ms Jitter: 69ms Min: 33ms Max: 274ms)
✓ Packet Loss: 1.77% (Sent: 277/Dup: 0/Max: 281)
`

func TestParseSpeedtest(t *testing.T) {
	r := parseSpeedtest(sampleOut)
	if r.DownloadMbps != 542.09 {
		t.Errorf("download=%v want 542.09", r.DownloadMbps)
	}
	if r.UploadMbps != 54.74 {
		t.Errorf("upload=%v want 54.74", r.UploadMbps)
	}
	if r.PingMs != 20.793609 {
		t.Errorf("ping=%v want 20.793609", r.PingMs)
	}
	if r.ISP != "China Unicom" {
		t.Errorf("isp=%q want China Unicom", r.ISP)
	}
	if r.Server != "[43752] 461.09km Beijing (China) by BJ Unicom" {
		t.Errorf("server=%q", r.Server)
	}
}

func TestParseSpeedtestEmpty(t *testing.T) {
	r := parseSpeedtest("connection refused\n")
	if r.DownloadMbps != 0 || r.UploadMbps != 0 {
		t.Errorf("expected zero result on junk, got %+v", r)
	}
}
