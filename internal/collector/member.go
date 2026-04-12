package collector

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/logger"
)

// maxResponseSize is the maximum allowed response body size (10 MB) to prevent OOM.
const maxResponseSize = 10 * 1024 * 1024

// MemberInfo etcd 集群成员信息
type MemberInfo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	ClientURLs      []string `json:"client_urls"`
	PeerURLs        []string `json:"peer_urls"`
	Endpoint        string   `json:"endpoint"`         // 展示用（advertise URL）
	CollectEndpoint string   `json:"-"`                // 采集用（本机可能是 127.0.0.1）
	IsDefault       bool     `json:"is_default"`
}

// etcdMemberJSON etcdctl / gRPC-gateway 通用的成员 JSON 结构
type etcdMemberJSON struct {
	ID         json.Number `json:"ID"`
	Name       string      `json:"name"`
	PeerURLs   []string    `json:"peerURLs"`
	ClientURLs []string    `json:"clientURLs"`
}

type etcdMemberListJSON struct {
	Header  json.RawMessage  `json:"header"`
	Members []etcdMemberJSON `json:"members"`
}

// discoverMembers 根据 auth_enable 配置选择发现方式
func (c *Collector) discoverMembers() []MemberInfo {
	if *c.cfg.Etcd.DiscoveryViaAPI {
		return c.discoverMembersViaAPI()
	}
	return c.discoverMembersViaCtl()
}

// discoverMembersViaAPI 通过 etcd v3 gRPC-gateway HTTP API
func (c *Collector) discoverMembersViaAPI() []MemberInfo {
	endpoint := c.cfg.Etcd.Endpoint

	// Step 1: 获取认证 token
	token := ""
	if c.cfg.Etcd.Username != "" {
		authURL := endpoint + "/v3/auth/authenticate"
		authBody, err := json.Marshal(map[string]string{
			"name":     c.cfg.Etcd.Username,
			"password": c.cfg.Etcd.Password,
		})
		if err != nil {
			logger.Errorf("[Collector] Auth body marshal failed: %v", err)
			return nil
		}

		req, err := http.NewRequest("POST", authURL, bytes.NewReader(authBody))
		if err != nil {
			logger.Errorf("[Collector] Auth request create failed: %v", err)
			return nil
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			logger.Errorf("[Collector] Auth request failed: %v", err)
			return nil
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		if resp.StatusCode != http.StatusOK {
			logger.Errorf("[Collector] Auth failed (%d): %s", resp.StatusCode, string(bodyBytes[:min(len(bodyBytes), 500)]))
			logger.Warnf("[Collector] Hint: if etcd gRPC-gateway is not enabled, set auth_enable: false in config.yaml")
			return nil
		}

		var authResp struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(bodyBytes, &authResp); err != nil || authResp.Token == "" {
			logger.Errorf("[Collector] Auth token parse failed: %v, body: %s", err, string(bodyBytes[:min(len(bodyBytes), 500)]))
			return nil
		}
		token = authResp.Token
		logger.Infof("[Collector] Auth succeeded, token length: %d", len(token))
	}

	// Step 2: 获取成员列表
	memberListURL := endpoint + "/v3/cluster/member/list"
	req, err := http.NewRequest("POST", memberListURL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		logger.Errorf("[Collector] Member list request create failed: %v", err)
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		logger.Errorf("[Collector] Member list request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if resp.StatusCode != http.StatusOK {
		logger.Errorf("[Collector] Member list returned %d: %s", resp.StatusCode, string(bodyBytes[:min(len(bodyBytes), 500)]))
		logger.Errorf("[Collector] Hint: if etcd gRPC-gateway is not enabled, set auth_enable: false in config.yaml")
		return nil
	}

	return c.parseMemberListJSON(bodyBytes)
}

// discoverMembersViaCtl 通过 etcdctl 命令获取成员列表
// validateCredentials checks if username/password contain dangerous characters
func (c *Collector) validateCredentials() bool {
	// Check for shell metacharacters that could cause issues
	dangerous := []string{"$", "`", ";", "|", "&", "<", ">", "(", ")", "\n", "\r"}
	for _, char := range dangerous {
		if strings.Contains(c.cfg.Etcd.Username, char) || strings.Contains(c.cfg.Etcd.Password, char) {
			logger.Warnf("[Collector] Credentials contain dangerous character: %s", char)
			return false
		}
	}
	return true
}

func (c *Collector) discoverMembersViaCtl() []MemberInfo {
	etcdctlPath := filepath.Join(c.cfg.Etcd.BinPath, "etcdctl")

	args := []string{
		"member", "list",
		"--endpoints=" + c.cfg.Etcd.Endpoint,
		"-w", "json",
	}

	if c.cfg.Etcd.Username != "" && c.cfg.Etcd.Password != "" {
		if !c.validateCredentials() {
			logger.Warnf("[Collector] Skipping etcdctl due to invalid credentials")
			return nil
		}
		args = append(args, "--user="+c.cfg.Etcd.Username+":"+c.cfg.Etcd.Password)
	}

	// Log command without credentials for security
	safeArgs := make([]string, len(args))
	copy(safeArgs, args)
	for i, arg := range safeArgs {
		if strings.HasPrefix(arg, "--user=") {
			safeArgs[i] = "--user=***:***"
		}
	}
	logger.Infof("[Collector] Running: %s %s", etcdctlPath, strings.Join(safeArgs, " "))

	cmd := exec.Command(etcdctlPath, args...)
	cmd.Env = append(cmd.Environ(), "ETCDCTL_API=3")

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("[Collector] etcdctl failed: %v, output: %s", err, string(output[:min(len(output), 500)]))
		return nil
	}

	logger.Debugf("[Collector] etcdctl output (%d bytes): %s", len(output), string(output[:min(len(output), 1000)]))

	return c.parseMemberListJSON(output)
}

// parseMemberListJSON 解析成员列表 JSON（etcdctl 和 gRPC-gateway 格式一致）
func (c *Collector) parseMemberListJSON(data []byte) []MemberInfo {
	var mlResp etcdMemberListJSON
	if err := json.Unmarshal(data, &mlResp); err != nil {
		logger.Errorf("[Collector] Error decoding member list JSON: %v", err)
		return nil
	}

	if len(mlResp.Members) == 0 {
		logger.Warnf("[Collector] Member list is empty")
		return nil
	}

	configEndpoint := config.NormalizeEndpoint(c.cfg.Etcd.Endpoint)
	isLocalConfig := isLocalAddress(configEndpoint)

	// 如果 config 是本地地址，先查出本机对应的真实 member ID
	localMemberID := ""
	if isLocalConfig {
		localMemberID = c.getLocalMemberID()
		if localMemberID != "" {
			logger.Infof("[Collector] Local member ID resolved: %s", localMemberID)
		}
	}

	var members []MemberInfo
	for _, m := range mlResp.Members {
		memberID := m.ID.String()
		memberEndpoint := ""
		if len(m.ClientURLs) > 0 {
			memberEndpoint = m.ClientURLs[0]
		}

		// 判断是否为 config.yaml 配置的默认节点
		isDefault := false

		if isLocalConfig && localMemberID != "" {
			// 本地地址模式：用精确的 member ID 匹配
			isDefault = memberID == localMemberID
		} else {
			// 外部 IP 模式：URL 精确匹配
			for _, url := range m.ClientURLs {
				if config.NormalizeEndpoint(url) == configEndpoint {
					isDefault = true
					break
				}
			}
		}

		if isDefault {
			// 本机节点：采集用 config.yaml 的地址（127.0.0.1 更快更可靠）
			// 但 Endpoint 展示用 advertise URL（前端可读性更好）
			// 采集地址通过 collectEndpoint 单独传递
		}

		members = append(members, MemberInfo{
			ID:              memberID,
			Name:            m.Name,
			ClientURLs:      m.ClientURLs,
			PeerURLs:        m.PeerURLs,
			Endpoint:        memberEndpoint,
			CollectEndpoint: func() string { if isDefault { return c.cfg.Etcd.Endpoint }; return memberEndpoint }(),
			IsDefault:       isDefault,
		})
	}

	logger.Infof("[Collector] Discovered %d members", len(members))
	return members
}

// extractPort 从 URL 中提取端口号
func extractPort(url string) string {
	// http://10.0.1.1:2379 -> 2379
	idx := strings.LastIndex(url, ":")
	if idx < 0 {
		return ""
	}
	port := url[idx+1:]
	// 去掉路径部分
	if slashIdx := strings.Index(port, "/"); slashIdx >= 0 {
		port = port[:slashIdx]
	}
	return port
}

// isLocalAddress 判断 URL 是否指向本地地址
func isLocalAddress(url string) bool {
	return strings.Contains(url, "127.0.0.1") ||
		strings.Contains(url, "localhost") ||
		strings.Contains(url, "0.0.0.0")
}

// etcdEndpointStatus etcdctl endpoint status -w json 的输出格式
type etcdEndpointStatus struct {
	Endpoint string `json:"Endpoint"`
	Status   struct {
		Header struct {
			MemberID json.Number `json:"member_id"`
		} `json:"header"`
	} `json:"Status"`
}

// getLocalMemberID 通过 etcdctl endpoint status 获取本机 etcd 的 member ID
func (c *Collector) getLocalMemberID() string {
	etcdctlPath := filepath.Join(c.cfg.Etcd.BinPath, "etcdctl")

	args := []string{
		"endpoint", "status",
		"--endpoints=" + c.cfg.Etcd.Endpoint,
		"-w", "json",
	}

	if c.cfg.Etcd.Username != "" && c.cfg.Etcd.Password != "" {
		args = append(args, "--user="+c.cfg.Etcd.Username+":"+c.cfg.Etcd.Password)
	}

	cmd := exec.Command(etcdctlPath, args...)
	cmd.Env = append(cmd.Environ(), "ETCDCTL_API=3")

	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("[Collector] etcdctl endpoint status failed: %v", err)
		return ""
	}

	// etcdctl endpoint status -w json 返回数组
	var statuses []etcdEndpointStatus
	if err := json.Unmarshal(output, &statuses); err != nil {
		logger.Errorf("[Collector] Error parsing endpoint status: %v, output: %s", err, string(output[:min(len(output), 500)]))
		return ""
	}

	if len(statuses) > 0 {
		memberID := statuses[0].Status.Header.MemberID.String()
		if memberID != "" && memberID != "0" {
			return memberID
		}
	}

	return ""
}
