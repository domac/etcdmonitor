package prefs

import (
	"testing"
)

// ===== sanitizeUsername tests =====

func TestSanitizeUsername_Normal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"admin", "admin"},
		{"user-name", "user-name"},
		{"user_name", "user_name"},
		{"user.name", "user.name"},
		{"User123", "User123"},
	}
	for _, tt := range tests {
		got := sanitizeUsername(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeUsername(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeUsername_SpecialChars(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"admin@!#", "admin"},
		{"../etc/passwd", "..etcpasswd"},
		{"user name", "username"},
		{"root;rm -rf /", "rootrm-rf"},
	}
	for _, tt := range tests {
		got := sanitizeUsername(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeUsername(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeUsername_Empty(t *testing.T) {
	tests := []string{"", "!@#$%", "   "}
	for _, input := range tests {
		got := sanitizeUsername(input)
		if got != "_anonymous" {
			t.Errorf("sanitizeUsername(%q) = %q, want %q", input, got, "_anonymous")
		}
	}
}

// ===== mergeWithDefaults tests =====

func TestMergeWithDefaults_FullMatch(t *testing.T) {
	// 用户已有所有面板
	panels := make([]PanelItem, len(DefaultPanels))
	copy(panels, DefaultPanels)

	result := mergeWithDefaults(panels)
	if len(result) != len(DefaultPanels) {
		t.Errorf("mergeWithDefaults() returned %d panels, want %d", len(result), len(DefaultPanels))
	}
}

func TestMergeWithDefaults_MissingPanels(t *testing.T) {
	// 用户只有前 3 个面板
	panels := []PanelItem{
		{ID: "chartRaftProposals", Visible: true, Order: 0},
		{ID: "chartLeaderChanges", Visible: false, Order: 1},
		{ID: "chartProposalLag", Visible: true, Order: 2},
	}

	result := mergeWithDefaults(panels)
	if len(result) != len(DefaultPanels) {
		t.Fatalf("mergeWithDefaults() returned %d panels, want %d", len(result), len(DefaultPanels))
	}

	// 前 3 个保持用户设置
	if result[0].ID != "chartRaftProposals" || result[0].Visible != true {
		t.Errorf("panel[0] = %+v, want chartRaftProposals/true", result[0])
	}
	if result[1].ID != "chartLeaderChanges" || result[1].Visible != false {
		t.Errorf("panel[1] = %+v, want chartLeaderChanges/false", result[1])
	}

	// 新追加的面板使用默认可见性
	found := false
	for _, p := range result {
		if p.ID == "chartDBSize" {
			found = true
			if p.Visible != true {
				t.Errorf("chartDBSize default visible should be true")
			}
			break
		}
	}
	if !found {
		t.Error("chartDBSize should be added by mergeWithDefaults")
	}
}

func TestMergeWithDefaults_InvalidIDs(t *testing.T) {
	panels := []PanelItem{
		{ID: "chartRaftProposals", Visible: true, Order: 0},
		{ID: "nonexistent_panel", Visible: true, Order: 1},
		{ID: "another_fake", Visible: true, Order: 2},
	}

	result := mergeWithDefaults(panels)

	// 无效 ID 应被过滤，最终数量 = DefaultPanels
	if len(result) != len(DefaultPanels) {
		t.Errorf("mergeWithDefaults() returned %d panels, want %d", len(result), len(DefaultPanels))
	}

	// 验证无效 ID 不存在
	for _, p := range result {
		if p.ID == "nonexistent_panel" || p.ID == "another_fake" {
			t.Errorf("invalid panel ID %q should have been filtered", p.ID)
		}
	}
}

func TestMergeWithDefaults_DuplicateIDs(t *testing.T) {
	panels := []PanelItem{
		{ID: "chartRaftProposals", Visible: true, Order: 0},
		{ID: "chartRaftProposals", Visible: false, Order: 1},
	}

	result := mergeWithDefaults(panels)

	// 第一个出现的保留，重复的被去掉
	count := 0
	for _, p := range result {
		if p.ID == "chartRaftProposals" {
			count++
			if p.Visible != true {
				t.Error("first occurrence should be kept (visible=true)")
			}
		}
	}
	if count != 1 {
		t.Errorf("chartRaftProposals appeared %d times, want 1", count)
	}
}

// ===== DefaultConfig tests =====

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}
	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
	if len(cfg.Panels) != len(DefaultPanels) {
		t.Errorf("len(Panels) = %d, want %d", len(cfg.Panels), len(DefaultPanels))
	}

	// 确保是拷贝而非引用
	cfg.Panels[0].Visible = false
	if DefaultPanels[0].Visible != true {
		t.Error("DefaultConfig() should return a copy, not reference")
	}
}

// ===== FileStore tests =====

func TestFileStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	cfg := &PanelConfig{
		Version: 1,
		Panels: []PanelItem{
			{ID: "chartRaftProposals", Visible: true, Order: 0},
			{ID: "chartLeaderChanges", Visible: false, Order: 1},
		},
	}

	if err := store.Save("testuser", cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := store.Load("testuser")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// mergeWithDefaults 会补齐缺失面板，所以总数 = DefaultPanels
	if len(loaded.Panels) != len(DefaultPanels) {
		t.Errorf("loaded %d panels, want %d", len(loaded.Panels), len(DefaultPanels))
	}

	// 前两个应保持保存时的设置
	if loaded.Panels[0].ID != "chartRaftProposals" || loaded.Panels[0].Visible != true {
		t.Errorf("panel[0] = %+v", loaded.Panels[0])
	}
	if loaded.Panels[1].ID != "chartLeaderChanges" || loaded.Panels[1].Visible != false {
		t.Errorf("panel[1] = %+v", loaded.Panels[1])
	}
}

func TestFileStore_LoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	cfg, err := store.Load("nobody")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// 应返回默认配置
	if len(cfg.Panels) != len(DefaultPanels) {
		t.Errorf("default panels = %d, want %d", len(cfg.Panels), len(DefaultPanels))
	}
}
