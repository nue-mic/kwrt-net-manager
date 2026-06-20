package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/mia-clark/kwrt-net-manager/internal/api/middleware"
	"github.com/mia-clark/kwrt-net-manager/internal/logcenter"
)

// DialStreamHandler 暴露「拨号实时日志」WebSocket 与「拨号诊断结论」端点。
// 传输层复用 events.go 同款升级/CORS/ping，鉴权由所在子树的 Bearer(?token=) 中间件负责。
type DialStreamHandler struct {
	c       *logcenter.Center
	log     *slog.Logger
	origins func() []string
}

// NewDialStreamHandler 装配。originsFn 每连接读取一次，使运行期 CORS 变更对新连接生效。
func NewDialStreamHandler(c *logcenter.Center, log *slog.Logger, originsFn func() []string) *DialStreamHandler {
	return &DialStreamHandler{c: c, log: log, origins: originsFn}
}

// dialFrame 是推给前端的一帧：信封(seq/type/ts) + 富化后的日志 Entry。
type dialFrame struct {
	Seq  uint64          `json:"seq"`
	Type string          `json:"type"`
	TS   string          `json:"ts"`
	Data logcenter.Entry `json:"data"`
}

// Stream 处理 GET(WS) /api/v1/logs/dialup/stream?token=&iface=&replay=N。
func (h *DialStreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	origins := h.origins()
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: middleware.IsWildcard(origins),
		OriginPatterns:     origins,
	})
	if err != nil {
		h.log.Warn("dial ws accept failed", slog.Any("err", err))
		return
	}
	defer conn.Close(websocket.StatusInternalError, "internal error")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	iface := strings.TrimSpace(r.URL.Query().Get("iface"))
	replay := parseReplay(r)

	id, ch, snap := h.c.SubscribeDial()
	defer h.c.UnsubscribeDial(id)

	// 回放最近 N 条（连上即有上下文，类比 events 的 ?since）。
	if replay > 0 && len(snap) > 0 {
		from := 0
		if len(snap) > replay {
			from = len(snap) - replay
		}
		for _, e := range snap[from:] {
			if dialIfaceMatch(e, iface) && !writeDial(ctx, conn, e) {
				return
			}
		}
	}

	// 读循环：客户端无需发消息，仅用于检测关闭。
	go drainReads(ctx, conn, cancel)

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if dialIfaceMatch(e, iface) && !writeDial(ctx, conn, e) {
				return
			}
		case <-ping.C:
			pctx, c := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pctx)
			c()
			if err != nil {
				return
			}
		}
	}
}

func writeDial(ctx context.Context, conn *websocket.Conn, e logcenter.Entry) bool {
	b, err := json.Marshal(dialFrame{Seq: e.Seq, Type: "dial.log", TS: e.Time, Data: e})
	if err != nil {
		return true
	}
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b) == nil
}

// dialIfaceMatch：未指定过滤=全收；行本身无接口名(如纯 pppd 发现行)=不隐藏；否则子串匹配。
func dialIfaceMatch(e logcenter.Entry, iface string) bool {
	if iface == "" || e.Iface == "" {
		return true
	}
	return strings.Contains(e.Iface, iface)
}

func drainReads(ctx context.Context, conn *websocket.Conn, cancel context.CancelFunc) {
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			cancel()
			return
		}
	}
}

// parseReplay 解析 ?replay（连上回放最近 N 条），默认 50，封顶 dialRing 容量级别。
func parseReplay(r *http.Request) int {
	v := r.URL.Query().Get("replay")
	if v == "" {
		return 50
	}
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return 50
		}
		n = n*10 + int(c-'0')
		if n > 300 {
			return 300
		}
	}
	return n
}

// Diagnose 处理 GET /api/v1/logs/dialup/diagnose?iface= —— 返回当前拨号结论横幅。
func (h *DialStreamHandler) Diagnose(w http.ResponseWriter, r *http.Request) {
	iface := strings.TrimSpace(r.URL.Query().Get("iface"))
	WriteJSON(w, http.StatusOK, h.c.Diagnose(iface))
}
