package logcenter

import (
	"context"
	"testing"
	"time"
)

// fakeStreamRunner 实现 Runner + StreamRunner，记录被调用的命令并按预置行投递。
type fakeStreamRunner struct {
	fakeRunner
	gotName string
	gotArgs []string
	lines   []string
}

func (f *fakeStreamRunner) Stream(ctx context.Context, name string, args ...string) (<-chan string, error) {
	f.gotName, f.gotArgs = name, args
	ch := make(chan string)
	go func() {
		defer close(ch)
		for _, ln := range f.lines {
			select {
			case ch <- ln:
			case <-ctx.Done():
				return
			}
		}
		<-ctx.Done() // 保持「长驻」直到取消，模拟 logread -f
	}()
	return ch, nil
}

func recvTimeout(t *testing.T, ch <-chan Entry) Entry {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("超时未收到拨号日志帧")
		return Entry{}
	}
}

// 真机路径：注入 fake StreamRunner，断言命令为 `logread -f`，且订阅者收到富化后的拨号帧。
func TestDialStreamReal(t *testing.T) {
	fr := &fakeStreamRunner{
		fakeRunner: fakeRunner{out: map[string]string{"date": "+0800"}},
		lines: []string{
			"Tue Jun 16 22:05:20 2026 daemon.notice pppd[555]: pppd 2.5.1 started by root, uid 0",
			"Tue Jun 16 22:05:21 2026 daemon.warn pppd[555]: Timeout waiting for PADO packets",
			"Tue Jun 16 22:05:21 2026 daemon.info dnsmasq[1]: query foo", // 非拨号行，应被过滤
			"Tue Jun 16 22:05:22 2026 daemon.err pppd[555]: Unable to complete PPPoE Discovery phase 1",
		},
	}
	c := New(t.TempDir(), nil)
	c.run = fr
	c.loc = time.FixedZone("local", 8*3600)

	id, ch, snap := c.SubscribeDial()
	defer c.UnsubscribeDial(id)
	if len(snap) != 0 {
		t.Fatalf("首个订阅者回放快照应为空，得 %d", len(snap))
	}

	e1 := recvTimeout(t, ch)
	if e1.Proc != "pppd" || e1.DialState != dialConnecting || e1.Seq != 1 {
		t.Errorf("帧1 = %+v，期望 pppd/connecting/seq1", e1)
	}
	e2 := recvTimeout(t, ch)
	if e2.Severity != "error" || e2.Phase != "discovery" || e2.Diagnosis == "" {
		t.Errorf("帧2(PADO 超时) = %+v，期望 error/discovery/有诊断", e2)
	}
	e3 := recvTimeout(t, ch) // dnsmasq 行被过滤，下一帧应是 Discovery 失败
	if e3.Severity != "error" || e3.Seq != 3 {
		t.Errorf("帧3 = %+v，期望 error/seq3（dnsmasq 行已过滤）", e3)
	}

	if fr.gotName != "logread" || len(fr.gotArgs) != 1 || fr.gotArgs[0] != "-f" {
		t.Errorf("数据源命令 = %q %v，期望 logread -f", fr.gotName, fr.gotArgs)
	}
}

// store/非 OpenWrt：Runner 不支持流式 → 不调 logread，不报错（仅无实时帧，靠模拟器或快照）。
func TestDialStreamNoStreamRunner(t *testing.T) {
	c := New(t.TempDir(), nil)
	c.run = fakeRunner{out: map[string]string{"date": "+0800"}} // 仅 Runner，无 Stream
	id, ch, _ := c.SubscribeDial()
	defer c.UnsubscribeDial(id)
	select {
	case <-ch:
		t.Fatal("无流式能力却收到帧")
	case <-time.After(200 * time.Millisecond):
		// 期望：静默无帧
	}
}

// store 模拟器：simulate=true 时不依赖 logread，按脚本推送富化拨号序列。
func TestDialStreamSimulator(t *testing.T) {
	c := New(t.TempDir(), nil)
	c.run = fakeRunner{out: map[string]string{"date": "+0800"}}
	c.loc = time.UTC
	c.SetSimulate(true)

	id, ch, _ := c.SubscribeDial()
	defer c.UnsubscribeDial(id)

	first := recvTimeout(t, ch)
	if first.Proc != "pppd" || first.Iface != "pppoe-wan" || first.DialState == "" {
		t.Errorf("模拟首帧 = %+v，期望 pppd/pppoe-wan/有状态", first)
	}
	// 序列里应能收到一条带诊断的失败帧（第一轮 PADO 超时）。
	gotDiag := first.Diagnosis != ""
	for i := 0; i < 4 && !gotDiag; i++ {
		if e := recvTimeout(t, ch); e.Diagnosis != "" {
			gotDiag = true
		}
	}
	if !gotDiag {
		t.Error("模拟序列前几帧未出现任何诊断文案")
	}
}
