package prefs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"etcdmonitor/internal/logger"
)

// PanelItem 单个面板的配置
type PanelItem struct {
	ID      string `json:"id"`
	Visible bool   `json:"visible"`
	Order   int    `json:"order"`
}

// PanelConfig 用户的面板配置
type PanelConfig struct {
	Panels  []PanelItem `json:"panels"`
	Version int         `json:"version"`
}

// DefaultPanels 默认面板列表（全选、原始顺序）
var DefaultPanels = []PanelItem{
	{ID: "chartRaftProposals", Visible: true, Order: 0},
	{ID: "chartLeaderChanges", Visible: true, Order: 1},
	{ID: "chartProposalLag", Visible: true, Order: 2},
	{ID: "chartProposalFailedRate", Visible: true, Order: 3},
	{ID: "chartWALFsync", Visible: true, Order: 4},
	{ID: "chartBackendCommit", Visible: true, Order: 5},
	{ID: "chartDBSize", Visible: true, Order: 6},
	{ID: "chartMVCCOps", Visible: true, Order: 7},
	{ID: "chartPeerTraffic", Visible: true, Order: 8},
	{ID: "chartPeerRTT", Visible: true, Order: 9},
	{ID: "chartGRPC", Visible: true, Order: 10},
	{ID: "chartGRPCTraffic", Visible: true, Order: 11},
	{ID: "chartCPU", Visible: true, Order: 12},
	{ID: "chartMemory", Visible: true, Order: 13},
	{ID: "chartGoroutines", Visible: true, Order: 14},
	{ID: "chartGC", Visible: true, Order: 15},
	{ID: "chartFDs", Visible: true, Order: 16},
	{ID: "chartMemSys", Visible: true, Order: 17},
}

// DefaultConfig 返回默认面板配置
func DefaultConfig() *PanelConfig {
	panels := make([]PanelItem, len(DefaultPanels))
	copy(panels, DefaultPanels)
	return &PanelConfig{
		Panels:  panels,
		Version: 1,
	}
}

// validPanelIDs 合法面板 ID 集合
var validPanelIDs map[string]bool

func init() {
	validPanelIDs = make(map[string]bool, len(DefaultPanels))
	for _, p := range DefaultPanels {
		validPanelIDs[p.ID] = true
	}
}

// FileStore 基于文件系统的用户偏好存储
type FileStore struct {
	baseDir string
	mu      sync.RWMutex // 保护并发文件访问
}

// NewFileStore 创建文件存储实例
func NewFileStore(baseDir string) *FileStore {
	return &FileStore{baseDir: baseDir}
}

// userFilePath 返回用户偏好文件路径
func (fs *FileStore) userFilePath(username string) string {
	// 安全处理用户名：只保留字母、数字、下划线、连字符、点号
	safe := sanitizeUsername(username)
	return filepath.Join(fs.baseDir, safe+".json")
}

// sanitizeUsername 清理用户名用于文件名
func sanitizeUsername(username string) string {
	var sb strings.Builder
	for _, c := range username {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
			sb.WriteRune(c)
		}
	}
	result := sb.String()
	if result == "" {
		result = "_anonymous"
	}
	return result
}

// Load 读取用户的面板配置，不存在则返回默认配置
func (fs *FileStore) Load(username string) (*PanelConfig, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	path := fs.userFilePath(username)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("read user prefs: %w", err)
	}

	var cfg PanelConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		logger.Warnf("[Prefs] Corrupt prefs file for user %s, returning default: %v", username, err)
		return DefaultConfig(), nil
	}

	// 校验并修复：过滤无效 ID，补充新面板
	cfg.Panels = mergeWithDefaults(cfg.Panels)

	return &cfg, nil
}

// Save 保存用户的面板配置
func (fs *FileStore) Save(username string, cfg *PanelConfig) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// 确保目录存在
	if err := os.MkdirAll(fs.baseDir, 0755); err != nil {
		return fmt.Errorf("create prefs dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prefs: %w", err)
	}

	path := fs.userFilePath(username)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write prefs: %w", err)
	}

	logger.Infof("[Prefs] Saved panel config for user: %s", username)
	return nil
}

// mergeWithDefaults 过滤无效面板 ID，并把缺失的新面板追加到末尾
func mergeWithDefaults(panels []PanelItem) []PanelItem {
	seen := make(map[string]bool, len(panels))
	result := make([]PanelItem, 0, len(DefaultPanels))

	// 保留用户已有的合法面板
	for _, p := range panels {
		if validPanelIDs[p.ID] && !seen[p.ID] {
			seen[p.ID] = true
			result = append(result, p)
		}
	}

	// 追加缺失的面板（默认可见，order 排在末尾）
	nextOrder := len(result)
	for _, dp := range DefaultPanels {
		if !seen[dp.ID] {
			result = append(result, PanelItem{
				ID:      dp.ID,
				Visible: true,
				Order:   nextOrder,
			})
			nextOrder++
		}
	}

	return result
}
