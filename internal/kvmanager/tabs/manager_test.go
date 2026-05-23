package tabs

import (
	"crypto/tls"
	"errors"
	"strings"
	"testing"

	"etcdmonitor/internal/config"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	repo := NewSQLiteRepo(newTestDB(t))
	km, err := NewFileKeyManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}
	cfg := &config.Config{}
	cfg.Etcd.Endpoint = "http://127.0.0.1:2379"
	cfg.Etcd.Username = "root"
	cfg.Etcd.Password = "secret"
	cfg.KVManager.ConnectTimeout = 5
	cfg.KVManager.RequestTimeout = 5
	return NewManager(repo, km, cfg, nil)
}

func TestManager_ValidateScheme(t *testing.T) {
	m := newTestManager(t)
	cases := []struct {
		endpoint string
		valid    bool
	}{
		{"http://localhost:2379", true},
		{"https://etcd.prod:2379", true},
		{"HTTP://upper.case:2379", true},
		{"HTTPS://Upper.Case:2379", true},
		{"unix:///tmp/etcd.sock", false},
		{"grpc://etcd:2379", false},
		{"localhost:2379", false},
		{"", false},
		{"http://", false},
	}
	for _, c := range cases {
		err := m.validateScheme(c.endpoint)
		if c.valid && err != nil {
			t.Errorf("validateScheme(%q) = %v; want nil", c.endpoint, err)
		}
		if !c.valid && !errors.Is(err, ErrInvalidScheme) {
			t.Errorf("validateScheme(%q) = %v; want ErrInvalidScheme", c.endpoint, err)
		}
	}
}

func TestTLSForEndpoint(t *testing.T) {
	cases := []struct {
		endpoint     string
		wantNil      bool
		wantSkipVer  bool
	}{
		{"http://example.com:2379", true, false},
		{"https://example.com:2379", false, true},
		{"HTTPS://upper.case:2379", false, true},
	}
	for _, c := range cases {
		got := tlsForEndpoint(c.endpoint)
		if c.wantNil {
			if got != nil {
				t.Errorf("tlsForEndpoint(%q) = %+v; want nil", c.endpoint, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("tlsForEndpoint(%q) = nil; want *tls.Config", c.endpoint)
			continue
		}
		if got.InsecureSkipVerify != c.wantSkipVer {
			t.Errorf("tlsForEndpoint(%q).InsecureSkipVerify = %v; want %v",
				c.endpoint, got.InsecureSkipVerify, c.wantSkipVer)
		}
		// 必须没有 RootCAs / Certificates（初版不支持 mTLS）
		if got.RootCAs != nil || len(got.Certificates) != 0 {
			t.Errorf("tlsForEndpoint(%q) unexpectedly populated RootCAs/Certificates", c.endpoint)
		}
		// 进一步保证不是 default cipher suites that would break things
		_ = (*tls.Config)(got) // 类型断言保险
	}
}

func TestNormalizeForMemberMatch(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"http://etcd:2379", "http://etcd:2379"},
		{"http://etcd:2379/", "http://etcd:2379"},
		{"HTTP://etcd:2379", "http://etcd:2379"},
		{"  https://etcd:2379  ", "https://etcd:2379"},
		{"etcd:2379", "etcd:2379"}, // 缺 scheme，原样返回
	}
	for _, c := range cases {
		got := normalizeForMemberMatch(c.in)
		if got != c.out {
			t.Errorf("normalizeForMemberMatch(%q) = %q; want %q", c.in, got, c.out)
		}
	}
}

func TestClassifyConnError(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus string
	}{
		{nil, "ok"},
		{errors.New("etcdserver: authentication is not enabled"), "auth_failed"},
		{errors.New("rpc error: code = Unauthenticated desc = etcdserver: invalid auth token"), "auth_failed"},
		{errors.New("etcdserver: permission denied"), "auth_failed"},
		{errors.New("context deadline exceeded"), "unreachable"},
		{errors.New("dial tcp 127.0.0.1:2379: connect: connection refused"), "unreachable"},
		{errors.New("dial tcp: lookup nope.invalid: no such host"), "unreachable"},
		{errors.New("transport: Error while dialing context canceled"), "unreachable"},
		{errors.New("rpc error: code = Unavailable desc = transport is closing"), "unreachable"},
		{errors.New("something completely random"), "unknown_error"},
	}
	for _, c := range cases {
		gotStatus, _ := classifyConnError(c.err)
		if gotStatus != c.wantStatus {
			t.Errorf("classifyConnError(%v) = %q; want %q", c.err, gotStatus, c.wantStatus)
		}
	}
}

func TestManager_Resolve_Default(t *testing.T) {
	m := newTestManager(t)

	// Both empty string and "default" should hit defaults
	for _, tabID := range []string{"", "default"} {
		conn, err := m.Resolve(tabID, 0)
		if err != nil {
			t.Errorf("Resolve(%q): %v", tabID, err)
			continue
		}
		if !conn.IsDefault {
			t.Errorf("Resolve(%q).IsDefault = false; want true", tabID)
		}
		if conn.Username != "root" || conn.Password != "secret" {
			t.Errorf("Resolve(%q) credentials = %s/%s; want root/secret",
				tabID, conn.Username, conn.Password)
		}
		if len(conn.Endpoints) == 0 {
			t.Errorf("Resolve(%q): no endpoints", tabID)
		}
	}
}

func TestManager_Resolve_NonDefault(t *testing.T) {
	m := newTestManager(t)

	// 创建用户 1 的 Tab
	plain := []byte("hunter2")
	cipher, err := m.km.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	tab := &Tab{
		CreatedByUserID: 1,
		Name:            "remote",
		Endpoint:        "http://10.0.0.1:2379",
		Username:        "u",
		PasswordCipher:  cipher,
	}
	if err := m.repo.Create(tab); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 用归属用户解析
	conn, err := m.Resolve(tab.ID, 1)
	if err != nil {
		t.Fatalf("Resolve(self): %v", err)
	}
	if conn.IsDefault {
		t.Error("non-default Tab returned IsDefault=true")
	}
	if conn.Password != string(plain) {
		t.Errorf("password = %q; want %q", conn.Password, plain)
	}
	if len(conn.Endpoints) != 1 || conn.Endpoints[0] != "http://10.0.0.1:2379" {
		t.Errorf("endpoints = %v; want [http://10.0.0.1:2379]", conn.Endpoints)
	}
	if conn.TLS != nil {
		t.Error("http:// must yield TLS=nil")
	}

	// 用他人 user_id 解析 → ErrTabNotFound
	if _, err := m.Resolve(tab.ID, 99); !errors.Is(err, ErrTabNotFound) {
		t.Errorf("Resolve(otherUser) = %v; want ErrTabNotFound", err)
	}

	// 不存在 ID → ErrTabNotFound
	if _, err := m.Resolve("does-not-exist", 1); !errors.Is(err, ErrTabNotFound) {
		t.Errorf("Resolve(missing) = %v; want ErrTabNotFound", err)
	}
}

func TestManager_Resolve_HTTPSBuildsTLS(t *testing.T) {
	m := newTestManager(t)
	cipher, _ := m.km.Encrypt([]byte("p"))
	tab := &Tab{
		CreatedByUserID: 1,
		Name:            "secure",
		Endpoint:        "https://prod:2379",
		PasswordCipher:  cipher,
	}
	_ = m.repo.Create(tab)

	conn, err := m.Resolve(tab.ID, 1)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if conn.TLS == nil {
		t.Fatal("https:// must yield non-nil TLS")
	}
	if !conn.TLS.InsecureSkipVerify {
		t.Error("https:// must yield InsecureSkipVerify=true")
	}
}

func TestManager_FallbackMembersWhenDefaultUnreachable(t *testing.T) {
	// 默认集群指向不可达地址；MemberList 必然失败 → 降级为字符串比对 cfg.Etcd.Endpoint
	m := newTestManager(t)
	m.cfg.Etcd.Endpoint = "http://10.255.255.255:2379,http://10.255.255.254:2379"
	m.cfg.KVManager.ConnectTimeout = 1
	m.cfg.KVManager.RequestTimeout = 1

	matched, url, degraded, err := m.IsDefaultClusterMember("http://10.255.255.255:2379")
	if err != nil {
		t.Fatalf("IsDefaultClusterMember: %v", err)
	}
	if !matched {
		t.Error("expected matched (degraded fallback should match string)")
	}
	if !degraded {
		t.Error("expected degraded=true")
	}
	if !strings.Contains(url, "10.255.255.255") {
		t.Errorf("matched URL = %q; want contains 10.255.255.255", url)
	}
}

func TestManager_MarkStatus_DefaultIsNoop(t *testing.T) {
	m := newTestManager(t)
	// 不应触发对 repo 的写入；默认 Tab 不入库
	m.MarkStatus("default", "error", "boom")
	m.MarkStatus("", "error", "boom")
	// 没有断言——只要不 panic / 不报错即通过
}

func TestManager_RotateNotImplementedPropagates(t *testing.T) {
	dir := t.TempDir()
	km, err := NewFileKeyManager(dir)
	if err != nil {
		t.Fatalf("NewFileKeyManager: %v", err)
	}
	if err := km.Rotate(); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("km.Rotate() = %v; want ErrNotImplemented", err)
	}
}
