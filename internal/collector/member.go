package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/logger"
)

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

// discoverMembers 通过 etcd v3 SDK 获取集群成员列表
func (c *Collector) discoverMembers() []MemberInfo {
	if c.etcdClient == nil {
		logger.Errorf("[Collector] etcd SDK client not available, cannot discover members")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.etcdClient.MemberList(ctx)
	if err != nil {
		logger.Errorf("[Collector] SDK MemberList failed: %v", err)
		return nil
	}

	if len(resp.Members) == 0 {
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
	for _, m := range resp.Members {
		memberID := fmt.Sprintf("%d", m.ID)
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

// getLocalMemberID 通过 etcd SDK Status 获取本机 etcd 的 member ID
func (c *Collector) getLocalMemberID() string {
	if c.etcdClient == nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.etcdClient.Status(ctx, c.cfg.Etcd.Endpoint)
	if err != nil {
		logger.Errorf("[Collector] SDK endpoint status failed: %v", err)
		return ""
	}

	if resp.Header != nil && resp.Header.MemberId != 0 {
		return fmt.Sprintf("%d", resp.Header.MemberId)
	}

	return ""
}

// isLocalAddress 判断 URL 是否指向本地地址
func isLocalAddress(url string) bool {
	return strings.Contains(url, "127.0.0.1") ||
		strings.Contains(url, "localhost") ||
		strings.Contains(url, "0.0.0.0")
}
