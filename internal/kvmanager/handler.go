package kvmanager

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"

	"go.uber.org/zap"
)

// KVHandler 提供 KV 管理的 HTTP 接口
type KVHandler struct {
	v3        *ClientV3
	v2        *ClientV2
	cfg       *config.Config
	healthMgr *health.Manager
	logger    *zap.Logger
}

// NewKVHandler 创建 KVHandler 实例
func NewKVHandler(cfg *config.Config, logger *zap.Logger, healthMgr *health.Manager) (*KVHandler, error) {
	v3, err := NewClientV3(cfg, healthMgr)
	if err != nil {
		return nil, fmt.Errorf("create v3 client: %w", err)
	}

	v2, err := NewClientV2(cfg, healthMgr)
	if err != nil {
		logger.Warn("v2 client creation failed, v2 API will be unavailable", zap.Error(err))
	}

	return &KVHandler{
		v3:        v3,
		v2:        v2,
		cfg:       cfg,
		healthMgr: healthMgr,
		logger:    logger,
	}, nil
}

// Close 关闭所有客户端连接
func (h *KVHandler) Close() {
	if h.v3 != nil {
		h.v3.Close()
	}
}

// RegisterRoutes 注册 KV 管理路由
func (h *KVHandler) RegisterRoutes(mux *http.ServeMux, authMiddleware func(http.HandlerFunc) http.HandlerFunc, securityHeaders func(http.HandlerFunc) http.HandlerFunc) {
	// V3 路由
	mux.HandleFunc("/api/kv/v3/connect", securityHeaders(authMiddleware(h.handleV3Connect)))
	mux.HandleFunc("/api/kv/v3/get", securityHeaders(authMiddleware(h.handleV3Get)))
	mux.HandleFunc("/api/kv/v3/getpath", securityHeaders(authMiddleware(h.handleV3GetPath)))
	mux.HandleFunc("/api/kv/v3/put", securityHeaders(authMiddleware(h.handleV3Put)))
	mux.HandleFunc("/api/kv/v3/delete", securityHeaders(authMiddleware(h.handleV3Delete)))
	mux.HandleFunc("/api/kv/v3/separator", securityHeaders(authMiddleware(h.handleV3Separator)))

	// V2 路由
	mux.HandleFunc("/api/kv/v2/connect", securityHeaders(authMiddleware(h.handleV2Connect)))
	mux.HandleFunc("/api/kv/v2/get", securityHeaders(authMiddleware(h.handleV2Get)))
	mux.HandleFunc("/api/kv/v2/getpath", securityHeaders(authMiddleware(h.handleV2GetPath)))
	mux.HandleFunc("/api/kv/v2/put", securityHeaders(authMiddleware(h.handleV2Put)))
	mux.HandleFunc("/api/kv/v2/delete", securityHeaders(authMiddleware(h.handleV2Delete)))
	mux.HandleFunc("/api/kv/v2/separator", securityHeaders(authMiddleware(h.handleV2Separator)))
}

// ===== V3 Handlers =====

func (h *KVHandler) handleV3Connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	info, err := h.v3.Connect()
	if err != nil {
		h.logger.Error("v3 connect failed", zap.Error(err))
		h.writeErrorFromEtcd(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, info)
}

func (h *KVHandler) handleV3Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		h.writeError(w, http.StatusBadRequest, "key is required", "")
		return
	}

	node, err := h.v3.Get(key)
	if err != nil {
		h.writeErrorFromEtcd(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV3GetPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		key = h.v3.GetSeparator()
	}

	node, err := h.v3.GetPath(key)
	if err != nil {
		h.writeErrorFromEtcd(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV3Put(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	var req PutRequest
	if err := h.readJSON(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	if req.Key == "" {
		h.writeError(w, http.StatusBadRequest, "key is required", "")
		return
	}

	node, err := h.v3.Put(req.Key, req.Value, req.TTL)
	if err != nil {
		h.writeErrorFromEtcd(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV3Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	var req DeleteRequest
	if err := h.readJSON(r, &req); err != nil {
		// 也支持 query 参数方式
		req.Key = r.URL.Query().Get("key")
		req.Dir = r.URL.Query().Get("dir") == "true"
	}

	if req.Key == "" {
		h.writeError(w, http.StatusBadRequest, "key is required", "")
		return
	}

	err := h.v3.Delete(req.Key, req.Dir)
	if err != nil {
		h.writeErrorFromEtcd(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{"message": "ok"})
}

func (h *KVHandler) handleV3Separator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	h.writeJSON(w, http.StatusOK, SeparatorResponse{Separator: h.v3.GetSeparator()})
}

// ===== V2 Handlers =====

func (h *KVHandler) handleV2Connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	if h.v2 == nil || !h.v2.IsAvailable() {
		h.writeError(w, http.StatusServiceUnavailable, "etcd v2 API is not available (requires --enable-v2=true)", "v2_unavailable")
		return
	}

	info, err := h.v2.Connect()
	if err != nil {
		h.logger.Error("v2 connect failed", zap.Error(err))
		h.writeError(w, http.StatusServiceUnavailable, err.Error(), "v2_unavailable")
		return
	}

	// V2 协议没有 Status API，借用 V3 客户端获取集群级信息（Version/Leader/DBSize）
	if h.v3 != nil {
		if v3Info, err := h.v3.Connect(); err == nil {
			info.Version = v3Info.Version
			info.Name = v3Info.Name
			info.Size = v3Info.Size
			info.SizeStr = v3Info.SizeStr
		}
	}

	h.writeJSON(w, http.StatusOK, info)
}

func (h *KVHandler) handleV2Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	if !h.checkV2Available(w) {
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		h.writeError(w, http.StatusBadRequest, "key is required", "")
		return
	}

	node, err := h.v2.Get(key)
	if err != nil {
		h.writeErrorFromEtcd(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV2GetPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	if !h.checkV2Available(w) {
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		key = h.v2.GetSeparator()
	}

	node, err := h.v2.GetPath(key)
	if err != nil {
		h.writeErrorFromEtcd(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV2Put(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	if !h.checkV2Available(w) {
		return
	}

	var req PutRequest
	if err := h.readJSON(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	if req.Key == "" {
		h.writeError(w, http.StatusBadRequest, "key is required", "")
		return
	}

	node, err := h.v2.Put(req.Key, req.Value, req.TTL, req.Dir)
	if err != nil {
		h.writeErrorFromEtcd(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV2Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	if !h.checkV2Available(w) {
		return
	}

	var req DeleteRequest
	if err := h.readJSON(r, &req); err != nil {
		req.Key = r.URL.Query().Get("key")
		req.Dir = r.URL.Query().Get("dir") == "true"
	}

	if req.Key == "" {
		h.writeError(w, http.StatusBadRequest, "key is required", "")
		return
	}

	err := h.v2.Delete(req.Key, req.Dir)
	if err != nil {
		h.writeErrorFromEtcd(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]string{"message": "ok"})
}

func (h *KVHandler) handleV2Separator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	h.writeJSON(w, http.StatusOK, SeparatorResponse{Separator: h.v2.GetSeparator()})
}

// ===== Helper Methods =====

func (h *KVHandler) checkV2Available(w http.ResponseWriter) bool {
	if h.v2 == nil || !h.v2.IsAvailable() {
		h.writeError(w, http.StatusServiceUnavailable, "etcd v2 API is not available (requires --enable-v2=true)", "v2_unavailable")
		return false
	}
	return true
}

func (h *KVHandler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *KVHandler) writeError(w http.ResponseWriter, status int, message string, code string) {
	resp := ErrorResponse{
		Error:   message,
		Code:    code,
		Message: message,
	}
	h.writeJSON(w, status, resp)
}

func (h *KVHandler) writeErrorFromEtcd(w http.ResponseWriter, err error) {
	errStr := err.Error()

	// 检测权限不足
	if strings.Contains(errStr, "permission denied") || strings.Contains(errStr, "etcdserver: permission denied") {
		h.writeError(w, http.StatusForbidden, "permission denied: the configured etcd user does not have permission for this operation", "permission_denied")
		return
	}

	// 检测 key 不存在
	if strings.Contains(errStr, "key not found") || strings.Contains(errStr, "Key not found") {
		h.writeError(w, http.StatusNotFound, errStr, "key_not_found")
		return
	}

	// 其他错误
	h.writeError(w, http.StatusInternalServerError, errStr, "")
}

func (h *KVHandler) readJSON(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(h.cfg.KVManager.MaxValueSize)+4096))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
