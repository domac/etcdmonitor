package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/logger"
)

// LoadClientTLSConfig 从配置中加载 etcd 客户端 TLS 配置
// 如果 TLSEnable 为 false，返回 nil（无 TLS）
// 否则尝试加载证书文件并构建 tls.Config
func LoadClientTLSConfig(cfg *config.Config) (*tls.Config, error) {
	if !cfg.Etcd.TLSEnable {
		return nil, nil
	}

	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.Etcd.TLSInsecureSkipVerify,
		ServerName:         cfg.Etcd.TLSServerName,
	}

	// 如果指定了客户端证书，加载客户端证书对
	if cfg.Etcd.TLSCert != "" && cfg.Etcd.TLSKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.Etcd.TLSCert, cfg.Etcd.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
		logger.Infof("[TLS] Client certificate loaded: cert=%s, key=%s", cfg.Etcd.TLSCert, cfg.Etcd.TLSKey)
	}

	// 如果指定了 CA 证书，加载 CA 证书
	if cfg.Etcd.TLSCACert != "" {
		caCert, err := os.ReadFile(cfg.Etcd.TLSCACert)
		if err != nil {
			return nil, fmt.Errorf("read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsCfg.RootCAs = caCertPool
		logger.Infof("[TLS] CA certificate loaded: %s", cfg.Etcd.TLSCACert)
	}

	if cfg.Etcd.TLSInsecureSkipVerify {
		logger.Warnf("[TLS] InsecureSkipVerify is enabled - server certificate verification is disabled")
	}
	if cfg.Etcd.TLSServerName != "" {
		logger.Infof("[TLS] Server name (SNI): %s", cfg.Etcd.TLSServerName)
	}

	logger.Infof("[TLS] etcd client TLS configuration loaded successfully")
	return tlsCfg, nil
}
