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
		Separator      string `yaml:"separator"`
		ConnectTimeout int    `yaml:"connect_timeout"`
		RequestTimeout int    `yaml:"request_timeout"`
		MaxValueSize   int    `yaml:"max_value_size"`
	} `yaml:"kv_manager"`

	Ops struct {
		Enable             *bool `yaml:"ops_enable"`
		AuditRetentionDays int   `yaml:"audit_retention_days"`
	} `yaml:"ops"`

	Auth struct {
		BcryptCost             int `yaml:"bcrypt_cost"`
		LockoutThreshold       int `yaml:"lockout_threshold"`
		LockoutDurationSeconds int `yaml:"lockout_duration_seconds"`
		MinPasswordLength      int `yaml:"min_password_length"`
	} `yaml:"auth"`
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
	if cfg.Server.SessionTimeout <= 0 {
		cfg.Server.SessionTimeout = 3600
	}
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
