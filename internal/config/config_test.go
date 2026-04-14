package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	content := `
etcd:
  endpoint: "http://10.0.1.1:2379,http://10.0.1.2:2379"
  username: "admin"
  password: "secret"
  metrics_path: "/prometheus/metrics"
server:
  listen: ":8080"
  session_timeout: 7200
collector:
  interval: 60
storage:
  db_path: "mydata/test.db"
  retention_days: 14
log:
  level: "debug"
  max_size_mb: 100
kv_manager:
  separator: "/"
  connect_timeout: 10
  request_timeout: 60
  max_value_size: 1048576
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Etcd.Endpoint != "http://10.0.1.1:2379,http://10.0.1.2:2379" {
		t.Errorf("Etcd.Endpoint = %q, want %q", cfg.Etcd.Endpoint, "http://10.0.1.1:2379,http://10.0.1.2:2379")
	}
	if cfg.Etcd.Username != "admin" {
		t.Errorf("Etcd.Username = %q, want %q", cfg.Etcd.Username, "admin")
	}
	if cfg.Etcd.MetricsPath != "/prometheus/metrics" {
		t.Errorf("Etcd.MetricsPath = %q, want %q", cfg.Etcd.MetricsPath, "/prometheus/metrics")
	}
	if cfg.Server.Listen != ":8080" {
		t.Errorf("Server.Listen = %q, want %q", cfg.Server.Listen, ":8080")
	}
	if cfg.Server.SessionTimeout != 7200 {
		t.Errorf("Server.SessionTimeout = %d, want %d", cfg.Server.SessionTimeout, 7200)
	}
	if cfg.Collector.Interval != 60 {
		t.Errorf("Collector.Interval = %d, want %d", cfg.Collector.Interval, 60)
	}
	if cfg.Storage.DBPath != "mydata/test.db" {
		t.Errorf("Storage.DBPath = %q, want %q", cfg.Storage.DBPath, "mydata/test.db")
	}
	if cfg.Storage.RetentionDays != 14 {
		t.Errorf("Storage.RetentionDays = %d, want %d", cfg.Storage.RetentionDays, 14)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
	}
	if cfg.KVManager.MaxValueSize != 1048576 {
		t.Errorf("KVManager.MaxValueSize = %d, want %d", cfg.KVManager.MaxValueSize, 1048576)
	}
}

func TestLoad_EmptyFileUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(cfgFile, []byte(""), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// 验证所有默认值
	if cfg.Etcd.Endpoint != "http://127.0.0.1:2379" {
		t.Errorf("default Etcd.Endpoint = %q, want %q", cfg.Etcd.Endpoint, "http://127.0.0.1:2379")
	}
	if cfg.Etcd.MetricsPath != "/metrics" {
		t.Errorf("default Etcd.MetricsPath = %q, want %q", cfg.Etcd.MetricsPath, "/metrics")
	}
	if cfg.Server.Listen != ":9090" {
		t.Errorf("default Server.Listen = %q, want %q", cfg.Server.Listen, ":9090")
	}
	if cfg.Server.SessionTimeout != 3600 {
		t.Errorf("default Server.SessionTimeout = %d, want %d", cfg.Server.SessionTimeout, 3600)
	}
	if cfg.Collector.Interval != 30 {
		t.Errorf("default Collector.Interval = %d, want %d", cfg.Collector.Interval, 30)
	}
	if cfg.Storage.DBPath != "data/etcdmonitor.db" {
		t.Errorf("default Storage.DBPath = %q, want %q", cfg.Storage.DBPath, "data/etcdmonitor.db")
	}
	if cfg.Storage.RetentionDays != 7 {
		t.Errorf("default Storage.RetentionDays = %d, want %d", cfg.Storage.RetentionDays, 7)
	}
	if cfg.Log.Dir != "logs" {
		t.Errorf("default Log.Dir = %q, want %q", cfg.Log.Dir, "logs")
	}
	if cfg.Log.Filename != "etcdmonitor.log" {
		t.Errorf("default Log.Filename = %q, want %q", cfg.Log.Filename, "etcdmonitor.log")
	}
	if cfg.Log.Level != "info" {
		t.Errorf("default Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
	if cfg.Log.MaxSizeMB != 50 {
		t.Errorf("default Log.MaxSizeMB = %d, want %d", cfg.Log.MaxSizeMB, 50)
	}
	if cfg.Log.MaxFiles != 5 {
		t.Errorf("default Log.MaxFiles = %d, want %d", cfg.Log.MaxFiles, 5)
	}
	if cfg.Log.MaxAge != 30 {
		t.Errorf("default Log.MaxAge = %d, want %d", cfg.Log.MaxAge, 30)
	}
	if cfg.KVManager.Separator != "/" {
		t.Errorf("default KVManager.Separator = %q, want %q", cfg.KVManager.Separator, "/")
	}
	if cfg.KVManager.ConnectTimeout != 5 {
		t.Errorf("default KVManager.ConnectTimeout = %d, want %d", cfg.KVManager.ConnectTimeout, 5)
	}
	if cfg.KVManager.RequestTimeout != 30 {
		t.Errorf("default KVManager.RequestTimeout = %d, want %d", cfg.KVManager.RequestTimeout, 30)
	}
	if cfg.KVManager.MaxValueSize != 2*1024*1024 {
		t.Errorf("default KVManager.MaxValueSize = %d, want %d", cfg.KVManager.MaxValueSize, 2*1024*1024)
	}
}

func TestLoad_TLSDefaultPaths(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	content := `
etcd:
  tls_enable: true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Etcd.TLSCert != "certs/client.crt" {
		t.Errorf("TLS default TLSCert = %q, want %q", cfg.Etcd.TLSCert, "certs/client.crt")
	}
	if cfg.Etcd.TLSKey != "certs/client.key" {
		t.Errorf("TLS default TLSKey = %q, want %q", cfg.Etcd.TLSKey, "certs/client.key")
	}
	if cfg.Etcd.TLSCACert != "certs/ca.crt" {
		t.Errorf("TLS default TLSCACert = %q, want %q", cfg.Etcd.TLSCACert, "certs/ca.crt")
	}
}

func TestLoad_TLSDisabledNoDefaultPaths(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	content := `
etcd:
  tls_enable: false
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Etcd.TLSCert != "" {
		t.Errorf("TLS disabled TLSCert = %q, want empty", cfg.Etcd.TLSCert)
	}
	if cfg.Etcd.TLSKey != "" {
		t.Errorf("TLS disabled TLSKey = %q, want empty", cfg.Etcd.TLSKey)
	}
	if cfg.Etcd.TLSCACert != "" {
		t.Errorf("TLS disabled TLSCACert = %q, want empty", cfg.Etcd.TLSCACert)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("Load() should return error for nonexistent file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(cfgFile, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	_, err := Load(cfgFile)
	if err == nil {
		t.Fatal("Load() should return error for invalid YAML")
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"http://127.0.0.1:2379", "http://127.0.0.1:2379"},
		{"http://127.0.0.1:2379/", "http://127.0.0.1:2379"},
		{"http://127.0.0.1:2379///", "http://127.0.0.1:2379"},
		{"  http://127.0.0.1:2379  ", "http://127.0.0.1:2379"},
		{"HTTP://127.0.0.1:2379", "http://127.0.0.1:2379"},
		{"HTTPS://10.0.1.1:2379/", "https://10.0.1.1:2379"},
		{"", ""},
	}

	for _, tt := range tests {
		got := NormalizeEndpoint(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeEndpoint(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEtcdEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     []string
	}{
		{
			name:     "single endpoint",
			endpoint: "http://127.0.0.1:2379",
			want:     []string{"http://127.0.0.1:2379"},
		},
		{
			name:     "multiple endpoints",
			endpoint: "http://10.0.1.1:2379,http://10.0.1.2:2379,http://10.0.1.3:2379",
			want:     []string{"http://10.0.1.1:2379", "http://10.0.1.2:2379", "http://10.0.1.3:2379"},
		},
		{
			name:     "with spaces",
			endpoint: "http://10.0.1.1:2379 , http://10.0.1.2:2379",
			want:     []string{"http://10.0.1.1:2379", "http://10.0.1.2:2379"},
		},
		{
			name:     "empty fallback",
			endpoint: "",
			want:     []string{"http://127.0.0.1:2379"},
		},
		{
			name:     "only commas",
			endpoint: ",,",
			want:     []string{"http://127.0.0.1:2379"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Etcd.Endpoint = tt.endpoint
			got := cfg.EtcdEndpoints()

			if len(got) != len(tt.want) {
				t.Fatalf("EtcdEndpoints() returned %d items, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("EtcdEndpoints()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestEtcdFirstEndpoint(t *testing.T) {
	cfg := &Config{}
	cfg.Etcd.Endpoint = "http://10.0.1.1:2379,http://10.0.1.2:2379"

	got := cfg.EtcdFirstEndpoint()
	if got != "http://10.0.1.1:2379" {
		t.Errorf("EtcdFirstEndpoint() = %q, want %q", got, "http://10.0.1.1:2379")
	}
}
