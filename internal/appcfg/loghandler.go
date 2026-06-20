package appcfg

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// LevelSplitHandler 按日志级别把记录分流到两个 writer：< Warn 写 lo、>= Warn 写 hi。
//
// 目的（OpenWrt / procd）：procd 把进程 stdout 标 syslog 的 daemon.info、stderr 标 daemon.err
// （按「来自哪个流」定 severity，不看内容）。让 INFO/DEBUG 走 stdout、WARN/ERROR 走 stderr，
// logread 里正常日志就是 daemon.info、真正的告警/错误才是 daemon.err——修掉「level=INFO /
// status=200 的正常访问日志被标成 daemon.err」的误导。两个子 handler 共享同一 opts（含运行时
// 可变的 LevelVar），故级别调整对两侧同时生效。
type LevelSplitHandler struct {
	lo slog.Handler
	hi slog.Handler
}

// NewLogHandler 构造默认分流处理器：INFO/DEBUG→stdout、WARN/ERROR→stderr。
func NewLogHandler(opts *slog.HandlerOptions) *LevelSplitHandler {
	return NewLevelSplitHandler(os.Stdout, os.Stderr, opts)
}

// NewLevelSplitHandler 用指定 writer 构造（便于单测）。
func NewLevelSplitHandler(lo, hi io.Writer, opts *slog.HandlerOptions) *LevelSplitHandler {
	return &LevelSplitHandler{
		lo: slog.NewTextHandler(lo, opts),
		hi: slog.NewTextHandler(hi, opts),
	}
}

func (h *LevelSplitHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.lo.Enabled(ctx, l)
}

func (h *LevelSplitHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		return h.hi.Handle(ctx, r)
	}
	return h.lo.Handle(ctx, r)
}

func (h *LevelSplitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LevelSplitHandler{lo: h.lo.WithAttrs(attrs), hi: h.hi.WithAttrs(attrs)}
}

func (h *LevelSplitHandler) WithGroup(name string) slog.Handler {
	return &LevelSplitHandler{lo: h.lo.WithGroup(name), hi: h.hi.WithGroup(name)}
}
