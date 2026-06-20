package speedtest

// 测速历史：每次任务完成落旁车 JSON（最近 maxHistory 次），供前端历史表 + 趋势图。

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const maxHistory = 50

// HistoryEntry 是一次任务的历史快照（含汇总与每节点明细）。
type HistoryEntry struct {
	Time             string       `json:"time"`
	BestNode         string       `json:"best_node"`
	BestDownloadMbps float64      `json:"best_download_mbps"`
	BestUploadMbps   float64      `json:"best_upload_mbps"`
	MinPingMs        float64      `json:"min_ping_ms"`
	Nodes            []NodeResult `json:"nodes"`
}

// History 读历史（最新在前）。文件缺失返回空。
func (s *Service) History() []HistoryEntry {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	return s.loadHistory()
}

func (s *Service) loadHistory() []HistoryEntry {
	b, err := os.ReadFile(s.histPath)
	if err != nil {
		return []HistoryEntry{}
	}
	var list []HistoryEntry
	if json.Unmarshal(b, &list) != nil {
		return []HistoryEntry{}
	}
	return list
}

// ClearHistory 清空历史。
func (s *Service) ClearHistory() error {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	if err := os.Remove(s.histPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// appendHistory 由 finish() 调用：从本次节点汇总出一条历史并前插。无成功节点则不记。
func (s *Service) appendHistory(nodes []NodeResult) {
	e := HistoryEntry{Time: s.now(), Nodes: nodes}
	hasDone := false
	for _, n := range nodes {
		if n.Status != "done" {
			continue
		}
		hasDone = true
		if n.DownloadMbps > e.BestDownloadMbps {
			e.BestDownloadMbps = n.DownloadMbps
			e.BestNode = nodeLabel(n)
		}
		if n.UploadMbps > e.BestUploadMbps {
			e.BestUploadMbps = n.UploadMbps
		}
		if n.PingMs > 0 && (e.MinPingMs == 0 || n.PingMs < e.MinPingMs) {
			e.MinPingMs = n.PingMs
		}
	}
	if !hasDone {
		return
	}
	s.histMu.Lock()
	defer s.histMu.Unlock()
	list := append([]HistoryEntry{e}, s.loadHistory()...)
	if len(list) > maxHistory {
		list = list[:maxHistory]
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.histPath), 0o755)
	_ = os.WriteFile(s.histPath, b, 0o644)
}
