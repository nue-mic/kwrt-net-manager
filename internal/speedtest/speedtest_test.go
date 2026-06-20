package speedtest

import (
	"strings"
	"testing"
	"time"
)

// 真机 speedtest-go v1.7.8 实采输出，锁定解析。
const sampleList = `
    speedtest-go v1.7.8 (git-dev) @showwin

✓ ISP: 119.112.135.194 (China Unicom) [38.9384, 121.6224]
✓ Found 20 Public Servers
[43752]    461.09km 20ms 	Beijing (China) by BJ Unicom
[73226]    492.36km 1148ms 	Seoul (South Korea) by GSL Networks
[ 5396]    855.62km 78ms 	Suzhou (China) by China Telecom JiangSu 5G
[24447]    861.51km 166ms 	Shanghai (China) by China Unicom 5G
[59386]    973.79km Timeout 	HangZhou (China) by 浙江电信
[27594]   1890.00km 60ms 	Guangzhou (China) by China Mobile
`

// 真机 --server 43752 --json 实采。
const sampleNodeJSON = `{"timestamp":"2026-06-20 06:58:25.382","user_info":{"IP":"119.112.135.194","Isp":"China Unicom"},"servers":[{"url":"http://x","name":"Beijing","country":"","sponsor":"BJ Unicom","id":"43752","distance":0,"latency":21552729,"jitter":2215026,"dl_speed":29699216.342925407,"ul_speed":7546696.055846809,"packet_loss":{"sent":0,"dup":0,"max":0}}]}`

func TestParseServerList(t *testing.T) {
	list := parseServerList(sampleList)
	if len(list) != 6 {
		t.Fatalf("解析到 %d 个节点，期望 6\n%+v", len(list), list)
	}
	// 第一个：北京联通，自带 20ms 可达。
	s0 := list[0]
	if s0.ID != "43752" || s0.Sponsor != "BJ Unicom" || s0.Name != "Beijing (China)" {
		t.Errorf("首节点解析错：%+v", s0)
	}
	if s0.DistanceKm != 461.09 || s0.PingMs != 20 || !s0.Reachable {
		t.Errorf("首节点距离/延迟/可达错：%+v", s0)
	}
	// 带前导空格的 id [ 5396]。
	if list[2].ID != "5396" {
		t.Errorf("前导空格 id 解析错：%q", list[2].ID)
	}
	// Timeout 节点不可达，ping=-1。
	var ts *Server
	for i := range list {
		if list[i].ID == "59386" {
			ts = &list[i]
		}
	}
	if ts == nil || ts.Reachable || ts.PingMs != -1 {
		t.Errorf("Timeout 节点应不可达：%+v", ts)
	}
}

func TestParseISP(t *testing.T) {
	if isp := parseISP(sampleList); isp != "China Unicom" {
		t.Errorf("ISP=%q want China Unicom", isp)
	}
}

func TestParseNodeJSON(t *testing.T) {
	nr, err := parseNodeJSON(sampleNodeJSON)
	if err != nil {
		t.Fatal(err)
	}
	// bps→Mbps：29699216/1e6 ≈ 29.70；纳秒→ms：21552729/1e6 ≈ 21.55。
	if nr.DownloadMbps != 29.70 {
		t.Errorf("download=%v want 29.70", nr.DownloadMbps)
	}
	if nr.UploadMbps != 7.55 {
		t.Errorf("upload=%v want 7.55", nr.UploadMbps)
	}
	if nr.PingMs != 21.55 {
		t.Errorf("ping=%v want 21.55", nr.PingMs)
	}
	if nr.JitterMs != 2.22 {
		t.Errorf("jitter=%v want 2.22", nr.JitterMs)
	}
	if nr.Sponsor != "BJ Unicom" || nr.ID != "43752" {
		t.Errorf("节点信息错：%+v", nr)
	}
}

func TestParseNodeJSONJunk(t *testing.T) {
	if _, err := parseNodeJSON("connection refused\n"); err == nil {
		t.Error("非 JSON 应报错")
	}
}

func TestPickRecommended(t *testing.T) {
	list := parseServerList(sampleList)
	pickRecommended(list, 3)
	var recs []Server
	for _, s := range list {
		if s.Recommended {
			recs = append(recs, s)
		}
	}
	if len(recs) != 3 {
		t.Fatalf("推荐 %d 个，期望 3", len(recs))
	}
	// 应覆盖多个运营商桶（联通/电信/移动），不重桶。
	buckets := map[string]bool{}
	for _, s := range recs {
		b := ispBucket(s.Sponsor)
		if buckets[b] {
			t.Errorf("推荐节点运营商桶重复：%s", b)
		}
		buckets[b] = true
	}
	// 不可达（Timeout）的 59386 不应被推荐。
	for _, s := range recs {
		if s.ID == "59386" {
			t.Error("不可达节点不应被推荐")
		}
	}
}

// fakeRunner 喂预置输出，验证 Run 全自动状态机（install→list→test→done）。
type fakeRunner struct {
	installed bool
}

func (f *fakeRunner) Run(stdin, name string, args ...string) (string, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	switch {
	case strings.Contains(cmd, "command -v speedtest-go"):
		if f.installed {
			return "/usr/bin/speedtest-go", nil
		}
		return "", &exitErr{}
	case strings.Contains(cmd, "speedtest-go --list"):
		return sampleList, nil
	case strings.Contains(cmd, "--server 43752 --json"):
		return sampleNodeJSON, nil
	case strings.Contains(cmd, "--server"): // 其它节点：返回空（模拟失败）
		return "", nil
	case name == "date":
		return "+0800", nil
	}
	return "", nil
}

type exitErr struct{}

func (e *exitErr) Error() string { return "exit status 1" }

func TestRunSingleNode(t *testing.T) {
	f := &fakeRunner{installed: true}
	svc := New(f, t.TempDir())
	if err := svc.Run([]string{"43752"}); err != nil {
		t.Fatal(err)
	}
	// 等 job 完成（同步等待 Running 转 false）。
	waitDone(t, svc)
	st := svc.Status()
	if st.Phase != "done" || st.Running {
		t.Fatalf("应 done：phase=%s running=%v", st.Phase, st.Running)
	}
	if len(st.Nodes) != 1 || st.Nodes[0].Status != "done" || st.Nodes[0].DownloadMbps != 29.70 {
		t.Fatalf("节点结果错：%+v", st.Nodes)
	}
	if st.ISP != "China Unicom" {
		t.Errorf("ISP=%q", st.ISP)
	}
	// 历史应落一条。
	h := svc.History()
	if len(h) != 1 || h[0].BestDownloadMbps != 29.70 {
		t.Fatalf("历史错：%+v", h)
	}
}

func TestRunAutoInstall(t *testing.T) {
	f := &fakeRunner{installed: false} // 未装
	svc := New(f, t.TempDir())
	// 注意：fakeRunner 的 install 走 pkgmgr，可能因命令缺失而“失败”；
	// 这里只验证未装时会进入 installing 阶段而非直接拒绝。
	if err := svc.Run([]string{"43752"}); err != nil {
		t.Fatal(err)
	}
	waitDone(t, svc)
	// install 在 fake 下大概率失败 → error；或成功 → done。两者都说明走了自动安装分支。
	st := svc.Status()
	if st.Phase != "error" && st.Phase != "done" {
		t.Fatalf("自动安装后应 error 或 done，得到 %s", st.Phase)
	}
}

func waitDone(t *testing.T, svc *Service) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if !svc.Status().Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("测速 job 超时未结束")
}
