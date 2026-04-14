package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"etcdmonitor/internal/logger"

	"github.com/gin-gonic/gin"
)

func (a *API) handleMembers(c *gin.Context) {
	a.setCORSHeader(c)

	members := a.collector.GetMembers()
	defaultID := a.collector.GetDefaultMemberID()

	c.JSON(http.StatusOK, gin.H{
		"members":           members,
		"default_member_id": defaultID,
		"timestamp":         time.Now().Unix(),
	})
}

func (a *API) handleCurrent(c *gin.Context) {
	a.setCORSHeader(c)

	memberID := a.resolveMemberID(c)
	latest := a.collector.GetLatest(memberID)

	c.JSON(http.StatusOK, gin.H{
		"timestamp": time.Now().Unix(),
		"member_id": memberID,
		"metrics":   latest,
	})
}

func (a *API) handleRange(c *gin.Context) {
	a.setCORSHeader(c)

	memberID := a.resolveMemberID(c)

	metricsParam := c.Query("metrics")
	if metricsParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "metrics parameter required"})
		return
	}

	var metricNames []string
	for _, m := range splitAndTrim(metricsParam, ",") {
		if m != "" {
			metricNames = append(metricNames, m)
		}
	}

	var start, end time.Time

	// 采集间隔的缓冲量：查询窗口前后各扩展一个采集周期
	// 避免短时间范围（如 1m）因边界问题丢失数据点
	buffer := time.Duration(a.cfg.Collector.Interval) * time.Second

	if rangeParam := c.Query("range"); rangeParam != "" {
		duration, err := parseDuration(rangeParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid range parameter"})
			return
		}
		end = time.Now().Add(buffer)
		start = end.Add(-duration).Add(-buffer)
	} else {
		startParam := c.Query("start")
		endParam := c.Query("end")

		if startParam == "" || endParam == "" {
			end = time.Now()
			start = end.Add(-1 * time.Hour)
		} else {
			// Input validation: parse timestamps properly
			startTs, err := strconv.ParseInt(startParam, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start parameter"})
				return
			}
			endTs, err := strconv.ParseInt(endParam, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end parameter"})
				return
			}
			start = time.Unix(startTs, 0)
			end = time.Unix(endTs, 0)
		}
	}

	// Bounds check: prevent excessive data queries
	const maxRangeAllowed = 90 * 24 * time.Hour // 90 days max
	if end.Sub(start) > maxRangeAllowed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "requested range too large (max 90 days)"})
		return
	}

	// Sanity check: start should not be in far future, end should not be in far past
	now := time.Now()
	if start.After(now.Add(24 * time.Hour)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start time cannot be in far future"})
		return
	}
	if end.Before(now.Add(-365 * 24 * time.Hour)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end time too far in past"})
		return
	}

	data, err := a.store.QueryRange(memberID, metricNames, start, end)
	if err != nil {
		logger.Errorf("[API] QueryRange error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"start":     start.Unix(),
		"end":       end.Unix(),
		"member_id": memberID,
		"metrics":   data,
	})
}

func (a *API) handleStatus(c *gin.Context) {
	a.setCORSHeader(c)

	members := a.collector.GetMembers()
	defaultID := a.collector.GetDefaultMemberID()

	defaultLatest := a.collector.GetLatest(defaultID)
	collectorUp := false
	if defaultLatest != nil {
		collectorUp = defaultLatest["collector_up"] == 1
	}

	c.JSON(http.StatusOK, gin.H{
		"status":            "running",
		"collector_up":      collectorUp,
		"etcd_endpoint":     a.cfg.Etcd.Endpoint,
		"etcd_version":      a.collector.GetVersion(),
		"collect_interval":  a.cfg.Collector.Interval,
		"retention_days":    a.cfg.Storage.RetentionDays,
		"members":           members,
		"default_member_id": defaultID,
		"member_count":      len(members),
		"timestamp":         time.Now().Unix(),
	})
}

func (a *API) handleDebug(c *gin.Context) {
	a.setCORSHeader(c)

	// DEBUG endpoint - restrict to localhost only
	clientIP := c.ClientIP()
	if !isLocalhost(clientIP) {
		c.JSON(http.StatusForbidden, gin.H{"error": "debug endpoint restricted to localhost"})
		return
	}

	memberIDs := a.store.DebugMemberIDs()
	members := a.collector.GetMembers()
	defaultID := a.collector.GetDefaultMemberID()

	type memberDebug struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Endpoint  string `json:"endpoint"`
		IsDefault bool   `json:"is_default"`
	}

	var memberList []memberDebug
	for _, m := range members {
		memberList = append(memberList, memberDebug{
			ID:        m.ID,
			Name:      m.Name,
			Endpoint:  m.Endpoint,
			IsDefault: m.IsDefault,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"db_member_ids":     memberIDs,
		"collector_members": memberList,
		"default_member_id": defaultID,
		"timestamp":         time.Now().Unix(),
	})
}

// === CORS Helper ===

// allowedOrigins is the whitelist of origins permitted for CORS requests.
// Since the dashboard is embedded and served from the same origin,
// only localhost variants need to be allowed.
var allowedOrigins = map[string]bool{
	"http://localhost":   true,
	"https://localhost":  true,
	"http://127.0.0.1":  true,
	"https://127.0.0.1": true,
	"http://[::1]":      true,
	"https://[::1]":     true,
}

// isAllowedOrigin checks if the origin matches the whitelist.
// It also dynamically allows origins with the configured listen port.
func (a *API) isAllowedOrigin(origin string) bool {
	if allowedOrigins[origin] {
		return true
	}
	// Also allow with the configured port (e.g., http://localhost:9090)
	port := a.cfg.Server.Listen
	if port != "" {
		for base := range allowedOrigins {
			if origin == base+port {
				return true
			}
		}
	}
	return false
}

// setCORSHeader sets CORS headers using a strict origin whitelist.
// Only localhost and configured listen address are allowed.
func (a *API) setCORSHeader(c *gin.Context) {
	origin := c.GetHeader("Origin")
	if origin == "" {
		// Same-origin request, no CORS header needed
		return
	}
	if a.isAllowedOrigin(origin) {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
	}
}

// === IP Helper ===

func isLocalhost(ip string) bool {
	return ip == "127.0.0.1" || ip == "localhost" || ip == "::1" || ip == "[::1]"
}

// === Helpers ===

func splitAndTrim(s string, sep string) []string {
	var parts []string
	for _, p := range strings.Split(s, sep) {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func parseDuration(s string) (time.Duration, error) {
	if len(s) == 0 {
		return time.Hour, nil
	}
	unit := s[len(s)-1]
	valueStr := s[:len(s)-1]
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, err
	}
	switch unit {
	case 'm':
		return time.Duration(value * float64(time.Minute)), nil
	case 'h':
		return time.Duration(value * float64(time.Hour)), nil
	case 'd':
		return time.Duration(value * 24 * float64(time.Hour)), nil
	default:
		return time.ParseDuration(s)
	}
}
