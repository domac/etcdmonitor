package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"etcdmonitor/internal/logger"
)

func (a *API) handleMembers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	a.setCORSHeader(w, r)

	members := a.collector.GetMembers()
	defaultID := a.collector.GetDefaultMemberID()

	resp := map[string]interface{}{
		"members":           members,
		"default_member_id": defaultID,
		"timestamp":         time.Now().Unix(),
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Errorf("[API] Encode error: %v", err)
	}
}

func (a *API) handleCurrent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	a.setCORSHeader(w, r)

	memberID := a.resolveMemberID(r)
	latest := a.collector.GetLatest(memberID)

	resp := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"member_id": memberID,
		"metrics":   latest,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Errorf("[API] Encode error: %v", err)
	}
}

func (a *API) handleRange(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	a.setCORSHeader(w, r)

	memberID := a.resolveMemberID(r)

	metricsParam := r.URL.Query().Get("metrics")
	if metricsParam == "" {
		http.Error(w, `{"error":"metrics parameter required"}`, http.StatusBadRequest)
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

	if rangeParam := r.URL.Query().Get("range"); rangeParam != "" {
		duration, err := parseDuration(rangeParam)
		if err != nil {
			http.Error(w, `{"error":"invalid range parameter"}`, http.StatusBadRequest)
			return
		}
		end = time.Now().Add(buffer)
		start = end.Add(-duration).Add(-buffer)
	} else {
		startParam := r.URL.Query().Get("start")
		endParam := r.URL.Query().Get("end")

		if startParam == "" || endParam == "" {
			end = time.Now()
			start = end.Add(-1 * time.Hour)
		} else {
			// Input validation: parse timestamps properly
			startTs, err := strconv.ParseInt(startParam, 10, 64)
			if err != nil {
				http.Error(w, `{"error":"invalid start parameter"}`, http.StatusBadRequest)
				return
			}
			endTs, err := strconv.ParseInt(endParam, 10, 64)
			if err != nil {
				http.Error(w, `{"error":"invalid end parameter"}`, http.StatusBadRequest)
				return
			}
			start = time.Unix(startTs, 0)
			end = time.Unix(endTs, 0)
		}
	}

	// Bounds check: prevent excessive data queries
	const maxRangeAllowed = 90 * 24 * time.Hour // 90 days max
	if end.Sub(start) > maxRangeAllowed {
		http.Error(w, `{"error":"requested range too large (max 90 days)"}`, http.StatusBadRequest)
		return
	}

	// Sanity check: start should not be in far future, end should not be in far past
	now := time.Now()
	if start.After(now.Add(24 * time.Hour)) {
		http.Error(w, `{"error":"start time cannot be in far future"}`, http.StatusBadRequest)
		return
	}
	if end.Before(now.Add(-365 * 24 * time.Hour)) {
		http.Error(w, `{"error":"end time too far in past"}`, http.StatusBadRequest)
		return
	}

	data, err := a.store.QueryRange(memberID, metricNames, start, end)
	if err != nil {
		logger.Errorf("[API] QueryRange error: %v", err)
		http.Error(w, `{"error":"query failed"}`, http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"start":     start.Unix(),
		"end":       end.Unix(),
		"member_id": memberID,
		"metrics":   data,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Errorf("[API] Encode error: %v", err)
	}
}

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	a.setCORSHeader(w, r)

	members := a.collector.GetMembers()
	defaultID := a.collector.GetDefaultMemberID()

	defaultLatest := a.collector.GetLatest(defaultID)
	collectorUp := false
	if defaultLatest != nil {
		collectorUp = defaultLatest["collector_up"] == 1
	}

	resp := map[string]interface{}{
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
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Errorf("[API] Encode error: %v", err)
	}
}

func (a *API) handleDebug(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// DEBUG endpoint - restrict to localhost only
	a.setCORSHeader(w, r)

	// Check if request is from localhost
	remoteIP := getRemoteIP(r)
	if !isLocalhost(remoteIP) {
		http.Error(w, `{"error":"debug endpoint restricted to localhost"}`, http.StatusForbidden)
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

	resp := map[string]interface{}{
		"db_member_ids":     memberIDs,
		"collector_members": memberList,
		"default_member_id": defaultID,
		"timestamp":         time.Now().Unix(),
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logger.Errorf("[API] Encode error: %v", err)
	}
}

// === CORS Helper ===

// allowedOrigins is the whitelist of origins permitted for CORS requests.
// Since the dashboard is embedded and served from the same origin,
// only localhost variants need to be allowed.
var allowedOrigins = map[string]bool{
	"http://localhost":       true,
	"https://localhost":      true,
	"http://127.0.0.1":      true,
	"https://127.0.0.1":     true,
	"http://[::1]":          true,
	"https://[::1]":         true,
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
func (a *API) setCORSHeader(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Same-origin request, no CORS header needed
		return
	}
	if a.isAllowedOrigin(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
}

// === IP Helper ===

// getRemoteIP extracts the remote IP from the request.
// SECURITY: X-Forwarded-For is NOT trusted because it can be spoofed.
// Only RemoteAddr (set by the Go HTTP server from the TCP connection) is used.
func getRemoteIP(r *http.Request) string {
	remoteAddr := r.RemoteAddr
	// Handle IPv6 addresses like [::1]:port
	if len(remoteAddr) > 0 && remoteAddr[0] == '[' {
		for i, c := range remoteAddr {
			if c == ']' {
				return remoteAddr[:i+1]
			}
		}
		return remoteAddr
	}
	// Remove port from IPv4 address
	for i := len(remoteAddr) - 1; i >= 0; i-- {
		if remoteAddr[i] == ':' {
			return remoteAddr[:i]
		}
	}
	return remoteAddr
}

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
