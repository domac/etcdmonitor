package prefs

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	if len(cfg.Panels) != len(DefaultPanels) {
		t.Errorf("len(Panels) = %d, want %d", len(cfg.Panels), len(DefaultPanels))
	}
	// Cards 字段保持为 nil（由前端 merge 补齐默认值）
	if cfg.Cards != nil {
		t.Errorf("Cards should be nil, got %+v", cfg.Cards)
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

// ===== ValidatePanelConfig tests =====

func makeCards(visibleCount int) []CardPref {
	cards := make([]CardPref, 10)
	for i := range cards {
		cards[i] = CardPref{
			ID:      "card" + string(rune('A'+i)),
			Visible: i < visibleCount,
			Order:   i,
		}
	}
	return cards
}

func TestValidatePanelConfig_Nil(t *testing.T) {
	if err := ValidatePanelConfig(nil); err != nil {
		t.Errorf("ValidatePanelConfig(nil) = %v, want nil", err)
	}
}

func TestValidatePanelConfig_NoCards(t *testing.T) {
	// 空 / nil Cards 视为合法（由前端 merge 补齐）
	cfg := &PanelConfig{Panels: DefaultPanels, Cards: nil}
	if err := ValidatePanelConfig(cfg); err != nil {
		t.Errorf("nil Cards should be valid, got %v", err)
	}

	cfg2 := &PanelConfig{Panels: DefaultPanels, Cards: []CardPref{}}
	if err := ValidatePanelConfig(cfg2); err != nil {
		t.Errorf("empty Cards should be valid, got %v", err)
	}
}

func TestValidatePanelConfig_AtLimit(t *testing.T) {
	cfg := &PanelConfig{Cards: makeCards(MaxVisibleCards)}
	if err := ValidatePanelConfig(cfg); err != nil {
		t.Errorf("Visible=MaxVisibleCards should be valid, got %v", err)
	}
}

func TestValidatePanelConfig_OverLimit(t *testing.T) {
	cfg := &PanelConfig{Cards: makeCards(MaxVisibleCards + 1)}
	err := ValidatePanelConfig(cfg)
	if err == nil {
		t.Fatal("Visible=MaxVisibleCards+1 should return error")
	}
	if !errors.Is(err, ErrTooManyVisibleCards) {
		t.Errorf("error should wrap ErrTooManyVisibleCards, got %v", err)
	}
}

func TestValidatePanelConfig_UnderLimit(t *testing.T) {
	cfg := &PanelConfig{Cards: makeCards(3)}
	if err := ValidatePanelConfig(cfg); err != nil {
		t.Errorf("Visible=3 should be valid, got %v", err)
	}
}

// ===== Legacy file (no cards field) load tests =====

func TestFileStore_LoadLegacyFile(t *testing.T) {
	dir := t.TempDir()
	// 写一个老版本 JSON：只有 panels 字段、没有 cards 字段、也没有 version
	legacy := `{
        "panels": [
            {"id": "chartRaftProposals", "visible": true, "order": 0},
            {"id": "chartLeaderChanges", "visible": false, "order": 1}
        ]
    }`
	path := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	store := NewFileStore(dir)
	cfg, err := store.Load("legacy")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// panels 被 mergeWithDefaults 补齐为 DefaultPanels 全量
	if len(cfg.Panels) != len(DefaultPanels) {
		t.Errorf("panels=%d, want %d", len(cfg.Panels), len(DefaultPanels))
	}
	// cards 必须保持为 nil（由前端 merge 补齐默认值；服务端不主动迁移）
	if cfg.Cards != nil {
		t.Errorf("legacy file cards should remain nil, got %+v", cfg.Cards)
	}
	// 用户原有的 visible 设置保留
	if cfg.Panels[0].ID != "chartRaftProposals" || !cfg.Panels[0].Visible {
		t.Errorf("panel[0] = %+v", cfg.Panels[0])
	}
	if cfg.Panels[1].ID != "chartLeaderChanges" || cfg.Panels[1].Visible {
		t.Errorf("panel[1] = %+v", cfg.Panels[1])
	}
}

func TestFileStore_SaveAndLoadWithCards(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	cfg := &PanelConfig{
		Panels: []PanelItem{{ID: "chartRaftProposals", Visible: true, Order: 0}},
		Cards: []CardPref{
			{ID: "cardLeader", Visible: true, Order: 0},
			{ID: "cardPending", Visible: false, Order: 1},
		},
	}
	if err := store.Save("u", cfg); err != nil {
		t.Fatal(err)
	}

	// 原始文件应包含 cards 字段（omitempty 仅在空数组/nil 时省略）
	raw, err := os.ReadFile(filepath.Join(dir, "u.json"))
	if err != nil {
		t.Fatal(err)
	}
	var blob map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blob); err != nil {
		t.Fatal(err)
	}
	if _, ok := blob["cards"]; !ok {
		t.Error("saved file should contain cards field")
	}

	loaded, err := store.Load("u")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Cards) != 2 {
		t.Fatalf("cards=%d, want 2", len(loaded.Cards))
	}
	if loaded.Cards[0].ID != "cardLeader" || !loaded.Cards[0].Visible {
		t.Errorf("card[0] = %+v", loaded.Cards[0])
	}
}
