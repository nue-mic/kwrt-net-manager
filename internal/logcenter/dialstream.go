package logcenter

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// dialRingCap 是拨号实时流的专属环形缓冲容量（供新连接「回放最近」，不污染主事件总线 Ring）。
const dialRingCap = 300

// dialStream 管理「拨号实时日志」：单例数据源（真机 `logread -f`；store 后端=脚本模拟）→
// 富化诊断 → 广播给所有订阅者。无论多少前端连接，**只有一个 logread -f**（busybox 铁律）。
type dialStream struct {
	c       *Center
	mu      sync.Mutex
	subs    map[int]chan Entry
	nextID  int
	ring    []Entry
	seq     uint64
	cancel  context.CancelFunc
	running bool
}

func newDialStream(c *Center) *dialStream {
	return &dialStream{c: c, subs: map[int]chan Entry{}, ring: make([]Entry, 0, dialRingCap)}
}

// SubscribeDial 注册一个订阅者，返回 (订阅id, 接收通道, 最近回放快照)。首个订阅者懒启动数据源。
func (c *Center) SubscribeDial() (int, <-chan Entry, []Entry) {
	return c.dial.subscribe()
}

// UnsubscribeDial 注销订阅者；无人订阅时停掉数据源（省 busybox 资源，避免僵尸 logread）。
func (c *Center) UnsubscribeDial(id int) {
	c.dial.unsubscribe(id)
}

func (ds *dialStream) subscribe() (int, <-chan Entry, []Entry) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	id := ds.nextID
	ds.nextID++
	ch := make(chan Entry, 128)
	ds.subs[id] = ch
	snap := make([]Entry, len(ds.ring))
	copy(snap, ds.ring)
	if !ds.running {
		ds.startLocked()
	}
	return id, ch, snap
}

func (ds *dialStream) unsubscribe(id int) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ch, ok := ds.subs[id]; ok {
		delete(ds.subs, id)
		close(ch)
	}
	if len(ds.subs) == 0 && ds.running {
		ds.stopLocked()
	}
}

// startLocked 启动数据源（调用方须持锁）。
func (ds *dialStream) startLocked() {
	ctx, cancel := context.WithCancel(context.Background())
	ds.cancel = cancel
	ds.running = true

	if ds.c.simulate {
		go ds.runSimulator(ctx)
		return
	}
	sr, ok := ds.c.run.(StreamRunner)
	if !ok {
		ds.c.log.Warn("拨号实时日志：Runner 不支持流式，已禁用（仅快照可用）")
		ds.running = false
		cancel()
		return
	}
	lines, err := sr.Stream(ctx, "logread", "-f")
	if err != nil {
		ds.c.log.Warn("拨号实时日志：启动 logread -f 失败", slog.Any("err", err))
		ds.running = false
		cancel()
		return
	}
	go ds.runReal(ctx, lines)
}

func (ds *dialStream) stopLocked() {
	if ds.cancel != nil {
		ds.cancel()
		ds.cancel = nil
	}
	ds.running = false
}

// runReal 消费 logread -f 的每一行：解析 syslog → 仅留拨号相关行 → 富化诊断 → 广播。
func (ds *dialStream) runReal(ctx context.Context, lines <-chan string) {
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				return
			}
			e, valid := parseSyslogLine(line, ds.c.loc)
			if !valid || !isDialupLine(e) {
				continue
			}
			enrichDial(&e)
			ds.broadcast(e)
		}
	}
}

// broadcast 赋序号、写环形缓冲、非阻塞扇出给所有订阅者（某订阅者满则丢其一帧，背压保护）。
func (ds *dialStream) broadcast(e Entry) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.seq++
	e.Seq = ds.seq
	ds.ring = append(ds.ring, e)
	if len(ds.ring) > dialRingCap {
		ds.ring = ds.ring[len(ds.ring)-dialRingCap:]
	}
	for _, ch := range ds.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// runSimulator 在 store 后端(非 OpenWrt)推送脚本化拨号序列：先演示一轮失败(PADO 超时)，
// 再演示一轮成功(认证通过→拿到 IP)，循环往复，复用同一 enrichDial，保证 Windows/CI 看到的
// 字段结构、诊断横幅与真机完全一致。
func (ds *dialStream) runSimulator(ctx context.Context) {
	type frame struct {
		level, proc, msg string
		gap              time.Duration
	}
	script := []frame{
		// 第一轮：失败（与真机这张「无 PPPoE 服务器」的网一致）
		{"notice", "pppd", "pppd 2.5.1 started by root, uid 0", 600 * time.Millisecond},
		{"info", "pppd", "Send PPPOE Discovery V1T1 PADI packet", 800 * time.Millisecond},
		{"warn", "pppd", "Timeout waiting for PADO packets", 600 * time.Millisecond},
		{"err", "pppd", "Unable to complete PPPoE Discovery phase 1", 500 * time.Millisecond},
		{"info", "pppd", "Exit.", 3000 * time.Millisecond},
		// 第二轮：成功（认证通过→拿到 IP）
		{"notice", "pppd", "pppd 2.5.1 started by root, uid 0", 600 * time.Millisecond},
		{"info", "pppd", "Send PPPOE Discovery V1T1 PADI packet", 600 * time.Millisecond},
		{"info", "pppd", "PADS: Service-Name: ''", 500 * time.Millisecond},
		{"notice", "pppd", "Connect: ppp0 <--> eth1", 500 * time.Millisecond},
		{"info", "pppd", "CHAP authentication succeeded", 600 * time.Millisecond},
		{"notice", "pppd", "local  IP address 100.64.12.34", 400 * time.Millisecond},
		{"notice", "pppd", "remote IP address 100.64.0.1", 400 * time.Millisecond},
		{"notice", "pppd", "primary   DNS address 119.29.29.29", 4000 * time.Millisecond},
	}
	i := 0
	for {
		f := script[i%len(script)]
		now := time.Now()
		e := Entry{
			Time:  now.In(ds.c.loc).Format("2006-01-02 15:04:05"),
			TS:    now.Unix(),
			Level: f.level,
			Proc:  f.proc,
			Iface:   "pppoe-wan",
			Message: f.msg,
		}
		enrichDial(&e)
		ds.broadcast(e)
		i++
		select {
		case <-ctx.Done():
			return
		case <-time.After(f.gap):
		}
	}
}
