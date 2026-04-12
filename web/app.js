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
let currentPanelConfig = null; // 当前面板配置

// === Panel Registry ===
// 每个面板的元数据：ID、显示名称、所属分区、默认顺序
const PANEL_REGISTRY = [
    { id: 'chartRaftProposals',    name: 'Raft Proposals',               section: 'raft',    order: 0 },
    { id: 'chartLeaderChanges',    name: 'Leader Changes & Slow Ops',    section: 'raft',    order: 1 },
    { id: 'chartProposalLag',      name: 'Proposal Commit-Apply Lag',    section: 'raft',    order: 2 },
    { id: 'chartProposalFailedRate', name: 'Proposal Failed Rate',       section: 'raft',    order: 3 },
    { id: 'chartWALFsync',        name: 'WAL Fsync Duration',            section: 'disk',    order: 4 },
    { id: 'chartBackendCommit',    name: 'Backend Commit Duration',      section: 'disk',    order: 5 },
    { id: 'chartDBSize',          name: 'Database Size',                  section: 'storage', order: 6 },
    { id: 'chartMVCCOps',         name: 'MVCC Operations',                section: 'storage', order: 7 },
    { id: 'chartPeerTraffic',     name: 'Peer Network Traffic',           section: 'network', order: 8 },
    { id: 'chartPeerRTT',         name: 'Peer Round Trip Time',           section: 'network', order: 9 },
    { id: 'chartGRPC',            name: 'gRPC Request Rate',              section: 'grpc',    order: 10 },
    { id: 'chartGRPCTraffic',     name: 'gRPC Client Traffic',            section: 'grpc',    order: 11 },
    { id: 'chartCPU',             name: 'CPU Usage',                      section: 'runtime', order: 12 },
    { id: 'chartMemory',          name: 'Memory',                         section: 'runtime', order: 13 },
    { id: 'chartGoroutines',      name: 'Goroutines',                     section: 'runtime', order: 14 },
    { id: 'chartGC',              name: 'GC Duration',                    section: 'runtime', order: 15 },
    { id: 'chartFDs',             name: 'File Descriptors',               section: 'runtime', order: 16 },
    { id: 'chartMemSys',          name: 'Memory Sys',                     section: 'runtime', order: 17 },
];

const SECTION_META = {
    raft:    { label: 'Raft & Server',     icon: 'R', cssClass: 'server' },
    disk:    { label: 'Disk Performance',  icon: 'D', cssClass: 'disk' },
    storage: { label: 'MVCC & Storage',    icon: 'S', cssClass: 'storage' },
    network: { label: 'Network & Peers',   icon: 'N', cssClass: 'network' },
    grpc:    { label: 'gRPC Requests',     icon: 'G', cssClass: 'grpc' },
    runtime: { label: 'Process & Runtime', icon: 'P', cssClass: 'runtime' },
};

// panelID → registry entry 快速查找
const PANEL_MAP = {};
PANEL_REGISTRY.forEach(p => { PANEL_MAP[p.id] = p; });

// 默认面板配置
function defaultPanelConfig() {
    return {
        panels: PANEL_REGISTRY.map(p => ({ id: p.id, visible: true, order: p.order })),
        version: 1
    };
}

// 加载面板配置：认证模式走 API，免认证走 localStorage
async function loadPanelConfig() {
    if (authRequired) {
        try {
            var resp = await fetchWithAuth('/api/user/panel-config');
            if (resp && resp.ok) {
                var cfg = await resp.json();
                if (cfg && cfg.panels && cfg.panels.length > 0) {
                    return mergePanelConfig(cfg);
                }
            }
        } catch (e) {
            console.warn('loadPanelConfig API error, using default:', e);
        }
        return defaultPanelConfig();
    } else {
        try {
            var raw = localStorage.getItem('etcdmonitor-panel-config');
            if (raw) {
                var cfg = JSON.parse(raw);
                if (cfg && cfg.panels && cfg.panels.length > 0) {
                    return mergePanelConfig(cfg);
                }
            }
        } catch (e) {
            console.warn('loadPanelConfig localStorage error, using default:', e);
        }
        return defaultPanelConfig();
    }
}

// 保存面板配置：认证模式走 API，免认证走 localStorage
async function savePanelConfig(config) {
    if (authRequired) {
        try {
            await fetchWithAuth('/api/user/panel-config', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(config)
            });
        } catch (e) {
            console.error('savePanelConfig API error:', e);
        }
    } else {
        try {
            localStorage.setItem('etcdmonitor-panel-config', JSON.stringify(config));
        } catch (e) {
            console.error('savePanelConfig localStorage error:', e);
        }
    }
}

// 合并配置：过滤无效 ID，补充缺失的新面板
function mergePanelConfig(cfg) {
    var validIds = {};
    PANEL_REGISTRY.forEach(function(p) { validIds[p.id] = true; });

    var seen = {};
    var panels = [];
    // 保留用户已有的合法面板
    (cfg.panels || []).forEach(function(p) {
        if (validIds[p.id] && !seen[p.id]) {
            seen[p.id] = true;
            panels.push(p);
        }
    });
    // 追加缺失的面板
    var nextOrder = panels.length;
    PANEL_REGISTRY.forEach(function(p) {
        if (!seen[p.id]) {
            panels.push({ id: p.id, visible: true, order: nextOrder++ });
        }
    });
    return { panels: panels, version: cfg.version || 1 };
}

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
    // 不要重复初始化
    if (charts[id]) return charts[id];
    const chart = echarts.init(dom, null, { renderer: 'canvas' });
    charts[id] = chart;
    return chart;
}

function initAllCharts() {
    // 仅对可见面板初始化 ECharts
    var visibleIds = getVisiblePanelIds();
    visibleIds.forEach(id => initChart(id));

    // Resize handling（只绑定一次）
    if (!window._resizeBound) {
        window.addEventListener('resize', () => {
            Object.values(charts).forEach(c => c && c.resize());
        });
        window._resizeBound = true;
    }
}

// 获取当前配置中可见的面板 ID 列表
function getVisiblePanelIds() {
    if (!currentPanelConfig || !currentPanelConfig.panels) {
        return PANEL_REGISTRY.map(p => p.id);
    }
    return currentPanelConfig.panels
        .filter(p => p.visible)
        .map(p => p.id);
}

// 渲染面板：根据配置控制显示/隐藏和排序
function renderPanels(config) {
    if (!config || !config.panels) return;

    // 构建面板可见性和排序映射
    var panelVisible = {};
    var panelOrder = {};
    config.panels.forEach(function(p) {
        panelVisible[p.id] = p.visible;
        panelOrder[p.id] = p.order;
    });

    // 按分区处理
    var sections = ['raft', 'disk', 'storage', 'network', 'grpc', 'runtime'];
    sections.forEach(function(section) {
        var grid = document.querySelector('.panel-grid[data-section="' + section + '"]');
        var header = document.querySelector('.section-header[data-section="' + section + '"]');
        if (!grid || !header) return;

        // 获取该分区内的面板元素
        var panelEls = Array.from(grid.querySelectorAll('.panel[data-panel-id]'));
        var hasVisible = false;

        // 按配置排序面板
        panelEls.sort(function(a, b) {
            var oa = panelOrder[a.dataset.panelId] !== undefined ? panelOrder[a.dataset.panelId] : 999;
            var ob = panelOrder[b.dataset.panelId] !== undefined ? panelOrder[b.dataset.panelId] : 999;
            return oa - ob;
        });

        // 重新排列 DOM 并控制可见性
        panelEls.forEach(function(el) {
            var id = el.dataset.panelId;
            var visible = panelVisible[id] !== false; // 默认可见
            el.style.display = visible ? '' : 'none';
            if (visible) hasVisible = true;
            // 重新插入 DOM 以反映排序
            grid.appendChild(el);
        });

        // 分区内全部面板隐藏时隐藏分区标题
        header.style.display = hasVisible ? '' : 'none';
        grid.style.display = hasVisible ? '' : 'none';
    });

    // 对可见面板初始化 ECharts（如果还没初始化）；对隐藏面板释放 ECharts
    config.panels.forEach(function(p) {
        if (p.visible) {
            if (!charts[p.id]) {
                initChart(p.id);
            }
        } else {
            if (charts[p.id]) {
                charts[p.id].dispose();
                delete charts[p.id];
            }
        }
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

// getAuthHeaders 获取认证请求头
function getAuthHeaders() {
    const token = sessionStorage.getItem('etcdmonitor_token');
    if (token) {
        return { 'Authorization': 'Bearer ' + token };
    }
    return {};
}

// fetchWithAuth 包装 fetch，带认证 header，401 时自动跳转登录页
async function fetchWithAuth(url, options) {
    try {
        const headers = { ...getAuthHeaders(), ...(options && options.headers || {}) };
        const resp = await fetch(url, { ...options, headers });
        if (resp.status === 401) {
            sessionStorage.removeItem('etcdmonitor_token');
            window.location.href = '/login.html';
            return null;
        }
        return resp;
    } catch (e) {
        console.error('fetchWithAuth error:', e);
        return null;
    }
}

async function fetchCurrent() {
    try {
        const memberParam = currentMemberID ? `?member_id=${currentMemberID}` : '';
        const resp = await fetchWithAuth(`/api/current${memberParam}`);
        if (!resp) return null;
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
        const resp = await fetchWithAuth(url);
        if (!resp) return {};
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
        const resp = await fetchWithAuth('/api/status');
        if (!resp) return null;
        return await resp.json();
    } catch (e) {
        return null;
    }
}

async function fetchMembers() {
    try {
        const resp = await fetchWithAuth('/api/members');
        if (!resp) return null;
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

// === Auth State ===
let authRequired = false;

// === Init ===
document.addEventListener('DOMContentLoaded', async () => {
    applyTheme();

    // 检查认证状态
    try {
        const authResp = await fetch('/api/auth/status', { headers: getAuthHeaders() });
        if (!authResp.ok) {
            console.error('Auth status check failed with status:', authResp.status);
            showLoadingError('认证服务异常 (HTTP ' + authResp.status + ')，请刷新重试');
            return;
        }
        const authData = await authResp.json();
        if (authData.auth_required && !authData.authenticated) {
            hideLoading();
            window.location.href = '/login.html';
            return;
        }
        authRequired = !!authData.auth_required;
    } catch (e) {
        console.error('Auth status check failed:', e);
        showLoadingError('无法连接服务，请检查网络后刷新');
        return;
    }

    // 显示/隐藏登出按钮
    const logoutBtn = document.getElementById('logoutBtn');
    if (logoutBtn) {
        logoutBtn.style.display = authRequired ? '' : 'none';
    }

    // 加载面板配置并渲染
    currentPanelConfig = await loadPanelConfig();
    renderPanels(currentPanelConfig);
    initAllCharts();

    refresh();
    resetRefreshTimer();
});

// === Loading Helpers ===
function hideLoading() {
    const el = document.getElementById('loading');
    if (el) el.classList.add('hidden');
}

function showLoadingError(msg) {
    const textEl = document.querySelector('.loading-text');
    if (textEl) textEl.textContent = msg;
    // 停止 spinner 动画
    const spinner = document.querySelector('.loading-spinner');
    if (spinner) spinner.style.display = 'none';
}

// === Panel Config Modal ===
var _panelConfigDraft = null; // 编辑中的临时配置

function openPanelConfig() {
    // 创建编辑副本
    _panelConfigDraft = JSON.parse(JSON.stringify(currentPanelConfig || defaultPanelConfig()));
    buildPanelConfigList();
    document.getElementById('panelConfigModal').style.display = '';
    document.addEventListener('keydown', _panelConfigEscHandler);
}

function closePanelConfig() {
    document.getElementById('panelConfigModal').style.display = 'none';
    document.removeEventListener('keydown', _panelConfigEscHandler);
    _panelConfigDraft = null;
}

function _panelConfigEscHandler(e) {
    if (e.key === 'Escape') closePanelConfig();
}

function buildPanelConfigList() {
    var container = document.getElementById('panelConfigList');
    container.innerHTML = '';

    if (!_panelConfigDraft || !_panelConfigDraft.panels) return;

    // 按分区分组
    var sectionOrder = ['raft', 'disk', 'storage', 'network', 'grpc', 'runtime'];
    var grouped = {};
    sectionOrder.forEach(function(s) { grouped[s] = []; });

    _panelConfigDraft.panels.forEach(function(p, idx) {
        var reg = PANEL_MAP[p.id];
        if (!reg) return;
        grouped[reg.section].push({ panel: p, index: idx, reg: reg });
    });

    sectionOrder.forEach(function(section) {
        var items = grouped[section];
        if (items.length === 0) return;
        var meta = SECTION_META[section];

        // Section label
        var label = document.createElement('div');
        label.className = 'config-section-label';
        label.textContent = meta.label;
        container.appendChild(label);

        // Section container for drag scope
        var sectionDiv = document.createElement('div');
        sectionDiv.dataset.configSection = section;
        container.appendChild(sectionDiv);

        // Sort items by order within this section
        items.sort(function(a, b) { return a.panel.order - b.panel.order; });

        items.forEach(function(item) {
            var row = document.createElement('div');
            row.className = 'config-panel-item';
            row.draggable = true;
            row.dataset.panelId = item.panel.id;
            row.dataset.section = section;

            // Drag handle
            var handle = document.createElement('span');
            handle.className = 'config-drag-handle';
            handle.textContent = '\u22EE\u22EE';
            row.appendChild(handle);

            // Checkbox
            var cb = document.createElement('input');
            cb.type = 'checkbox';
            cb.checked = item.panel.visible;
            cb.onchange = function() {
                // Update draft
                var p = findDraftPanel(item.panel.id);
                if (p) p.visible = cb.checked;
            };
            row.appendChild(cb);

            // Panel name
            var nameSpan = document.createElement('span');
            nameSpan.className = 'config-panel-name';
            nameSpan.textContent = item.reg.name;
            row.appendChild(nameSpan);

            // Section tag
            var tag = document.createElement('span');
            tag.className = 'config-panel-section-tag';
            tag.textContent = meta.label;
            row.appendChild(tag);

            // Drag events
            row.addEventListener('dragstart', onDragStart);
            row.addEventListener('dragend', onDragEnd);
            row.addEventListener('dragover', onDragOver);
            row.addEventListener('drop', onDrop);
            row.addEventListener('dragleave', onDragLeave);

            sectionDiv.appendChild(row);
        });
    });
}

var _dragSrcEl = null;

function onDragStart(e) {
    _dragSrcEl = this;
    this.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', this.dataset.panelId);
}

function onDragEnd(e) {
    this.classList.remove('dragging');
    // Remove all drag-over indicators
    document.querySelectorAll('.config-panel-item.drag-over').forEach(function(el) {
        el.classList.remove('drag-over');
    });
    _dragSrcEl = null;
}

function onDragOver(e) {
    e.preventDefault();
    if (!_dragSrcEl) return;
    // 禁止跨分区拖拽
    if (this.dataset.section !== _dragSrcEl.dataset.section) {
        e.dataTransfer.dropEffect = 'none';
        return;
    }
    e.dataTransfer.dropEffect = 'move';
    this.classList.add('drag-over');
}

function onDragLeave(e) {
    this.classList.remove('drag-over');
}

function onDrop(e) {
    e.preventDefault();
    this.classList.remove('drag-over');
    if (!_dragSrcEl || _dragSrcEl === this) return;
    // 禁止跨分区
    if (this.dataset.section !== _dragSrcEl.dataset.section) return;

    // DOM reorder
    var parent = this.parentNode;
    var allItems = Array.from(parent.querySelectorAll('.config-panel-item'));
    var srcIdx = allItems.indexOf(_dragSrcEl);
    var tgtIdx = allItems.indexOf(this);

    if (srcIdx < tgtIdx) {
        parent.insertBefore(_dragSrcEl, this.nextSibling);
    } else {
        parent.insertBefore(_dragSrcEl, this);
    }

    // Update draft order for this section
    updateDraftOrderFromDOM(this.dataset.section);
}

function updateDraftOrderFromDOM(section) {
    var sectionDiv = document.querySelector('[data-config-section="' + section + '"]');
    if (!sectionDiv) return;
    var items = Array.from(sectionDiv.querySelectorAll('.config-panel-item'));
    items.forEach(function(el, idx) {
        var p = findDraftPanel(el.dataset.panelId);
        if (p) p.order = idx;
    });
}

function findDraftPanel(id) {
    if (!_panelConfigDraft) return null;
    return _panelConfigDraft.panels.find(function(p) { return p.id === id; });
}

async function savePanelConfigUI() {
    if (!_panelConfigDraft) return;

    // Collect final state from DOM (checkbox states + order)
    var sections = ['raft', 'disk', 'storage', 'network', 'grpc', 'runtime'];
    var globalOrder = 0;
    sections.forEach(function(section) {
        var sectionDiv = document.querySelector('[data-config-section="' + section + '"]');
        if (!sectionDiv) return;
        var items = Array.from(sectionDiv.querySelectorAll('.config-panel-item'));
        items.forEach(function(el) {
            var p = findDraftPanel(el.dataset.panelId);
            if (p) {
                var cb = el.querySelector('input[type="checkbox"]');
                p.visible = cb ? cb.checked : true;
                p.order = globalOrder++;
            }
        });
    });

    currentPanelConfig = JSON.parse(JSON.stringify(_panelConfigDraft));
    await savePanelConfig(currentPanelConfig);
    renderPanels(currentPanelConfig);
    closePanelConfig();
    // 触发一次刷新以确保新可见面板有数据
    refresh();
}

function resetPanelConfigUI() {
    _panelConfigDraft = defaultPanelConfig();
    buildPanelConfigList();
}

// === Logout ===
async function logout() {
    try {
        await fetch('/api/auth/logout', { method: 'POST', headers: getAuthHeaders() });
    } catch (e) {
        // ignore
    }
    sessionStorage.removeItem('etcdmonitor_token');
    window.location.href = '/login.html';
}
