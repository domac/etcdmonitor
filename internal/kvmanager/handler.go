package kvmanager

import (
	"fmt"
	"net/http"
	"strings"

	"etcdmonitor/internal/config"
	"etcdmonitor/internal/health"

	"github.com/gin-gonic/gin"
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

// RegisterRoutes 注册 KV 管理路由到 Gin 路由组
func (h *KVHandler) RegisterRoutes(rg *gin.RouterGroup) {
	// V3 路由
	v3 := rg.Group("/kv/v3")
	{
		v3.POST("/connect", h.handleV3Connect)
		v3.GET("/connect", h.handleV3Connect)
		v3.GET("/get", h.handleV3Get)
		v3.GET("/getpath", h.handleV3GetPath)
		v3.PUT("/put", h.handleV3Put)
		v3.POST("/delete", h.handleV3Delete)
		v3.GET("/separator", h.handleV3Separator)
		v3.GET("/keys", h.handleV3Keys)
	}

	// V2 路由
	v2 := rg.Group("/kv/v2")
	{
		v2.POST("/connect", h.handleV2Connect)
		v2.GET("/connect", h.handleV2Connect)
		v2.GET("/get", h.handleV2Get)
		v2.GET("/getpath", h.handleV2GetPath)
		v2.PUT("/put", h.handleV2Put)
		v2.POST("/delete", h.handleV2Delete)
		v2.GET("/separator", h.handleV2Separator)
		v2.GET("/keys", h.handleV2Keys)
	}
}

// ===== V3 Handlers =====

func (h *KVHandler) handleV3Connect(c *gin.Context) {
	info, err := h.v3.Connect()
	if err != nil {
		h.logger.Error("v3 connect failed", zap.Error(err))
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, info)
}

func (h *KVHandler) handleV3Get(c *gin.Context) {
	key := c.Query("key")
	if key == "" {
		h.writeError(c, http.StatusBadRequest, "key is required", "")
		return
	}

	node, err := h.v3.Get(key)
	if err != nil {
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV3GetPath(c *gin.Context) {
	key := c.Query("key")
	if key == "" {
		key = h.v3.GetSeparator()
	}

	node, err := h.v3.GetPath(key)
	if err != nil {
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV3Put(c *gin.Context) {
	var req PutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, http.StatusBadRequest, "invalid request body", "")
		return
	}

	node, err := h.v3.Put(req.Key, req.Value, req.TTL)
	if err != nil {
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV3Delete(c *gin.Context) {
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, http.StatusBadRequest, "invalid request body", "")
		return
	}

	err := h.v3.Delete(req.Key, req.Dir)
	if err != nil {
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *KVHandler) handleV3Separator(c *gin.Context) {
	c.JSON(http.StatusOK, SeparatorResponse{Separator: h.v3.GetSeparator()})
}

func (h *KVHandler) handleV3Keys(c *gin.Context) {
	node, err := h.v3.Keys()
	if err != nil {
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

// ===== V2 Handlers =====

func (h *KVHandler) handleV2Connect(c *gin.Context) {
	if h.v2 == nil || !h.v2.IsAvailable() {
		h.writeError(c, http.StatusServiceUnavailable, "etcd v2 API is not available (requires --enable-v2=true)", "v2_unavailable")
		return
	}

	info, err := h.v2.Connect()
	if err != nil {
		h.logger.Error("v2 connect failed", zap.Error(err))
		h.writeError(c, http.StatusServiceUnavailable, err.Error(), "v2_unavailable")
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

	c.JSON(http.StatusOK, info)
}

func (h *KVHandler) handleV2Get(c *gin.Context) {
	if !h.checkV2Available(c) {
		return
	}

	key := c.Query("key")
	if key == "" {
		h.writeError(c, http.StatusBadRequest, "key is required", "")
		return
	}

	node, err := h.v2.Get(key)
	if err != nil {
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV2GetPath(c *gin.Context) {
	if !h.checkV2Available(c) {
		return
	}

	key := c.Query("key")
	if key == "" {
		key = h.v2.GetSeparator()
	}

	node, err := h.v2.GetPath(key)
	if err != nil {
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV2Put(c *gin.Context) {
	if !h.checkV2Available(c) {
		return
	}

	var req PutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, http.StatusBadRequest, "invalid request body", "")
		return
	}

	node, err := h.v2.Put(req.Key, req.Value, req.TTL, req.Dir)
	if err != nil {
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

func (h *KVHandler) handleV2Delete(c *gin.Context) {
	if !h.checkV2Available(c) {
		return
	}

	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.writeError(c, http.StatusBadRequest, "invalid request body", "")
		return
	}

	err := h.v2.Delete(req.Key, req.Dir)
	if err != nil {
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *KVHandler) handleV2Separator(c *gin.Context) {
	c.JSON(http.StatusOK, SeparatorResponse{Separator: h.v2.GetSeparator()})
}

func (h *KVHandler) handleV2Keys(c *gin.Context) {
	if !h.checkV2Available(c) {
		return
	}

	node, err := h.v2.Keys()
	if err != nil {
		h.writeErrorFromEtcd(c, err)
		return
	}

	c.JSON(http.StatusOK, NodeResponse{Node: *node})
}

// ===== Helper Methods =====

func (h *KVHandler) checkV2Available(c *gin.Context) bool {
	if h.v2 == nil || !h.v2.IsAvailable() {
		h.writeError(c, http.StatusServiceUnavailable, "etcd v2 API is not available (requires --enable-v2=true)", "v2_unavailable")
		return false
	}
	return true
}

func (h *KVHandler) writeError(c *gin.Context, status int, message string, code string) {
	c.JSON(status, ErrorResponse{
		Error:   message,
		Code:    code,
		Message: message,
	})
}

func (h *KVHandler) writeErrorFromEtcd(c *gin.Context, err error) {
	errStr := err.Error()

	// 检测权限不足
	if strings.Contains(errStr, "permission denied") || strings.Contains(errStr, "etcdserver: permission denied") {
		h.writeError(c, http.StatusForbidden, "permission denied: the configured etcd user does not have permission for this operation", "permission_denied")
		return
	}

	// 检测 key 不存在
	if strings.Contains(errStr, "key not found") || strings.Contains(errStr, "Key not found") {
		h.writeError(c, http.StatusNotFound, errStr, "key_not_found")
		return
	}

	// 其他错误
	h.writeError(c, http.StatusInternalServerError, errStr, "")
}
