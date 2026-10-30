package main

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

type HistoryManager struct {
	mu       sync.RWMutex
	filePath string
	records  []TransferRecord
}

func NewHistoryManager() *HistoryManager {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".ghosttunnel")
	_ = os.MkdirAll(dir, 0755)
	historyPath := filepath.Join(dir, "history.json")

	hm := &HistoryManager{
		filePath: historyPath,
		records:  make([]TransferRecord, 0),
	}
	hm.load()
	return hm
}

func (hm *HistoryManager) load() {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	data, err := os.ReadFile(hm.filePath)
	if err != nil {
		return
	}

	var recs []TransferRecord
	if err := json.Unmarshal(data, &recs); err == nil {
		hm.records = recs
	}
}

func (hm *HistoryManager) saveLocked() {
	data, err := json.MarshalIndent(hm.records, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(hm.filePath, data, 0644)
}

func (hm *HistoryManager) Add(record TransferRecord) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	// 限制最多保留 200 条最新历史
	hm.records = append([]TransferRecord{record}, hm.records...)
	if len(hm.records) > 200 {
		hm.records = hm.records[:200]
	}
	hm.saveLocked()
}

func (hm *HistoryManager) GetAll() []TransferRecord {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	result := make([]TransferRecord, len(hm.records))
	copy(result, hm.records)
	return result
}

func (hm *HistoryManager) Clear() error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.records = make([]TransferRecord, 0)
	hm.saveLocked()
	return nil
}

// 在操作系统文件管理器中高亮定位文件
func (hm *HistoryManager) OpenInFileManager(targetPath string) error {
	if targetPath == "" {
		return nil
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// macOS 使用 open -R 可以在 Finder 中选中该文件
		cmd = exec.Command("open", "-R", targetPath)
	case "windows":
		cmd = exec.Command("explorer", "/select,", targetPath)
	default: // linux
		cmd = exec.Command("xdg-open", filepath.Dir(targetPath))
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[History] 无法在文件管理器中打开: %v", err)
		return err
	}
	return nil
}
