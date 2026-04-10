// ============================================
// etcdmonitor - Dashboard Application
// Professional dark theme monitoring charts
// ============================================

// === Global State ===
let charts = {};
let refreshTimer = null;
let currentRange = '1h';
let currentMemberID = '';  // 当前选中的成员 ID
let members = [];          // 所有成员列表
let currentTheme = localStorage.getItem('etcdmonitor-theme') || 'dark';

// === Theme System ===
const THEMES = {
    dark: {
        colors: {
            blue: '#58a6ff', green: '#3fb950', red: '#f85149', orange: '#d29922',
            purple: '#bc8cff', cyan: '#39d2c0', yellow: '#e3b341', pink: '#f778ba', gray: '#8b949e'
        },
        axis: {
            axisLine: { lineStyle: { color: '#30363d' } },
            axisTick: { lineStyle: { color: '#30363d' } },
            axisLabel: { color: '#8b949e', fontSize: 11 },
            splitLine: { lineStyle: { color: '#21262d', type: 'dashed' } }
        },
        tooltip: {
            trigger: 'axis', backgroundColor: '#1c2333', borderColor: '#30363d',
            textStyle: { color: '#e6edf3', fontSize: 12 },
            axisPointer: { lineStyle: { color: '#58a6ff', opacity: 0.3 } }
        },
        legend: { textStyle: { color: '#8b949e', fontSize: 11 }, top: 5, right: 10 },
        btnIcon: '\u263E'
    },
    light: {
        colors: {
            blue: '#0969da', green: '#1a7f37', red: '#cf222e', orange: '#bc4c00',
            purple: '#8250df', cyan: '#0e8a7e', yellow: '#9a6700', pink: '#bf3989', gray: '#656d76'
        },
        axis: {
            axisLine: { lineStyle: { color: '#d0d7de' } },
            axisTick: { lineStyle: { color: '#d0d7de' } },
            axisLabel: { color: '#656d76', fontSize: 11 },
            splitLine: { lineStyle: { color: '#eaeef2', type: 'dashed' } }
        },
        tooltip: {
            trigger: 'axis', backgroundColor: '#ffffff', borderColor: '#d0d7de',
            textStyle: { color: '#1f2328', fontSize: 12 },
            axisPointer: { lineStyle: { color: '#0969da', opacity: 0.3 } }
        },
        legend: { textStyle: { color: '#656d76', fontSize: 11 }, top: 5, right: 10 },
        btnIcon: '\u2600'
    }
};

function T() { return THEMES[currentTheme]; }

function toggleTheme() {
    currentTheme = currentTheme === 'dark' ? 'light' : 'dark';
    localStorage.setItem('etcdmonitor-theme', currentTheme);
    applyTheme();
    refresh();
}

function applyTheme() {
    document.documentElement.setAttribute('data-theme', currentTheme);
    document.getElementById('themeBtn').textContent = T().btnIcon;
}

// === ECharts Theme Accessors (dynamic) ===
function COLORS_() { return T().colors; }

const CHART_BG = 'transparent';
const GRID = { left: 60, right: 20, top: 30, bottom: 30 };
const GRID_LEGEND = { left: 60, right: 20, top: 40, bottom: 30 };

function AXIS_STYLE_() { return T().axis; }
function TOOLTIP_() { return T().tooltip; }
function LEGEND_() { return T().legend; }

// === Utility Functions ===
function formatBytes(bytes) {
    if (bytes === 0 || bytes === undefined) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function formatDuration(seconds) {
    if (seconds === undefined || seconds === null) return '-';
    if (seconds < 0.001) return (seconds * 1000000).toFixed(0) + ' us';
    if (seconds < 1) return (seconds * 1000).toFixed(2) + ' ms';
    return seconds.toFixed(3) + ' s';
}

function formatNumber(num) {
    if (num === undefined || num === null) return '-';
    if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
    if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
    return Math.round(num).toString();
}

function formatTime(ts) {
    const d = new Date(ts * 1000);
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function makeSeries(name, color, data, opts = {}) {
    return {
        name: name,
        type: opts.type || 'line',
        smooth: true,
        symbol: 'none',
        lineStyle: { width: 2, color: color },
        itemStyle: { color: color },
        areaStyle: opts.area ? {
            color: {
                type: 'linear', x: 0, y: 0, x2: 0, y2: 1,
                colorStops: [
                    { offset: 0, color: color + '33' },
                    { offset: 1, color: color + '05' }
                ]
            }
        } : undefined,
        data: (data || []).map(p => [p.ts * 1000, p.value]),
        ...opts.extra
    };
}

// === Chart Initialization ===
function initChart(id) {
    const dom = document.getElementById(id);
    if (!dom) return null;
    const chart = echarts.init(dom, null, { renderer: 'canvas' });
    charts[id] = chart;
    return chart;
}

function initAllCharts() {
    const ids = [
        'chartRaftProposals', 'chartLeaderChanges',
        'chartProposalLag', 'chartProposalFailedRate',
        'chartWALFsync', 'chartBackendCommit',
        'chartDBSize', 'chartMVCCOps',
        'chartPeerTraffic', 'chartPeerRTT',
        'chartGRPC', 'chartGRPCTraffic',
        'chartCPU', 'chartMemory', 'chartGoroutines', 'chartGC', 'chartFDs', 'chartMemSys'
    ];
    ids.forEach(id => initChart(id));

    // Resize handling
    window.addEventListener('resize', () => {
        Object.values(charts).forEach(c => c && c.resize());
    });
}

// === Update Key Metrics Banner ===
function updateBanner(metrics) {
    if (!metrics) return;

    // CPU Usage Rate
    const cpuPercent = metrics['process_cpu_usage_percent'];
    document.getElementById('bannerCPU').textContent = cpuPercent !== undefined ? cpuPercent.toFixed(1) + '%' : '-';

    // Memory
    const mem = metrics['process_resident_memory_bytes'];
    document.getElementById('bannerMemory').textContent = mem !== undefined ? formatBytes(mem) : '-';

    // DB Size
    const dbSize = metrics['etcd_mvcc_db_total_size_in_bytes'];
    const dbInUse = metrics['etcd_mvcc_db_total_size_in_use_in_bytes'];
    document.getElementById('bannerDBSize').textContent = dbSize !== undefined ? formatBytes(dbSize) : '-';
    document.getElementById('bannerDBInUse').textContent = dbInUse !== undefined ? 'In use: ' + formatBytes(dbInUse) : '';

    // KV Total
    const keys = metrics['etcd_mvcc_keys_total'];
    document.getElementById('bannerKeys').textContent = keys !== undefined ? Math.round(keys).toLocaleString() : '-';

    // Backend Commit P99
    const commitP99 = metrics['etcd_disk_backend_commit_duration_seconds_p99'];
    const commitEl = document.getElementById('bannerCommitP99');
    commitEl.textContent = commitP99 !== undefined ? formatDuration(commitP99) : '-';
    if (commitP99 !== undefined && commitP99 > 0.025) {
        commitEl.style.color = 'var(--accent-red)';
    } else {
        commitEl.style.color = 'var(--text-primary)';
    }
}

// === Update Overview Cards ===
function updateCards(metrics) {
    if (!metrics) return;

    // Leader status
    const hasLeader = metrics['etcd_server_has_leader'];
    const isLeader = metrics['etcd_server_is_leader'];
    const el = document.getElementById('cardLeader');
    if (hasLeader === 1) {
        el.textContent = 'YES';
        el.className = 'value green';
    } else if (hasLeader === 0) {
        el.textContent = 'NO';
        el.className = 'value red';
    } else {
        el.textContent = '-';
    }
    document.getElementById('cardLeaderSub').textContent =
        isLeader === 1 ? 'This node IS leader' : 'This node is follower';

    // Leader changes
    const changes = metrics['etcd_server_leader_changes_seen_total'];
    document.getElementById('cardLeaderChanges').textContent = changes !== undefined ? formatNumber(changes) : '-';

    // WAL Fsync P99
    const walP99 = metrics['etcd_disk_wal_fsync_duration_seconds_p99'];
    const walEl = document.getElementById('cardWAL');
    walEl.textContent = walP99 !== undefined ? formatDuration(walP99) : '-';
    if (walP99 !== undefined && walP99 > 0.01) {
        walEl.className = 'value red';
    } else {
        walEl.className = 'value orange';
    }

    // Proposals Pending
    const pending = metrics['etcd_server_proposals_pending'];
    const pendingEl = document.getElementById('cardPending');
    pendingEl.textContent = pending !== undefined ? formatNumber(pending) : '-';
    if (pending !== undefined && pending > 5) {
        pendingEl.className = 'value red';
    } else {
        pendingEl.className = 'value purple';
    }

    // Commit-Apply Lag
    const lag = metrics['etcd_server_proposals_commit_apply_lag'];
    const lagEl = document.getElementById('cardLag');
    lagEl.textContent = lag !== undefined ? formatNumber(lag) : '-';
    if (lag !== undefined && lag > 50) {
        lagEl.className = 'value red';
    } else {
        lagEl.className = 'value cyan';
    }

    // Proposal Failed Rate
    const failedRate = metrics['etcd_server_proposals_failed_rate'];
    const failedRateEl = document.getElementById('cardFailedRate');
    if (failedRate !== undefined) {
        failedRateEl.textContent = failedRate.toFixed(2) + '/s';
        failedRateEl.className = failedRate > 0 ? 'value red' : 'value green';
    } else {
        failedRateEl.textContent = '-';
        failedRateEl.className = 'value red';
    }
}

// === Update Charts ===

function updateRaftProposals(data) {
    const chart = charts['chartRaftProposals'];
    if (!chart) return;
    chart.setOption({
        tooltip: TOOLTIP_(),
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: { type: 'value', ...AXIS_STYLE_(), axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatNumber(v) } },
        series: [
            makeSeries('Committed', COLORS_().green, data['etcd_server_proposals_committed_total'], { area: true }),
            makeSeries('Applied', COLORS_().blue, data['etcd_server_proposals_applied_total']),
            makeSeries('Pending', COLORS_().orange, data['etcd_server_proposals_pending']),
            makeSeries('Failed', COLORS_().red, data['etcd_server_proposals_failed_total'])
        ]
    });
}

function updateLeaderChanges(data) {
    const chart = charts['chartLeaderChanges'];
    if (!chart) return;
    chart.setOption({
        tooltip: TOOLTIP_(),
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: { type: 'value', ...AXIS_STYLE_() },
        series: [
            makeSeries('Leader Changes', COLORS_().orange, data['etcd_server_leader_changes_seen_total']),
            makeSeries('Slow Apply', COLORS_().red, data['etcd_server_slow_apply_total']),
            makeSeries('Slow Read Index', COLORS_().purple, data['etcd_server_slow_read_indexes_total'])
        ]
    });
}

function updateProposalLag(data) {
    const chart = charts['chartProposalLag'];
    if (!chart) return;
    chart.setOption({
        tooltip: TOOLTIP_(),
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: { type: 'value', ...AXIS_STYLE_(), axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatNumber(v) } },
        series: [
            makeSeries('Commit-Apply Lag', COLORS_().orange, data['etcd_server_proposals_commit_apply_lag'], { area: true })
        ]
    });
}

function updateProposalFailedRate(data) {
    const chart = charts['chartProposalFailedRate'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${p.value[1].toFixed(4)}/s<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: { type: 'value', ...AXIS_STYLE_(), axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => v.toFixed(2) + '/s' } },
        series: [
            makeSeries('Failed Rate', COLORS_().red, data['etcd_server_proposals_failed_rate'], { area: true })
        ]
    });
}

function updateWALFsync(data) {
    const chart = charts['chartWALFsync'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${formatDuration(p.value[1])}<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: {
            type: 'value', ...AXIS_STYLE_(),
            axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatDuration(v) }
        },
        visualMap: {
            show: false, dimension: 1, pieces: [
                { lt: 0.01, color: COLORS_().green },
                { gte: 0.01, lt: 0.05, color: COLORS_().orange },
                { gte: 0.05, color: COLORS_().red }
            ],
            seriesIndex: 2
        },
        series: [
            makeSeries('P50', COLORS_().green, data['etcd_disk_wal_fsync_duration_seconds_p50'], { area: true }),
            makeSeries('P90', COLORS_().orange, data['etcd_disk_wal_fsync_duration_seconds_p90']),
            makeSeries('P99', COLORS_().red, data['etcd_disk_wal_fsync_duration_seconds_p99'])
        ]
    });
}

function updateBackendCommit(data) {
    const chart = charts['chartBackendCommit'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${formatDuration(p.value[1])}<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: {
            type: 'value', ...AXIS_STYLE_(),
            axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatDuration(v) }
        },
        series: [
            makeSeries('P50', COLORS_().green, data['etcd_disk_backend_commit_duration_seconds_p50'], { area: true }),
            makeSeries('P90', COLORS_().orange, data['etcd_disk_backend_commit_duration_seconds_p90']),
            makeSeries('P99', COLORS_().red, data['etcd_disk_backend_commit_duration_seconds_p99'])
        ]
    });
}

function updateDBSize(data) {
    const chart = charts['chartDBSize'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${formatBytes(p.value[1])}<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: {
            type: 'value', ...AXIS_STYLE_(),
            axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatBytes(v) }
        },
        series: [
            makeSeries('Total Size', COLORS_().blue, data['etcd_mvcc_db_total_size_in_bytes'], { area: true }),
            makeSeries('In Use', COLORS_().cyan, data['etcd_mvcc_db_total_size_in_use_in_bytes'], { area: true })
        ]
    });
}

function updateMVCCOps(data) {
    const chart = charts['chartMVCCOps'];
    if (!chart) return;
    chart.setOption({
        tooltip: TOOLTIP_(),
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: { type: 'value', ...AXIS_STYLE_(), axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatNumber(v) } },
        series: [
            makeSeries('Put', COLORS_().green, data['etcd_mvcc_put_total']),
            makeSeries('Delete', COLORS_().red, data['etcd_mvcc_delete_total']),
            makeSeries('Txn', COLORS_().blue, data['etcd_mvcc_txn_total']),
            makeSeries('Range', COLORS_().cyan, data['etcd_mvcc_range_total'])
        ]
    });
}

function updatePeerTraffic(data) {
    const chart = charts['chartPeerTraffic'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${formatBytes(p.value[1])}<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: {
            type: 'value', ...AXIS_STYLE_(),
            axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatBytes(v) }
        },
        series: [
            makeSeries('Sent', COLORS_().blue, data['etcd_network_peer_sent_bytes_total'], { area: true }),
            makeSeries('Received', COLORS_().green, data['etcd_network_peer_received_bytes_total'], { area: true }),
            makeSeries('Send Failures', COLORS_().red, data['etcd_network_peer_sent_failures_total']),
            makeSeries('Recv Failures', COLORS_().orange, data['etcd_network_peer_received_failures_total'])
        ]
    });
}

function updatePeerRTT(data) {
    const chart = charts['chartPeerRTT'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${formatDuration(p.value[1])}<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: {
            type: 'value', ...AXIS_STYLE_(),
            axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatDuration(v) }
        },
        series: [
            makeSeries('P50', COLORS_().green, data['etcd_network_peer_round_trip_time_seconds_p50'], { area: true }),
            makeSeries('P90', COLORS_().orange, data['etcd_network_peer_round_trip_time_seconds_p90']),
            makeSeries('P99', COLORS_().red, data['etcd_network_peer_round_trip_time_seconds_p99'])
        ]
    });
}

function updateGRPC(data) {
    const chart = charts['chartGRPC'];
    if (!chart) return;
    chart.setOption({
        tooltip: TOOLTIP_(),
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: { type: 'value', ...AXIS_STYLE_(), axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatNumber(v) } },
        series: [
            makeSeries('Total', COLORS_().blue, data['grpc_server_handled_total']),
            makeSeries('OK', COLORS_().green, data['grpc_server_handled_ok_total'], { area: true }),
            makeSeries('Error', COLORS_().red, data['grpc_server_handled_error_total']),
            makeSeries('Started', COLORS_().purple, data['grpc_server_started_total'])
        ]
    });
}

function updateGRPCTraffic(data) {
    const chart = charts['chartGRPCTraffic'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${formatBytes(p.value[1])}<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: {
            type: 'value', ...AXIS_STYLE_(),
            axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatBytes(v) }
        },
        series: [
            makeSeries('gRPC Sent', COLORS_().blue, data['etcd_network_client_grpc_sent_bytes_total'], { area: true }),
            makeSeries('gRPC Received', COLORS_().green, data['etcd_network_client_grpc_received_bytes_total'], { area: true })
        ]
    });
}

function updateCPU(data) {
    const chart = charts['chartCPU'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${p.value[1].toFixed(1)}%<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: {
            type: 'value', ...AXIS_STYLE_(),
            axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => v.toFixed(1) + '%' }
        },
        series: [
            makeSeries('CPU Usage', COLORS_().red, data['process_cpu_usage_percent'], { area: true })
        ]
    });
}

function updateMemory(data) {
    const chart = charts['chartMemory'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${formatBytes(p.value[1])}<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: {
            type: 'value', ...AXIS_STYLE_(),
            axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatBytes(v) }
        },
        series: [
            makeSeries('Resident Memory', COLORS_().blue, data['process_resident_memory_bytes'], { area: true }),
            makeSeries('Heap Alloc', COLORS_().cyan, data['go_memstats_alloc_bytes'])
        ]
    });
}

function updateGoroutines(data) {
    const chart = charts['chartGoroutines'];
    if (!chart) return;
    chart.setOption({
        tooltip: TOOLTIP_(),
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: { type: 'value', ...AXIS_STYLE_(), axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatNumber(v) } },
        series: [
            makeSeries('Goroutines', COLORS_().orange, data['go_goroutines'], { area: true })
        ]
    });
}

function updateGC(data) {
    const chart = charts['chartGC'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${formatDuration(p.value[1])}<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: {
            type: 'value', ...AXIS_STYLE_(),
            axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatDuration(v) }
        },
        series: [
            makeSeries('P50', COLORS_().green, data['go_gc_duration_seconds_q050'], { area: true }),
            makeSeries('P75', COLORS_().orange, data['go_gc_duration_seconds_q075']),
            makeSeries('Max', COLORS_().red, data['go_gc_duration_seconds_q1'])
        ]
    });
}

function updateFDs(data) {
    const chart = charts['chartFDs'];
    if (!chart) return;
    chart.setOption({
        tooltip: TOOLTIP_(),
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: { type: 'value', ...AXIS_STYLE_(), axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatNumber(v) } },
        series: [
            makeSeries('Open FDs', COLORS_().purple, data['process_open_fds'], { area: true })
        ]
    });
}

function updateMemSys(data) {
    const chart = charts['chartMemSys'];
    if (!chart) return;
    chart.setOption({
        tooltip: {
            ...TOOLTIP_(),
            formatter: params => {
                let html = formatTime(params[0].value[0] / 1000) + '<br/>';
                params.forEach(p => {
                    html += `${p.marker} ${p.seriesName}: ${formatBytes(p.value[1])}<br/>`;
                });
                return html;
            }
        },
        legend: LEGEND_(),
        grid: GRID_LEGEND,
        xAxis: { type: 'time', ...AXIS_STYLE_() },
        yAxis: {
            type: 'value', ...AXIS_STYLE_(),
            axisLabel: { ...AXIS_STYLE_().axisLabel, formatter: v => formatBytes(v) }
        },
        series: [
            makeSeries('Sys Memory', COLORS_().purple, data['go_memstats_sys_bytes'], { area: true })
        ]
    });
}

// === Data Fetching ===

async function fetchCurrent() {
    try {
        const memberParam = currentMemberID ? `?member_id=${currentMemberID}` : '';
        const resp = await fetch(`/api/current${memberParam}`);
        const data = await resp.json();
        return data.metrics || {};
    } catch (e) {
        console.error('fetchCurrent error:', e);
        return null;
    }
}

async function fetchRange(metrics) {
    try {
        const memberParam = currentMemberID ? `&member_id=${currentMemberID}` : '';
        const url = `/api/range?metrics=${metrics.join(',')}&range=${currentRange}${memberParam}`;
        console.log(`[fetchRange] member_id=${currentMemberID}, range=${currentRange}`);
        const resp = await fetch(url);
        const data = await resp.json();
        const metricCount = Object.keys(data.metrics || {}).length;
        console.log(`[fetchRange] got ${metricCount} metric series, member_id in response: ${data.member_id}`);
        return data.metrics || {};
    } catch (e) {
        console.error('fetchRange error:', e);
        return {};
    }
}

async function fetchStatus() {
    try {
        const resp = await fetch('/api/status');
        return await resp.json();
    } catch (e) {
        return null;
    }
}

async function fetchMembers() {
    try {
        const resp = await fetch('/api/members');
        return await resp.json();
    } catch (e) {
        return null;
    }
}

// === All metrics we need for range queries ===
const ALL_RANGE_METRICS = [
    // Raft
    'etcd_server_proposals_committed_total', 'etcd_server_proposals_applied_total',
    'etcd_server_proposals_pending', 'etcd_server_proposals_failed_total',
    'etcd_server_proposals_commit_apply_lag', 'etcd_server_proposals_failed_rate',
    'etcd_server_leader_changes_seen_total',
    'etcd_server_slow_apply_total', 'etcd_server_slow_read_indexes_total',
    // Disk
    'etcd_disk_wal_fsync_duration_seconds_p50', 'etcd_disk_wal_fsync_duration_seconds_p90',
    'etcd_disk_wal_fsync_duration_seconds_p99',
    'etcd_disk_backend_commit_duration_seconds_p50', 'etcd_disk_backend_commit_duration_seconds_p90',
    'etcd_disk_backend_commit_duration_seconds_p99',
    // MVCC
    'etcd_mvcc_db_total_size_in_bytes', 'etcd_mvcc_db_total_size_in_use_in_bytes',
    'etcd_mvcc_put_total', 'etcd_mvcc_delete_total', 'etcd_mvcc_txn_total', 'etcd_mvcc_range_total',
    // Network
    'etcd_network_peer_sent_bytes_total', 'etcd_network_peer_received_bytes_total',
    'etcd_network_peer_sent_failures_total', 'etcd_network_peer_received_failures_total',
    'etcd_network_peer_round_trip_time_seconds_p50', 'etcd_network_peer_round_trip_time_seconds_p90',
    'etcd_network_peer_round_trip_time_seconds_p99',
    // gRPC
    'grpc_server_handled_total', 'grpc_server_handled_ok_total', 'grpc_server_handled_error_total',
    'grpc_server_started_total',
    'etcd_network_client_grpc_sent_bytes_total', 'etcd_network_client_grpc_received_bytes_total',
    // Runtime
    'process_resident_memory_bytes', 'go_memstats_alloc_bytes', 'go_memstats_sys_bytes', 'go_goroutines',
    'process_cpu_seconds_total', 'process_cpu_usage_percent', 'process_open_fds',
    'go_gc_duration_seconds_q050', 'go_gc_duration_seconds_q075', 'go_gc_duration_seconds_q1'
];

// === Main Refresh ===
async function refresh() {
    // Fetch all data in parallel
    const [currentMetrics, rangeData, status] = await Promise.all([
        fetchCurrent(),
        fetchRange(ALL_RANGE_METRICS),
        fetchStatus()
    ]);

    // Update status badge
    const badge = document.getElementById('statusBadge');
    const statusText = document.getElementById('statusText');
    if (status && status.collector_up) {
        badge.className = 'status-badge';
        statusText.textContent = 'Connected';
    } else {
        badge.className = 'status-badge error';
        statusText.textContent = 'Disconnected';
    }

    // Update members
    if (status && status.members) {
        updateMemberSelect(status.members, status.default_member_id);
        updateMemberCard(status.members, currentMetrics);
    }

    document.getElementById('lastUpdate').textContent =
        'Updated: ' + new Date().toLocaleTimeString('zh-CN');

    // Update banner and cards
    updateBanner(currentMetrics);
    updateCards(currentMetrics);

    // Update all charts
    updateRaftProposals(rangeData);
    updateLeaderChanges(rangeData);
    updateProposalLag(rangeData);
    updateProposalFailedRate(rangeData);
    updateWALFsync(rangeData);
    updateBackendCommit(rangeData);
    updateDBSize(rangeData);
    updateMVCCOps(rangeData);
    updatePeerTraffic(rangeData);
    updatePeerRTT(rangeData);
    updateGRPC(rangeData);
    updateGRPCTraffic(rangeData);
    updateMemory(rangeData);
    updateCPU(rangeData);
    updateGoroutines(rangeData);
    updateGC(rangeData);
    updateFDs(rangeData);
    updateMemSys(rangeData);

    // Hide loading
    document.getElementById('loading').classList.add('hidden');
}

// === Member Management ===

function updateMemberSelect(memberList, defaultID) {
    members = memberList || [];
    const select = document.getElementById('memberSelect');

    // 只在成员列表变化时更新下拉框
    const newOptions = members.map(m => m.id).sort().join(',');
    if (select.dataset.optionKeys === newOptions) return;
    select.dataset.optionKeys = newOptions;

    // 保存当前用户选择
    const previousSelection = currentMemberID;

    select.innerHTML = '';
    members.forEach(m => {
        const opt = document.createElement('option');
        opt.value = m.id;
        const label = m.name ? `${m.name} (${m.endpoint})` : m.endpoint;
        opt.textContent = label;
        select.appendChild(opt);
    });

    // 恢复用户选择：优先保留之前选中的成员
    if (previousSelection && members.some(m => m.id === previousSelection)) {
        select.value = previousSelection;
        currentMemberID = previousSelection;
    } else if (!currentMemberID) {
        // 首次加载，选择默认成员
        if (defaultID && members.some(m => m.id === defaultID)) {
            select.value = defaultID;
            currentMemberID = defaultID;
        } else if (members.length > 0) {
            select.value = members[0].id;
            currentMemberID = members[0].id;
        }
    }

    console.log(`[updateMemberSelect] members=${members.length}, currentMemberID=${currentMemberID}, select.value=${select.value}`);
}

function updateMemberCard(memberList, currentMetrics) {
    // Member Size 卡片
    document.getElementById('cardMemberSize').textContent = memberList.length;

    // 构建 tooltip - SECURE: using DOM manipulation instead of innerHTML
    const tooltip = document.getElementById('memberTooltip');
    tooltip.innerHTML = '';  // Clear safely
    
    memberList.forEach(m => {
        // 判断 leader/follower（如果当前选中的成员有 is_leader 指标）
        const dotClass = 'follower';
        const name = m.name || m.id.substring(0, 8);
        const url = m.endpoint || (m.client_urls && m.client_urls[0]) || '-';
        
        // Create DOM elements safely (no innerHTML with unsanitized data)
        const itemDiv = document.createElement('div');
        itemDiv.className = 'member-tooltip-item';
        itemDiv.onclick = () => switchToMember(m.id);
        
        const dotDiv = document.createElement('div');
        dotDiv.className = 'member-tooltip-dot ' + dotClass;
        
        const infoDiv = document.createElement('div');
        
        const nameDiv = document.createElement('div');
        nameDiv.className = 'member-tooltip-name';
        nameDiv.textContent = name;  // textContent is safe - no HTML interpretation
        
        const urlDiv = document.createElement('div');
        urlDiv.className = 'member-tooltip-url';
        urlDiv.textContent = url;  // textContent is safe - no HTML interpretation
        
        infoDiv.appendChild(nameDiv);
        infoDiv.appendChild(urlDiv);
        
        itemDiv.appendChild(dotDiv);
        itemDiv.appendChild(infoDiv);
        
        tooltip.appendChild(itemDiv);
    });
}

function onMemberChange() {
    const select = document.getElementById('memberSelect');
    currentMemberID = select.value;
    refresh();
}

function switchToMember(memberID) {
    currentMemberID = memberID;
    const select = document.getElementById('memberSelect');
    select.value = memberID;
    refresh();
}

function onTimeRangeChange() {
    currentRange = document.getElementById('timeRange').value;
    refresh();
}

function onRefreshIntervalChange() {
    resetRefreshTimer();
}

function manualRefresh() {
    const btn = document.getElementById('refreshBtn');
    btn.classList.add('spinning');
    setTimeout(() => btn.classList.remove('spinning'), 600);
    refresh();
}

function resetRefreshTimer() {
    if (refreshTimer) {
        clearInterval(refreshTimer);
        refreshTimer = null;
    }
    const interval = parseInt(document.getElementById('refreshInterval').value);
    if (interval > 0) {
        refreshTimer = setInterval(refresh, interval);
    }
}

// === Init ===
document.addEventListener('DOMContentLoaded', () => {
    applyTheme();
    initAllCharts();
    refresh();

    // 根据下拉框设置刷新频率
    resetRefreshTimer();
});
