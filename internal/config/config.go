package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 应用配置结构
type Config struct {
	Etcd struct {
		Endpoint              string `yaml:"endpoint"`
		Username              string `yaml:"username"`
		Password              string `yaml:"password"`
		MetricsPath           string `yaml:"metrics_path"`
		TLSEnable             bool   `yaml:"tls_enable"`
		TLSCert               string `yaml:"tls_cert"`
		TLSKey                string `yaml:"tls_key"`
		TLSCACert             string `yaml:"tls_ca_cert"`
		TLSInsecureSkipVerify bool   `yaml:"tls_insecure_skip_verify"`
		TLSServerName         string `yaml:"tls_server_name"`
	} `yaml:"etcd"`

	Server struct {
		Listen         string `yaml:"listen"`
		TLSEnable      bool   `yaml:"tls_enable"`
		TLSCert        string `yaml:"tls_cert"`
		TLSKey         string `yaml:"tls_key"`
		SessionTimeout int    `yaml:"session_timeout"`
	} `yaml:"server"`

	Collector struct {
		Interval int `yaml:"interval"`
	} `yaml:"collector"`

	Storage struct {
		DBPath        string `yaml:"db_path"`
		RetentionDays int    `yaml:"retention_days"`
	} `yaml:"storage"`

	Log struct {
		Dir       string `yaml:"dir"`
		Filename  string `yaml:"filename"`
		Level     string `yaml:"level"`
		MaxSizeMB int    `yaml:"max_size_mb"`
		MaxFiles  int    `yaml:"max_files"`
		MaxAge    int    `yaml:"max_age"`
		Compress  bool   `yaml:"compress"`
		Console   bool   `yaml:"console"`
	} `yaml:"log"`

	KVManager struct {
		Separator       string `yaml:"separator"`
		ConnectTimeout  int    `yaml:"connect_timeout"`
		RequestTimeout  int    `yaml:"request_timeout"`
		MaxValueSize    int    `yaml:"max_value_size"`
		TabPingInterval int    `yaml:"tab_ping_interval"`
	} `yaml:"kv_manager"`

	Ops struct {
		Enable             *bool `yaml:"ops_enable"`
		AuditRetentionDays int   `yaml:"audit_retention_days"`
	} `yaml:"ops"`

	Service struct {
		RunUser string `yaml:"run_user"`
	} `yaml:"service"`

	Auth struct {
		BcryptCost             int `yaml:"bcrypt_cost"`
		LockoutThreshold       int `yaml:"lockout_threshold"`
		LockoutDurationSeconds int `yaml:"lockout_duration_seconds"`
		MinPasswordLength      int `yaml:"min_password_length"`
	} `yaml:"auth"`
}

// rawSessionTimeoutProbe 仅用于 Load 内部第二次解析，区分
// `server.session_timeout` 在 YAML 中的三种情况：
//   - 整段 server 缺失 / session_timeout 未列出   → SessionTimeout == nil
//   - session_timeout: 0 / -1 / 1234              → SessionTimeout != nil，按值处理
// 必须保留指针类型，才能识别"显式 0"与"未配置"。
type rawSessionTimeoutProbe struct {
	Server struct {
		SessionTimeout *int `yaml:"session_timeout"`
	} `yaml:"server"`
}

// Load 从文件加载配置并填充默认值
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	// 第二次解析：仅探测 server.session_timeout 是否在 YAML 中显式出现，
	// 用于区分「未配置」（→ 默认 3600）与「显式 0」（→ 永不过期，保留 0）。
	// 这是必要的：Go 的 int 零值与"用户没写"无法直接区分。
	probe := rawSessionTimeoutProbe{}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse config file (probe): %w", err)
	}
	switch {
	case probe.Server.SessionTimeout == nil:
		// 未配置 → 默认 1 小时
		cfg.Server.SessionTimeout = 3600
	case *probe.Server.SessionTimeout < 0:
		// 非法负数 → 回退默认并打印一次 WARN
		fmt.Fprintf(os.Stderr,
			"[Config] WARN: server.session_timeout=%d 非法，已回退为 3600\n",
			*probe.Server.SessionTimeout)
		cfg.Server.SessionTimeout = 3600
	default:
		// *probe == 0：永不过期，保留 0；*probe > 0：按值使用
		cfg.Server.SessionTimeout = *probe.Server.SessionTimeout
	}

	// 默认值
	if cfg.Etcd.Endpoint == "" {
		cfg.Etcd.Endpoint = "http://127.0.0.1:2379"
	}
	if cfg.Etcd.MetricsPath == "" {
		cfg.Etcd.MetricsPath = "/metrics"
	}
	// 默认 etcd 客户端 TLS 证书路径
	if cfg.Etcd.TLSCert == "" && cfg.Etcd.TLSEnable {
		cfg.Etcd.TLSCert = "certs/client.crt"
	}
	if cfg.Etcd.TLSKey == "" && cfg.Etcd.TLSEnable {
		cfg.Etcd.TLSKey = "certs/client.key"
	}
	if cfg.Etcd.TLSCACert == "" && cfg.Etcd.TLSEnable {
		cfg.Etcd.TLSCACert = "certs/ca.crt"
	}

	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":9090"
	}
	if cfg.Server.TLSCert == "" {
		cfg.Server.TLSCert = "certs/server.crt"
	}
	if cfg.Server.TLSKey == "" {
		cfg.Server.TLSKey = "certs/server.key"
	}
	// 注意：cfg.Server.SessionTimeout 已在上方 rawConfig 阶段完成三态处理
	//   nil → 3600；负数 → 3600+WARN；0 → 保留 0（永不过期）；正数 → 直接使用
	// 这里不要再用 `<= 0 ⇒ 3600` 兜底，否则会把「永不过期」语义吞掉。
	if cfg.Collector.Interval <= 0 {
		cfg.Collector.Interval = 30
	}
	if cfg.Storage.DBPath == "" {
		cfg.Storage.DBPath = "data/etcdmonitor.db"
	}
	if cfg.Storage.RetentionDays <= 0 {
		cfg.Storage.RetentionDays = 7
	}
	if cfg.Log.Dir == "" {
		cfg.Log.Dir = "logs"
	}
	if cfg.Log.Filename == "" {
		cfg.Log.Filename = "etcdmonitor.log"
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.MaxSizeMB <= 0 {
		cfg.Log.MaxSizeMB = 50
	}
	if cfg.Log.MaxFiles <= 0 {
		cfg.Log.MaxFiles = 5
	}
	if cfg.Log.MaxAge <= 0 {
		cfg.Log.MaxAge = 30
	}

	// KVManager 默认值
	if cfg.KVManager.Separator == "" {
		cfg.KVManager.Separator = "/"
	}
	if cfg.KVManager.ConnectTimeout <= 0 {
		cfg.KVManager.ConnectTimeout = 5
	}
	if cfg.KVManager.RequestTimeout <= 0 {
		cfg.KVManager.RequestTimeout = 30
	}
	if cfg.KVManager.MaxValueSize <= 0 {
		cfg.KVManager.MaxValueSize = 2 * 1024 * 1024 // 2MB
	}
	// TabPingInterval: 默认 30 秒，最小 10 秒；越界打印 WARN 并夹紧
	if cfg.KVManager.TabPingInterval == 0 {
		cfg.KVManager.TabPingInterval = 30
	} else if cfg.KVManager.TabPingInterval < 10 {
		fmt.Fprintf(os.Stderr,
			"[Config] WARN: kv_manager.tab_ping_interval=%d 小于最小值 10 秒，已强制为 10\n",
			cfg.KVManager.TabPingInterval)
		cfg.KVManager.TabPingInterval = 10
	}

	// Ops 默认值
	if cfg.Ops.Enable == nil {
		t := true
		cfg.Ops.Enable = &t
	}
	if cfg.Ops.AuditRetentionDays <= 0 {
		cfg.Ops.AuditRetentionDays = 7
	}

	// Auth 默认值
	// 注意：BcryptCost 的越界（<8 或 >14）回退与 WARN 日志由 auth.HashPassword 负责，
	// 这里仅把未配置（0）的情况补成默认 10。
	if cfg.Auth.BcryptCost == 0 {
		cfg.Auth.BcryptCost = 10
	}
	if cfg.Auth.LockoutThreshold <= 0 {
		cfg.Auth.LockoutThreshold = 5
	}
	if cfg.Auth.LockoutDurationSeconds <= 0 {
		cfg.Auth.LockoutDurationSeconds = 900
	}
	if cfg.Auth.MinPasswordLength <= 0 {
		cfg.Auth.MinPasswordLength = 8
	}

	return cfg, nil
}

// NormalizeEndpoint 统一 etcd 地址格式用于比较
func NormalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	endpoint = strings.TrimRight(endpoint, "/")
	if strings.HasPrefix(endpoint, "HTTP://") {
		endpoint = "http://" + endpoint[7:]
	} else if strings.HasPrefix(endpoint, "HTTPS://") {
		endpoint = "https://" + endpoint[8:]
	}
	return endpoint
}

// EtcdEndpoints 解析 Endpoint 配置为地址列表（支持逗号分隔多地址）
func (cfg *Config) EtcdEndpoints() []string {
	parts := strings.Split(cfg.Etcd.Endpoint, ",")
	var eps []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			eps = append(eps, p)
		}
	}
	if len(eps) == 0 {
		eps = []string{"http://127.0.0.1:2379"}
	}
	return eps
}

// EtcdFirstEndpoint 返回配置的第一个 etcd 地址（仅用于本地节点匹配等无需连接的场景）
func (cfg *Config) EtcdFirstEndpoint() string {
	eps := cfg.EtcdEndpoints()
	return eps[0]
}

// OpsEnabled 返回运维面板是否启用
func (cfg *Config) OpsEnabled() bool {
	if cfg.Ops.Enable == nil {
		return true
	}
	return *cfg.Ops.Enable
}
