package kvmanager

import "fmt"

// Node 表示 etcd 中的一个 key 或目录节点
type Node struct {
	Key            string `json:"key"`
	Value          string `json:"value,omitempty"`
	Dir            bool   `json:"dir"`
	TTL            int64  `json:"ttl,omitempty"`
	CreateRevision int64  `json:"createdIndex,omitempty"` // V3: CreateRevision / V2: CreatedIndex
	ModRevision    int64  `json:"modifiedIndex,omitempty"` // V3: ModRevision / V2: ModifiedIndex
	Version        int64  `json:"version,omitempty"`       // V3 only
	Nodes          []Node `json:"nodes,omitempty"`          // 子节点列表
}

// NodeResponse 是 getpath/get 等接口的返回结构
type NodeResponse struct {
	Node Node `json:"node"`
}

// ConnectInfo 是 connect 接口的返回结构，包含集群基本信息
type ConnectInfo struct {
	Version  string `json:"version"`   // etcd 版本号
	Name     string `json:"name"`      // Leader 成员名称
	Size     int64  `json:"size"`      // 数据库大小（字节）
	SizeStr  string `json:"size_str"`  // 数据库大小（格式化字符串）
}

// PutRequest 是 put 接口的请求结构
type PutRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	TTL   int64  `json:"ttl,omitempty"`
	Dir   bool   `json:"dir,omitempty"`
}

// DeleteRequest 是 delete 接口的请求结构
type DeleteRequest struct {
	Key string `json:"key"`
	Dir bool   `json:"dir"`
}

// ErrorResponse 是错误响应结构
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"` // e.g., "permission_denied"
	Message string `json:"message,omitempty"`
}

// SeparatorResponse 是 separator 接口的返回结构
type SeparatorResponse struct {
	Separator string `json:"separator"`
}

// formatSize 将字节数格式化为可读字符串
func formatSize(size int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case size >= GB:
		return fmt.Sprintf("%.2f GB", float64(size)/float64(GB))
	case size >= MB:
		return fmt.Sprintf("%.2f MB", float64(size)/float64(MB))
	case size >= KB:
		return fmt.Sprintf("%.2f KB", float64(size)/float64(KB))
	default:
		return fmt.Sprintf("%d B", size)
	}
}
