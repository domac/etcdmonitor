package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"etcdmonitor/internal/config"
)

// generateTestCertificate 生成自签名测试证书
func generateTestCertificate(certPath, keyPath string) error {
	// 生成 RSA 密钥对
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// 创建证书模板
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"etcdmonitor-test"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
			x509.ExtKeyUsageClientAuth,
		},
		DNSNames: []string{"localhost", "127.0.0.1"},
	}

	// 自签名证书
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return err
	}

	// 写入证书文件
	certFile, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certFile.Close()
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes})

	// 写入密钥文件
	keyFile, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyFile.Close()
	privateKeyBytes, _ := x509.MarshalPKCS8PrivateKey(privateKey)
	pem.Encode(keyFile, &pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyBytes})

	return nil
}

// TestLoadClientTLSConfig_Disabled 测试禁用 TLS 时返回 nil
func TestLoadClientTLSConfig_Disabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Etcd.TLSEnable = false

	tlsCfg, err := LoadClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tlsCfg != nil {
		t.Errorf("expected nil tlsCfg when TLS disabled, got: %v", tlsCfg)
	}
}

// TestLoadClientTLSConfig_EnabledWithoutCerts 测试启用 TLS 但无证书文件时
func TestLoadClientTLSConfig_EnabledWithoutCerts(t *testing.T) {
	cfg := &config.Config{}
	cfg.Etcd.TLSEnable = true
	cfg.Etcd.TLSCert = "nonexistent.crt"
	cfg.Etcd.TLSKey = "nonexistent.key"

	tlsCfg, err := LoadClientTLSConfig(cfg)
	if err == nil {
		t.Fatalf("expected error when cert files don't exist, got nil")
	}
	if tlsCfg != nil {
		t.Errorf("expected nil tlsCfg when cert loading fails, got: %v", tlsCfg)
	}
}

// TestLoadClientTLSConfig_ValidCertificates 测试使用有效证书加载 TLS 配置
func TestLoadClientTLSConfig_ValidCertificates(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "client.crt")
	keyPath := filepath.Join(tmpDir, "client.key")

	// 生成测试证书
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("failed to generate test certificate: %v", err)
	}

	cfg := &config.Config{}
	cfg.Etcd.TLSEnable = true
	cfg.Etcd.TLSCert = certPath
	cfg.Etcd.TLSKey = keyPath

	tlsCfg, err := LoadClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tlsCfg == nil {
		t.Fatalf("expected non-nil tlsCfg, got nil")
	}
	if len(tlsCfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(tlsCfg.Certificates))
	}
}

// TestLoadClientTLSConfig_CACertificate 测试 CA 证书加载
func TestLoadClientTLSConfig_CACertificate(t *testing.T) {
	tmpDir := t.TempDir()
	caCertPath := filepath.Join(tmpDir, "ca.crt")
	certPath := filepath.Join(tmpDir, "client.crt")
	keyPath := filepath.Join(tmpDir, "client.key")

	// 生成测试证书和 CA 证书（相同）
	if err := generateTestCertificate(caCertPath, filepath.Join(tmpDir, "ca.key")); err != nil {
		t.Fatalf("failed to generate CA certificate: %v", err)
	}
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("failed to generate test certificate: %v", err)
	}

	cfg := &config.Config{}
	cfg.Etcd.TLSEnable = true
	cfg.Etcd.TLSCert = certPath
	cfg.Etcd.TLSKey = keyPath
	cfg.Etcd.TLSCACert = caCertPath

	tlsCfg, err := LoadClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tlsCfg == nil {
		t.Fatalf("expected non-nil tlsCfg, got nil")
	}
	if tlsCfg.RootCAs == nil {
		t.Errorf("expected RootCAs to be set, got nil")
	}
}

// TestLoadClientTLSConfig_InvalidCACert 测试无效的 CA 证书
func TestLoadClientTLSConfig_InvalidCACert(t *testing.T) {
	tmpDir := t.TempDir()
	invalidCACertPath := filepath.Join(tmpDir, "invalid-ca.crt")

	// 写入无效的 CA 证书内容
	if err := os.WriteFile(invalidCACertPath, []byte("invalid pem content"), 0600); err != nil {
		t.Fatalf("failed to write invalid CA cert: %v", err)
	}

	cfg := &config.Config{}
	cfg.Etcd.TLSEnable = true
	cfg.Etcd.TLSCACert = invalidCACertPath

	tlsCfg, err := LoadClientTLSConfig(cfg)
	if err == nil {
		t.Fatalf("expected error when CA cert is invalid, got nil")
	}
	if tlsCfg != nil {
		t.Errorf("expected nil tlsCfg when CA cert parsing fails, got: %v", tlsCfg)
	}
}

// TestLoadClientTLSConfig_InsecureSkipVerify 测试 InsecureSkipVerify 标志
func TestLoadClientTLSConfig_InsecureSkipVerify(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "client.crt")
	keyPath := filepath.Join(tmpDir, "client.key")

	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("failed to generate test certificate: %v", err)
	}

	cfg := &config.Config{}
	cfg.Etcd.TLSEnable = true
	cfg.Etcd.TLSCert = certPath
	cfg.Etcd.TLSKey = keyPath
	cfg.Etcd.TLSInsecureSkipVerify = true

	tlsCfg, err := LoadClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tlsCfg == nil {
		t.Fatalf("expected non-nil tlsCfg, got nil")
	}
	if !tlsCfg.InsecureSkipVerify {
		t.Errorf("expected InsecureSkipVerify=true, got false")
	}
}

// TestLoadClientTLSConfig_ServerName 测试 ServerName (SNI) 字段
func TestLoadClientTLSConfig_ServerName(t *testing.T) {
	cfg := &config.Config{}
	cfg.Etcd.TLSEnable = true
	cfg.Etcd.TLSServerName = "etcd.example.com"

	tlsCfg, err := LoadClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tlsCfg == nil {
		t.Fatalf("expected non-nil tlsCfg, got nil")
	}
	if tlsCfg.ServerName != "etcd.example.com" {
		t.Errorf("expected ServerName='etcd.example.com', got '%s'", tlsCfg.ServerName)
	}
}

// TestLoadClientTLSConfig_FullConfiguration 测试完整配置
func TestLoadClientTLSConfig_FullConfiguration(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "client.crt")
	keyPath := filepath.Join(tmpDir, "client.key")
	caCertPath := filepath.Join(tmpDir, "ca.crt")

	if err := generateTestCertificate(caCertPath, filepath.Join(tmpDir, "ca.key")); err != nil {
		t.Fatalf("failed to generate CA certificate: %v", err)
	}
	if err := generateTestCertificate(certPath, keyPath); err != nil {
		t.Fatalf("failed to generate client certificate: %v", err)
	}

	cfg := &config.Config{}
	cfg.Etcd.TLSEnable = true
	cfg.Etcd.TLSCert = certPath
	cfg.Etcd.TLSKey = keyPath
	cfg.Etcd.TLSCACert = caCertPath
	cfg.Etcd.TLSInsecureSkipVerify = false
	cfg.Etcd.TLSServerName = "etcd.example.com"

	tlsCfg, err := LoadClientTLSConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tlsCfg == nil {
		t.Fatalf("expected non-nil tlsCfg, got nil")
	}

	// 验证所有字段
	if len(tlsCfg.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(tlsCfg.Certificates))
	}
	if tlsCfg.RootCAs == nil {
		t.Errorf("expected RootCAs to be set, got nil")
	}
	if tlsCfg.ServerName != "etcd.example.com" {
		t.Errorf("expected ServerName='etcd.example.com', got '%s'", tlsCfg.ServerName)
	}
	if tlsCfg.InsecureSkipVerify {
		t.Errorf("expected InsecureSkipVerify=false, got true")
	}
}
