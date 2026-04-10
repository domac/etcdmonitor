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
		Endpoint    string `yaml:"endpoint"`
		Username    string `yaml:"username"`
		Password    string `yaml:"password"`
		MetricsPath string `yaml:"metrics_path"`
		AuthEnable  *bool  `yaml:"auth_enable"`
		BinPath     string `yaml:"bin_path"`
	} `yaml:"etcd"`

	Server struct {
		Listen    string `yaml:"listen"`
		TLSEnable bool   `yaml:"tls_enable"`
		TLSCert   string `yaml:"tls_cert"`
		TLSKey    string `yaml:"tls_key"`
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
	if cfg.Etcd.AuthEnable == nil {
		defaultTrue := true
		cfg.Etcd.AuthEnable = &defaultTrue
	}
	if cfg.Etcd.BinPath == "" {
		cfg.Etcd.BinPath = "/data/services/etcd/bin"
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
