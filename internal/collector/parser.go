package collector

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// MetricFamily 代表一组 Prometheus 指标
type MetricFamily struct {
	Name    string
	Help    string
	Type    string
	Metrics []Metric
}

// Metric 单个指标数据点
type Metric struct {
	Labels map[string]string
	Value  float64
}

// ParsePrometheusText 解析 Prometheus text format
func ParsePrometheusText(r io.Reader) []MetricFamily {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	families := make(map[string]*MetricFamily)
	var currentName string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "# HELP ") {
			parts := strings.SplitN(line[7:], " ", 2)
			name := parts[0]
			help := ""
			if len(parts) > 1 {
				help = parts[1]
			}
			if _, ok := families[name]; !ok {
				families[name] = &MetricFamily{Name: name, Help: help}
			} else {
				families[name].Help = help
			}
			currentName = name
			continue
		}

		if strings.HasPrefix(line, "# TYPE ") {
			parts := strings.SplitN(line[7:], " ", 2)
			name := parts[0]
			typ := ""
			if len(parts) > 1 {
				typ = parts[1]
			}
			if _, ok := families[name]; !ok {
				families[name] = &MetricFamily{Name: name, Type: typ}
			} else {
				families[name].Type = typ
			}
			currentName = name
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		name, labels, value := parseMetricLine(line)
		if name == "" {
			continue
		}

		familyName := name
		baseName := stripSuffixes(name)
		if currentName != "" && strings.HasPrefix(name, currentName) {
			familyName = currentName
		} else if f, ok := families[baseName]; ok {
			familyName = f.Name
		}

		if _, ok := families[familyName]; !ok {
			families[familyName] = &MetricFamily{Name: familyName}
		}

		families[familyName].Metrics = append(families[familyName].Metrics, Metric{
			Labels: labels,
			Value:  value,
		})
	}

	result := make([]MetricFamily, 0, len(families))
	for _, f := range families {
		result = append(result, *f)
	}
	return result
}

func stripSuffixes(name string) string {
	for _, suffix := range []string{"_bucket", "_count", "_sum", "_total"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}

func parseMetricLine(line string) (string, map[string]string, float64) {
	labels := make(map[string]string)
	var name, rest string

	braceIdx := strings.IndexByte(line, '{')
	if braceIdx >= 0 {
		name = line[:braceIdx]
		endBrace := strings.LastIndexByte(line, '}')
		if endBrace < 0 {
			return "", nil, 0
		}
		labelStr := line[braceIdx+1 : endBrace]
		rest = strings.TrimSpace(line[endBrace+1:])
		labels = parseLabels(labelStr)
	} else {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return "", nil, 0
		}
		name = parts[0]
		rest = parts[1]
	}

	value, err := strconv.ParseFloat(rest, 64)
	if err != nil {
		return "", nil, 0
	}
	return name, labels, value
}

func parseLabels(s string) map[string]string {
	labels := make(map[string]string)
	if s == "" {
		return labels
	}

	for s != "" {
		s = strings.TrimSpace(s)
		if s == "" {
			break
		}
		eqIdx := strings.IndexByte(s, '=')
		if eqIdx < 0 {
			break
		}
		key := strings.TrimSpace(s[:eqIdx])
		s = s[eqIdx+1:]
		if len(s) == 0 || s[0] != '"' {
			break
		}
		s = s[1:]

		var value strings.Builder
		escaped := false
		i := 0
		for i < len(s) {
			if escaped {
				value.WriteByte(s[i])
				escaped = false
			} else if s[i] == '\\' {
				escaped = true
			} else if s[i] == '"' {
				break
			} else {
				value.WriteByte(s[i])
			}
			i++
		}
		labels[key] = value.String()
		if i < len(s) {
			s = s[i+1:]
		} else {
			break
		}
		s = strings.TrimLeft(s, ", ")
	}
	return labels
}

// FirstValue 获取 MetricFamily 的第一个非 bucket/quantile 值
func FirstValue(family MetricFamily) float64 {
	for _, m := range family.Metrics {
		if _, ok := m.Labels["le"]; ok {
			continue
		}
		if _, ok := m.Labels["quantile"]; ok {
			continue
		}
		return m.Value
	}
	if len(family.Metrics) > 0 {
		return family.Metrics[0].Value
	}
	return 0
}

// SumValues 对 MetricFamily 所有非 bucket/quantile 值求和
func SumValues(family MetricFamily) float64 {
	var total float64
	for _, m := range family.Metrics {
		if _, ok := m.Labels["le"]; ok {
			continue
		}
		if _, ok := m.Labels["quantile"]; ok {
			continue
		}
		total += m.Value
	}
	return total
}

// ExtractHistogram 从 Histogram 类型指标提取 p50/p90/p99
func ExtractHistogram(family MetricFamily, prefix string, snapshot map[string]float64) {
	var buckets []struct {
		le    float64
		count float64
	}

	for _, m := range family.Metrics {
		if le, ok := m.Labels["le"]; ok {
			var leVal float64
			if le == "+Inf" {
				leVal = math.Inf(1)
			} else {
				leVal, _ = strconv.ParseFloat(le, 64)
			}
			buckets = append(buckets, struct {
				le    float64
				count float64
			}{leVal, m.Value})
		}
	}

	if len(buckets) > 0 {
		sort.Slice(buckets, func(i, j int) bool {
			return buckets[i].le < buckets[j].le
		})
		totalCount := buckets[len(buckets)-1].count
		if totalCount > 0 {
			snapshot[prefix+"_p50"] = histogramPercentile(buckets, 0.50)
			snapshot[prefix+"_p90"] = histogramPercentile(buckets, 0.90)
			snapshot[prefix+"_p99"] = histogramPercentile(buckets, 0.99)
		}
		snapshot[prefix+"_count"] = totalCount
	}
}

func histogramPercentile(buckets []struct {
	le    float64
	count float64
}, percentile float64) float64 {
	if len(buckets) == 0 {
		return 0
	}
	total := buckets[len(buckets)-1].count
	if total == 0 {
		return 0
	}
	target := percentile * total
	var prevCount, prevBound float64

	for _, b := range buckets {
		if math.IsInf(b.le, 1) {
			continue
		}
		if b.count >= target {
			fraction := float64(0)
			if b.count-prevCount > 0 {
				fraction = (target - prevCount) / (b.count - prevCount)
			}
			return prevBound + (b.le-prevBound)*fraction
		}
		prevCount = b.count
		prevBound = b.le
	}

	for i := len(buckets) - 1; i >= 0; i-- {
		if !math.IsInf(buckets[i].le, 1) {
			return buckets[i].le
		}
	}
	return 0
}

// ExtractSummary 从 Summary 类型指标提取分位数
func ExtractSummary(family MetricFamily, prefix string, snapshot map[string]float64) {
	for _, m := range family.Metrics {
		if q, ok := m.Labels["quantile"]; ok {
			snapshot[prefix+"_q"+strings.Replace(q, ".", "", 1)] = m.Value
		}
	}
}

// ExtractGRPCMetrics 提取 gRPC 按方法分组的指标
func ExtractGRPCMetrics(family MetricFamily, snapshot map[string]float64) {
	var total, totalOK float64
	methodCounts := make(map[string]float64)

	for _, m := range family.Metrics {
		total += m.Value
		if m.Labels["grpc_code"] == "OK" {
			totalOK += m.Value
		}
		if method := m.Labels["grpc_method"]; method != "" {
			methodCounts[method] += m.Value
		}
	}

	snapshot["grpc_server_handled_total"] = total
	snapshot["grpc_server_handled_ok_total"] = totalOK
	snapshot["grpc_server_handled_error_total"] = total - totalOK

	type mc struct {
		method string
		count  float64
	}
	var sorted []mc
	for m, c := range methodCounts {
		sorted = append(sorted, mc{m, c})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})
	for i, item := range sorted {
		if i >= 10 {
			break
		}
		snapshot[fmt.Sprintf("grpc_method_%d_name", i)] = 0
		snapshot[fmt.Sprintf("grpc_method_%d_count", i)] = item.count
	}
}
