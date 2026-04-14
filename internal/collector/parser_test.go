package collector

import (
	"math"
	"os"
	"strings"
	"testing"
)

// ===== ParsePrometheusText tests =====

func TestParsePrometheusText_StandardFormat(t *testing.T) {
	input := `# HELP etcd_server_proposals_committed_total The total number of consensus proposals committed.
# TYPE etcd_server_proposals_committed_total counter
etcd_server_proposals_committed_total 12345
# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 42
`
	families := ParsePrometheusText(strings.NewReader(input))
	if len(families) < 2 {
		t.Fatalf("expected at least 2 families, got %d", len(families))
	}

	familyMap := make(map[string]MetricFamily)
	for _, f := range families {
		familyMap[f.Name] = f
	}

	proposals, ok := familyMap["etcd_server_proposals_committed_total"]
	if !ok {
		t.Fatal("missing family etcd_server_proposals_committed_total")
	}
	if proposals.Type != "counter" {
		t.Errorf("Type = %q, want %q", proposals.Type, "counter")
	}
	if proposals.Help != "The total number of consensus proposals committed." {
		t.Errorf("Help = %q", proposals.Help)
	}
	if len(proposals.Metrics) != 1 || proposals.Metrics[0].Value != 12345 {
		t.Errorf("Metrics = %v, want value 12345", proposals.Metrics)
	}

	goroutines, ok := familyMap["go_goroutines"]
	if !ok {
		t.Fatal("missing family go_goroutines")
	}
	if goroutines.Type != "gauge" {
		t.Errorf("Type = %q, want %q", goroutines.Type, "gauge")
	}
	if len(goroutines.Metrics) != 1 || goroutines.Metrics[0].Value != 42 {
		t.Errorf("Metrics = %v, want value 42", goroutines.Metrics)
	}
}

func TestParsePrometheusText_EmptyInput(t *testing.T) {
	families := ParsePrometheusText(strings.NewReader(""))
	if len(families) != 0 {
		t.Errorf("expected 0 families, got %d", len(families))
	}
}

func TestParsePrometheusText_CommentsOnly(t *testing.T) {
	input := `# This is a comment
# Another comment
`
	families := ParsePrometheusText(strings.NewReader(input))
	if len(families) != 0 {
		t.Errorf("expected 0 families, got %d", len(families))
	}
}

func TestParsePrometheusText_FromTestdata(t *testing.T) {
	data, err := os.ReadFile("testdata/etcd_metrics.txt")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	families := ParsePrometheusText(strings.NewReader(string(data)))
	if len(families) == 0 {
		t.Fatal("expected non-empty families from testdata")
	}

	// 验证能解析出已知的指标族
	familyMap := make(map[string]MetricFamily)
	for _, f := range families {
		familyMap[f.Name] = f
	}

	expectedNames := []string{
		"etcd_server_proposals_committed_total",
		"etcd_disk_wal_fsync_duration_seconds",
		"grpc_server_handled_total",
		"go_gc_duration_seconds",
		"etcd_server_client_requests_total",
	}
	for _, name := range expectedNames {
		if _, ok := familyMap[name]; !ok {
			t.Errorf("missing expected family %q", name)
		}
	}
}

// ===== parseMetricLine tests =====

func TestParseMetricLine_WithLabels(t *testing.T) {
	line := `grpc_server_handled_total{grpc_code="OK",grpc_method="Range"} 50000`
	name, labels, value := parseMetricLine(line)

	if name != "grpc_server_handled_total" {
		t.Errorf("name = %q, want %q", name, "grpc_server_handled_total")
	}
	if labels["grpc_code"] != "OK" {
		t.Errorf("labels[grpc_code] = %q, want %q", labels["grpc_code"], "OK")
	}
	if labels["grpc_method"] != "Range" {
		t.Errorf("labels[grpc_method] = %q, want %q", labels["grpc_method"], "Range")
	}
	if value != 50000 {
		t.Errorf("value = %f, want %f", value, 50000.0)
	}
}

func TestParseMetricLine_WithoutLabels(t *testing.T) {
	line := `go_goroutines 42`
	name, labels, value := parseMetricLine(line)

	if name != "go_goroutines" {
		t.Errorf("name = %q, want %q", name, "go_goroutines")
	}
	if len(labels) != 0 {
		t.Errorf("labels = %v, want empty", labels)
	}
	if value != 42 {
		t.Errorf("value = %f, want %f", value, 42.0)
	}
}

func TestParseMetricLine_ScientificNotation(t *testing.T) {
	line := `process_resident_memory_bytes 5.24288e+07`
	name, _, value := parseMetricLine(line)

	if name != "process_resident_memory_bytes" {
		t.Errorf("name = %q", name)
	}
	if value != 5.24288e+07 {
		t.Errorf("value = %f, want %f", value, 5.24288e+07)
	}
}

func TestParseMetricLine_InvalidLine(t *testing.T) {
	tests := []string{
		"",
		"single_token",
		"name{broken_brace 123",
	}
	for _, line := range tests {
		name, _, _ := parseMetricLine(line)
		if name != "" {
			t.Errorf("parseMetricLine(%q) name = %q, want empty", line, name)
		}
	}
}

// ===== parseLabels tests =====

func TestParseLabels_Normal(t *testing.T) {
	s := `grpc_code="OK",grpc_method="Range"`
	labels := parseLabels(s)

	if labels["grpc_code"] != "OK" {
		t.Errorf("grpc_code = %q, want %q", labels["grpc_code"], "OK")
	}
	if labels["grpc_method"] != "Range" {
		t.Errorf("grpc_method = %q, want %q", labels["grpc_method"], "Range")
	}
}

func TestParseLabels_EscapedValue(t *testing.T) {
	// parseLabels 的转义处理：\\ 转为下一个字符
	s := `key="val\"ue"`
	labels := parseLabels(s)

	if labels["key"] != `val"ue` {
		t.Errorf("key = %q, want %q", labels["key"], `val"ue`)
	}
}

func TestParseLabels_Empty(t *testing.T) {
	labels := parseLabels("")
	if len(labels) != 0 {
		t.Errorf("expected empty labels, got %v", labels)
	}
}

func TestParseLabels_SingleLabel(t *testing.T) {
	labels := parseLabels(`le="0.001"`)
	if labels["le"] != "0.001" {
		t.Errorf("le = %q, want %q", labels["le"], "0.001")
	}
}

// ===== FirstValue / SumValues tests =====

func TestFirstValue_SkipsBucketAndQuantile(t *testing.T) {
	family := MetricFamily{
		Metrics: []Metric{
			{Labels: map[string]string{"le": "0.001"}, Value: 100},
			{Labels: map[string]string{"le": "0.01"}, Value: 200},
			{Labels: map[string]string{"quantile": "0.5"}, Value: 0.003},
			{Labels: map[string]string{}, Value: 42},
		},
	}

	got := FirstValue(family)
	if got != 42 {
		t.Errorf("FirstValue() = %f, want %f", got, 42.0)
	}
}

func TestFirstValue_OnlyBuckets(t *testing.T) {
	family := MetricFamily{
		Metrics: []Metric{
			{Labels: map[string]string{"le": "0.001"}, Value: 100},
			{Labels: map[string]string{"le": "0.01"}, Value: 200},
		},
	}

	got := FirstValue(family)
	if got != 100 {
		t.Errorf("FirstValue() fallback = %f, want %f", got, 100.0)
	}
}

func TestFirstValue_Empty(t *testing.T) {
	got := FirstValue(MetricFamily{})
	if got != 0 {
		t.Errorf("FirstValue() empty = %f, want 0", got)
	}
}

func TestSumValues(t *testing.T) {
	family := MetricFamily{
		Metrics: []Metric{
			{Labels: map[string]string{"le": "0.001"}, Value: 100},
			{Labels: map[string]string{}, Value: 10},
			{Labels: map[string]string{}, Value: 20},
			{Labels: map[string]string{"quantile": "0.5"}, Value: 0.003},
			{Labels: map[string]string{}, Value: 30},
		},
	}

	got := SumValues(family)
	if got != 60 {
		t.Errorf("SumValues() = %f, want %f", got, 60.0)
	}
}

// ===== ExtractHistogram tests =====

func TestExtractHistogram(t *testing.T) {
	family := MetricFamily{
		Metrics: []Metric{
			{Labels: map[string]string{"le": "0.001"}, Value: 100},
			{Labels: map[string]string{"le": "0.002"}, Value: 200},
			{Labels: map[string]string{"le": "0.004"}, Value: 400},
			{Labels: map[string]string{"le": "0.008"}, Value: 600},
			{Labels: map[string]string{"le": "0.016"}, Value: 800},
			{Labels: map[string]string{"le": "0.032"}, Value: 900},
			{Labels: map[string]string{"le": "0.064"}, Value: 950},
			{Labels: map[string]string{"le": "0.128"}, Value: 980},
			{Labels: map[string]string{"le": "0.256"}, Value: 995},
			{Labels: map[string]string{"le": "0.512"}, Value: 999},
			{Labels: map[string]string{"le": "1.024"}, Value: 1000},
			{Labels: map[string]string{"le": "+Inf"}, Value: 1000},
		},
	}

	snapshot := make(map[string]float64)
	ExtractHistogram(family, "wal_fsync", snapshot)

	// 验证 count
	if snapshot["wal_fsync_count"] != 1000 {
		t.Errorf("count = %f, want 1000", snapshot["wal_fsync_count"])
	}

	// p50 应该在合理范围内（线性插值结果）
	p50 := snapshot["wal_fsync_p50"]
	if p50 <= 0 || p50 > 0.016 {
		t.Errorf("p50 = %f, want between 0 and 0.016", p50)
	}

	// p90 应该在合理范围内
	p90 := snapshot["wal_fsync_p90"]
	if p90 <= 0 || p90 > 0.064 {
		t.Errorf("p90 = %f, want between 0 and 0.064", p90)
	}

	// p99 应该在合理范围内
	p99 := snapshot["wal_fsync_p99"]
	if p99 <= 0 || p99 > 0.512 {
		t.Errorf("p99 = %f, want between 0 and 0.512", p99)
	}
}

func TestExtractHistogram_EmptyBuckets(t *testing.T) {
	family := MetricFamily{}
	snapshot := make(map[string]float64)
	ExtractHistogram(family, "test", snapshot)

	if len(snapshot) != 0 {
		t.Errorf("expected empty snapshot, got %v", snapshot)
	}
}

func TestHistogramPercentile_ZeroTotal(t *testing.T) {
	buckets := []struct {
		le    float64
		count float64
	}{
		{0.001, 0},
		{0.01, 0},
		{math.Inf(1), 0},
	}

	got := histogramPercentile(buckets, 0.5)
	if got != 0 {
		t.Errorf("histogramPercentile with zero total = %f, want 0", got)
	}
}

func TestHistogramPercentile_EmptyBuckets(t *testing.T) {
	got := histogramPercentile(nil, 0.5)
	if got != 0 {
		t.Errorf("histogramPercentile with nil = %f, want 0", got)
	}
}

// ===== ExtractHistogramMs tests =====

func TestExtractHistogramMs(t *testing.T) {
	family := MetricFamily{
		Metrics: []Metric{
			{Labels: map[string]string{"le": "1"}, Value: 100},
			{Labels: map[string]string{"le": "2"}, Value: 500},
			{Labels: map[string]string{"le": "4"}, Value: 900},
			{Labels: map[string]string{"le": "8"}, Value: 1000},
			{Labels: map[string]string{"le": "+Inf"}, Value: 1000},
		},
	}

	snapshot := make(map[string]float64)
	ExtractHistogramMs(family, "test", snapshot)

	// ms 转 s：值应该被除以 1000
	p50 := snapshot["test_p50"]
	if p50 >= 1 {
		t.Errorf("p50 = %f, should be < 1 (ms converted to s)", p50)
	}
}

// ===== ExtractSummary tests =====

func TestExtractSummary(t *testing.T) {
	family := MetricFamily{
		Metrics: []Metric{
			{Labels: map[string]string{"quantile": "0"}, Value: 0.00001},
			{Labels: map[string]string{"quantile": "0.5"}, Value: 0.00003},
			{Labels: map[string]string{"quantile": "0.75"}, Value: 0.00005},
			{Labels: map[string]string{"quantile": "1"}, Value: 0.001},
		},
	}

	snapshot := make(map[string]float64)
	ExtractSummary(family, "gc", snapshot)

	if snapshot["gc_q0"] != 0.00001 {
		t.Errorf("q0 = %f, want %f", snapshot["gc_q0"], 0.00001)
	}
	if snapshot["gc_q05"] != 0.00003 {
		t.Errorf("q0.5 = %f, want %f", snapshot["gc_q05"], 0.00003)
	}
	if snapshot["gc_q1"] != 0.001 {
		t.Errorf("q1 = %f, want %f", snapshot["gc_q1"], 0.001)
	}
}

// ===== ExtractGRPCMetrics tests =====

func TestExtractGRPCMetrics(t *testing.T) {
	family := MetricFamily{
		Metrics: []Metric{
			{Labels: map[string]string{"grpc_code": "OK", "grpc_method": "Range"}, Value: 50000},
			{Labels: map[string]string{"grpc_code": "OK", "grpc_method": "Put"}, Value: 10000},
			{Labels: map[string]string{"grpc_code": "InvalidArgument", "grpc_method": "Range"}, Value: 100},
			{Labels: map[string]string{"grpc_code": "OK", "grpc_method": "Watch"}, Value: 2000},
		},
	}

	snapshot := make(map[string]float64)
	ExtractGRPCMetrics(family, snapshot)

	totalExpected := 50000.0 + 10000.0 + 100.0 + 2000.0
	if snapshot["grpc_server_handled_total"] != totalExpected {
		t.Errorf("total = %f, want %f", snapshot["grpc_server_handled_total"], totalExpected)
	}

	okExpected := 50000.0 + 10000.0 + 2000.0
	if snapshot["grpc_server_handled_ok_total"] != okExpected {
		t.Errorf("ok_total = %f, want %f", snapshot["grpc_server_handled_ok_total"], okExpected)
	}

	errorExpected := 100.0
	if snapshot["grpc_server_handled_error_total"] != errorExpected {
		t.Errorf("error_total = %f, want %f", snapshot["grpc_server_handled_error_total"], errorExpected)
	}

	// top method should be Range (50000 + 100 = 50100)
	if snapshot["grpc_method_0_count"] != 50100 {
		t.Errorf("top method count = %f, want 50100", snapshot["grpc_method_0_count"])
	}
}

// ===== ExtractClientRequests tests =====

func TestExtractClientRequests(t *testing.T) {
	family := MetricFamily{
		Metrics: []Metric{
			{Labels: map[string]string{"client_api_version": "3.0", "type": "Range"}, Value: 30000},
			{Labels: map[string]string{"client_api_version": "3.0", "type": "Put"}, Value: 8000},
			{Labels: map[string]string{"client_api_version": "2.0", "type": "GET"}, Value: 5000},
		},
	}

	snapshot := make(map[string]float64)
	ExtractClientRequests(family, snapshot)

	if snapshot["etcd_server_client_requests_total"] != 43000 {
		t.Errorf("total = %f, want 43000", snapshot["etcd_server_client_requests_total"])
	}
	if snapshot["etcd_server_client_requests_30"] != 38000 {
		t.Errorf("v3.0 = %f, want 38000", snapshot["etcd_server_client_requests_30"])
	}
	if snapshot["etcd_server_client_requests_20"] != 5000 {
		t.Errorf("v2.0 = %f, want 5000", snapshot["etcd_server_client_requests_20"])
	}
}

func TestExtractClientRequests_NoVersion(t *testing.T) {
	family := MetricFamily{
		Metrics: []Metric{
			{Labels: map[string]string{}, Value: 1000},
		},
	}

	snapshot := make(map[string]float64)
	ExtractClientRequests(family, snapshot)

	if snapshot["etcd_server_client_requests_unknown"] != 1000 {
		t.Errorf("unknown = %f, want 1000", snapshot["etcd_server_client_requests_unknown"])
	}
}

// ===== Benchmarks =====

func BenchmarkParsePrometheusText(b *testing.B) {
	data, err := os.ReadFile("testdata/etcd_metrics.txt")
	if err != nil {
		b.Fatalf("read testdata: %v", err)
	}
	input := string(data)

	b.ResetTimer()
	for b.Loop() {
		ParsePrometheusText(strings.NewReader(input))
	}
}

func BenchmarkParseMetricLine_WithLabels(b *testing.B) {
	line := `grpc_server_handled_total{grpc_code="OK",grpc_method="Range",grpc_service="etcdserverpb.KV",grpc_type="unary"} 50000`

	b.ResetTimer()
	for b.Loop() {
		parseMetricLine(line)
	}
}

func BenchmarkParseLabels(b *testing.B) {
	s := `grpc_code="OK",grpc_method="Range",grpc_service="etcdserverpb.KV",grpc_type="unary",instance="localhost:2379"`

	b.ResetTimer()
	for b.Loop() {
		parseLabels(s)
	}
}

func BenchmarkExtractHistogram(b *testing.B) {
	family := MetricFamily{
		Metrics: []Metric{
			{Labels: map[string]string{"le": "0.001"}, Value: 100},
			{Labels: map[string]string{"le": "0.002"}, Value: 200},
			{Labels: map[string]string{"le": "0.004"}, Value: 400},
			{Labels: map[string]string{"le": "0.008"}, Value: 600},
			{Labels: map[string]string{"le": "0.016"}, Value: 800},
			{Labels: map[string]string{"le": "0.032"}, Value: 900},
			{Labels: map[string]string{"le": "0.064"}, Value: 950},
			{Labels: map[string]string{"le": "0.128"}, Value: 980},
			{Labels: map[string]string{"le": "0.256"}, Value: 995},
			{Labels: map[string]string{"le": "0.512"}, Value: 999},
			{Labels: map[string]string{"le": "1.024"}, Value: 1000},
			{Labels: map[string]string{"le": "2.048"}, Value: 1000},
			{Labels: map[string]string{"le": "4.096"}, Value: 1000},
			{Labels: map[string]string{"le": "8.192"}, Value: 1000},
			{Labels: map[string]string{"le": "+Inf"}, Value: 1000},
			// sum/count
			{Labels: map[string]string{}, Value: 3.456},
			{Labels: map[string]string{}, Value: 1000},
		},
	}

	b.ResetTimer()
	for b.Loop() {
		snapshot := make(map[string]float64)
		ExtractHistogram(family, "wal_fsync", snapshot)
	}
}
