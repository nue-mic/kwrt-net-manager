package appcfg

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLevelSplitHandler(t *testing.T) {
	var lo, hi bytes.Buffer
	logger := slog.New(NewLevelSplitHandler(&lo, &hi, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")

	loS, hiS := lo.String(), hi.String()
	// INFO/DEBUG → stdout 侧（procd 标 daemon.info）。
	if !strings.Contains(loS, "msg=d") || !strings.Contains(loS, "msg=i") {
		t.Errorf("debug/info 应进 stdout：%q", loS)
	}
	if strings.Contains(loS, "msg=w") || strings.Contains(loS, "msg=e") {
		t.Errorf("warn/error 不应进 stdout：%q", loS)
	}
	// WARN/ERROR → stderr 侧（procd 标 daemon.err）。
	if !strings.Contains(hiS, "msg=w") || !strings.Contains(hiS, "msg=e") {
		t.Errorf("warn/error 应进 stderr：%q", hiS)
	}
	if strings.Contains(hiS, "msg=d") || strings.Contains(hiS, "msg=i") {
		t.Errorf("debug/info 不应进 stderr：%q", hiS)
	}
}

func TestLevelSplitWithAttrs(t *testing.T) {
	// WithAttrs（logger.With）后仍按级别分流，且属性保留。
	var lo, hi bytes.Buffer
	logger := slog.New(NewLevelSplitHandler(&lo, &hi, nil)).With("k", "v")
	logger.Info("i")
	logger.Error("e")
	if !strings.Contains(lo.String(), "k=v") || !strings.Contains(lo.String(), "msg=i") {
		t.Errorf("info+attr 应进 stdout：%q", lo.String())
	}
	if !strings.Contains(hi.String(), "k=v") || !strings.Contains(hi.String(), "msg=e") {
		t.Errorf("error+attr 应进 stderr：%q", hi.String())
	}
}

func TestLevelSplitRespectsLevel(t *testing.T) {
	// LevelVar 运行时调级对两侧同时生效（共享 opts）。
	var lo, hi bytes.Buffer
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelWarn) // 只放行 Warn 及以上
	logger := slog.New(NewLevelSplitHandler(&lo, &hi, &slog.HandlerOptions{Level: lv}))
	logger.Info("i")  // 应被过滤
	logger.Error("e") // 应输出到 stderr
	if lo.Len() != 0 {
		t.Errorf("Warn 级别下 Info 不应输出：%q", lo.String())
	}
	if !strings.Contains(hi.String(), "msg=e") {
		t.Errorf("Error 应输出到 stderr：%q", hi.String())
	}
}
