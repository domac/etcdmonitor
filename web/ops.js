// ============================================
// Ops Panel Module - etcdmonitor
// ============================================

var opsInitialized = false;

function opsInit() {
    if (opsInitialized) return;
    opsInitialized = true;
    opsShowCards();
}

// === Helpers ===
function opsGetAuthHeaders() {
    var h = { 'Content-Type': 'application/json' };
    var token = sessionStorage.getItem('etcdmonitor_token');
    if (token) h['Authorization'] = 'Bearer ' + token;
    return h;
}

async function opsFetchJSON(url, opts) {
    var resp = await fetch(url, Object.assign({ headers: opsGetAuthHeaders() }, opts || {}));
    if (resp.status === 401) { window.location.href = '/login.html'; return null; }
    return resp;
}

function opsToast(msg, type) {
    var el = document.getElementById('opsToastEl');
    if (!el) {
        el = document.createElement('div');
        el.id = 'opsToastEl';
        el.className = 'ops-toast';
        document.body.appendChild(el);
    }
    el.textContent = msg;
    el.className = 'ops-toast ' + (type || '');
    requestAnimationFrame(function() { el.classList.add('show'); });
    setTimeout(function() { el.classList.remove('show'); }, 3000);
}

function opsConfirm(title, message) {
    return new Promise(function(resolve) {
        var overlay = document.createElement('div');
        overlay.className = 'ops-dialog-overlay';
        overlay.innerHTML =
            '<div class="ops-dialog">' +
            '<h3>' + title + '</h3>' +
            '<p>' + message + '</p>' +
            '<div class="ops-dialog-actions">' +
            '<button class="ops-btn ops-btn-secondary" id="opsDlgCancel">Cancel</button>' +
            '<button class="ops-btn ops-btn-danger" id="opsDlgConfirm">Confirm</button>' +
            '</div></div>';
        document.body.appendChild(overlay);
        overlay.querySelector('#opsDlgCancel').onclick = function() { document.body.removeChild(overlay); resolve(false); };
        overlay.querySelector('#opsDlgConfirm').onclick = function() { document.body.removeChild(overlay); resolve(true); };
        overlay.onclick = function(e) { if (e.target === overlay) { document.body.removeChild(overlay); resolve(false); } };
    });
}

async function opsGetMembers() {
    var resp = await opsFetchJSON('/api/members');
    if (!resp) return [];
    var data = await resp.json();
    return data.members || [];
}

// === Card Grid ===
function opsShowCards() {
    var container = document.getElementById('opsContainer');
    container.innerHTML =
        '<div class="ops-cards">' +
        opsCardHTML('defrag', 'D', 'Defragment', 'Online compaction to reclaim disk space. Executes follower-first for safety.') +
        opsCardHTML('snapshot', 'S', 'Snapshot', 'Download a cluster snapshot backup to your browser.') +
        opsCardHTML('alarm', 'A', 'Alarms', 'View and disarm cluster alarms (NOSPACE, CORRUPT).') +
        opsCardHTML('leader', 'L', 'Move Leader', 'Transfer leader role to a different member node.') +
        opsCardHTML('hashkv', 'H', 'HashKV Check', 'Verify data consistency across all cluster members.') +
        opsCardHTML('audit', 'R', 'Audit Log', 'View all operations history with user, time, and result.') +
        '</div>';
}

function opsCardHTML(id, icon, title, desc) {
    return '<div class="ops-card" onclick="opsOpenPanel(\'' + id + '\')">' +
        '<div class="ops-card-icon ' + id + '">' + icon + '</div>' +
        '<div class="ops-card-body"><h3>' + title + '</h3><p>' + desc + '</p></div></div>';
}

function opsOpenPanel(id) {
    var panels = {
        'defrag': opsShowDefragment,
        'snapshot': opsShowSnapshot,
        'alarm': opsShowAlarm,
        'leader': opsShowMoveLeader,
        'hashkv': opsShowHashKV,
        'audit': opsShowAuditLog
    };
    if (panels[id]) panels[id]();
}

function opsPanelHeader(title) {
    return '<div class="ops-panel-header">' +
        '<button class="ops-back-btn" onclick="opsShowCards()">&larr; Back</button>' +
        '<div class="ops-panel-title">' + title + '</div></div>';
}

// === Defragment Panel ===
async function opsShowDefragment() {
    var container = document.getElementById('opsContainer');
    container.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Defragment') +
        '<div id="opsDefragContent"><div class="ops-spinner"></div> Loading members...</div></div>';

    var members = await opsGetMembers();
    var content = document.getElementById('opsDefragContent');
    if (!members.length) { content.innerHTML = '<div class="ops-empty">No members available</div>'; return; }

    // Sort: followers first, leader last
    var sorted = members.slice().sort(function(a, b) { return (a.is_leader ? 1 : 0) - (b.is_leader ? 1 : 0); });

    var html = '<div class="ops-select-all"><label><input type="checkbox" id="opsDefragAll" onchange="opsDefragToggleAll(this.checked)"> Select All</label></div>';
    html += '<div class="ops-member-list">';
    sorted.forEach(function(m) {
        var role = m.is_leader ? 'Leader' : 'Follower';
        var roleClass = m.is_leader ? 'leader' : 'follower';
        html += '<div class="ops-member-item" data-id="' + m.id + '">' +
            '<label><input type="checkbox" class="ops-defrag-cb" value="' + m.id + '" data-leader="' + (m.is_leader ? '1' : '0') + '"> ' +
            m.name + ' <span class="ops-member-role ' + roleClass + '">' + role + '</span></label>' +
            '<span class="ops-member-status" id="opsDefragStatus_' + m.id + '"></span></div>';
    });
    html += '</div>';
    html += '<button class="ops-btn ops-btn-primary" id="opsDefragBtn" onclick="opsExecDefragment()">Execute Defragment</button>';
    content.innerHTML = html;
}

function opsDefragToggleAll(checked) {
    document.querySelectorAll('.ops-defrag-cb').forEach(function(cb) { cb.checked = checked; });
}

async function opsExecDefragment() {
    var cbs = document.querySelectorAll('.ops-defrag-cb:checked');
    if (!cbs.length) { opsToast('Please select at least one member', 'error'); return; }

    // Sort: followers first, leaders last
    var ids = Array.from(cbs).sort(function(a, b) {
        return parseInt(a.dataset.leader) - parseInt(b.dataset.leader);
    }).map(function(cb) { return cb.value; });

    var confirmed = await opsConfirm('Confirm Defragment',
        'Defragment will make each node briefly unavailable.<br>Execution order: Followers first, Leader last.<br><br>Proceed with ' + ids.length + ' node(s)?');
    if (!confirmed) return;

    var btn = document.getElementById('opsDefragBtn');
    btn.disabled = true;
    btn.textContent = 'Executing...';
    document.querySelectorAll('.ops-defrag-cb').forEach(function(cb) { cb.disabled = true; });

    for (var i = 0; i < ids.length; i++) {
        var id = ids[i];
        var statusEl = document.getElementById('opsDefragStatus_' + id);
        statusEl.innerHTML = '<span class="ops-spinner"></span>';

        try {
            var resp = await opsFetchJSON('/api/ops/defragment', {
                method: 'POST',
                body: JSON.stringify({ member_id: id })
            });
            var data = await resp.json();
            if (resp.ok) {
                statusEl.textContent = '\u2713';
                statusEl.style.color = 'var(--accent-green)';
                statusEl.title = 'Done in ' + data.duration_ms + 'ms';
            } else {
                statusEl.textContent = '\u2717';
                statusEl.style.color = 'var(--accent-red)';
                statusEl.title = data.error || 'Failed';
            }
        } catch (e) {
            statusEl.textContent = '\u2717';
            statusEl.style.color = 'var(--accent-red)';
            statusEl.title = e.message;
        }
    }

    btn.textContent = 'Completed';
    opsToast('Defragment completed', 'success');
}

// === Snapshot Panel ===
async function opsShowSnapshot() {
    var container = document.getElementById('opsContainer');
    container.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Snapshot Backup') +
        '<div id="opsSnapContent"><div class="ops-spinner"></div> Loading...</div></div>';

    var members = await opsGetMembers();
    var content = document.getElementById('opsSnapContent');

    var html = '<div class="ops-info"><div class="ops-info-row"><span class="ops-info-label">Source Node</span>' +
        '<select class="ops-filter-select" id="opsSnapMember">';
    members.forEach(function(m) {
        html += '<option value="' + m.id + '">' + m.name + (m.is_leader ? ' (Leader)' : '') + '</option>';
    });
    html += '</select></div></div>';
    html += '<p style="font-size:13px;color:var(--text-secondary);margin-bottom:16px">Snapshot will be streamed directly to your browser. No temporary files are created on the server.</p>';
    html += '<button class="ops-btn ops-btn-primary" id="opsSnapBtn" onclick="opsExecSnapshot()">Create Snapshot</button>';
    html += '<div id="opsSnapStatus" style="margin-top:12px"></div>';
    content.innerHTML = html;
}

async function opsExecSnapshot() {
    var memberID = document.getElementById('opsSnapMember').value;
    var btn = document.getElementById('opsSnapBtn');
    var status = document.getElementById('opsSnapStatus');
    btn.disabled = true;
    btn.textContent = 'Creating...';
    status.innerHTML = '<span class="ops-spinner"></span> Downloading snapshot...';

    try {
        var resp = await fetch('/api/ops/snapshot?member_id=' + memberID, { headers: opsGetAuthHeaders() });
        if (!resp.ok) {
            var err = await resp.json();
            throw new Error(err.error || 'Snapshot failed');
        }
        var blob = await resp.blob();
        var cd = resp.headers.get('Content-Disposition') || '';
        var fname = 'etcd-snapshot.db';
        var match = cd.match(/filename="(.+)"/);
        if (match) fname = match[1];

        var url = URL.createObjectURL(blob);
        var a = document.createElement('a');
        a.href = url; a.download = fname;
        document.body.appendChild(a); a.click(); document.body.removeChild(a);
        URL.revokeObjectURL(url);

        status.innerHTML = '<div class="ops-result success">Snapshot downloaded: ' + fname + ' (' + (blob.size / 1024 / 1024).toFixed(2) + ' MB)</div>';
        opsToast('Snapshot downloaded', 'success');
    } catch (e) {
        status.innerHTML = '<div class="ops-result error">Failed: ' + e.message + '</div>';
        opsToast('Snapshot failed', 'error');
    }
    btn.disabled = false;
    btn.textContent = 'Create Snapshot';
}

// === Alarm Panel ===
async function opsShowAlarm() {
    var container = document.getElementById('opsContainer');
    container.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Cluster Alarms') +
        '<div id="opsAlarmContent"><div class="ops-spinner"></div> Loading...</div></div>';
    await opsRefreshAlarms();
}

async function opsRefreshAlarms() {
    var content = document.getElementById('opsAlarmContent');
    try {
        var resp = await opsFetchJSON('/api/ops/alarms');
        var data = await resp.json();
        var alarms = data.alarms || [];

        if (!alarms.length) {
            content.innerHTML = '<div class="ops-empty"><div class="ops-empty-icon">\u2705</div>Cluster is healthy. No active alarms.</div>';
            return;
        }

        var html = '<table class="ops-table"><thead><tr><th>Type</th><th>Member</th><th>Action</th></tr></thead><tbody>';
        alarms.forEach(function(a) {
            html += '<tr><td><strong>' + a.alarm_type + '</strong></td><td>' + a.member_name + ' (' + a.member_id + ')</td>' +
                '<td><button class="ops-btn ops-btn-danger" style="padding:4px 10px;font-size:12px" ' +
                'onclick="opsDisarmAlarm(\'' + a.member_id + '\',\'' + a.alarm_type + '\')">Disarm</button></td></tr>';
        });
        html += '</tbody></table>';
        content.innerHTML = html;
    } catch (e) {
        content.innerHTML = '<div class="ops-result error">Failed to load alarms: ' + e.message + '</div>';
    }
}

async function opsDisarmAlarm(memberID, alarmType) {
    var confirmed = await opsConfirm('Disarm Alarm', 'Disarm <strong>' + alarmType + '</strong> alarm on member ' + memberID + '?<br><br>Make sure the root cause has been resolved before disarming.');
    if (!confirmed) return;

    try {
        var resp = await opsFetchJSON('/api/ops/alarms/disarm', {
            method: 'POST',
            body: JSON.stringify({ member_id: memberID, alarm_type: alarmType })
        });
        if (resp.ok) {
            opsToast('Alarm disarmed', 'success');
            await opsRefreshAlarms();
        } else {
            var data = await resp.json();
            opsToast(data.error || 'Disarm failed', 'error');
        }
    } catch (e) {
        opsToast('Disarm failed: ' + e.message, 'error');
    }
}

// === Move Leader Panel ===
async function opsShowMoveLeader() {
    var container = document.getElementById('opsContainer');
    container.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Move Leader') +
        '<div id="opsLeaderContent"><div class="ops-spinner"></div> Loading...</div></div>';

    var members = await opsGetMembers();
    var content = document.getElementById('opsLeaderContent');
    var leader = members.find(function(m) { return m.is_leader; });
    var followers = members.filter(function(m) { return !m.is_leader; });

    if (members.length <= 1) {
        content.innerHTML = '<div class="ops-empty"><div class="ops-empty-icon">\u2139\ufe0f</div>Single-node cluster. Leader migration is not applicable.</div>';
        return;
    }

    var html = '<div class="ops-info">' +
        '<div class="ops-info-row"><span class="ops-info-label">Current Leader</span><span class="ops-info-value">' +
        (leader ? leader.name + ' (' + leader.id + ')' : 'Unknown') + '</span></div></div>';

    html += '<div style="margin-bottom:16px"><label style="font-size:13px;color:var(--text-secondary);font-weight:600;display:block;margin-bottom:6px">Target Node</label>' +
        '<select class="ops-filter-select" id="opsLeaderTarget" style="min-width:200px">';
    followers.forEach(function(m) {
        html += '<option value="' + m.id + '">' + m.name + '</option>';
    });
    html += '</select></div>';
    html += '<button class="ops-btn ops-btn-primary" onclick="opsExecMoveLeader()">Move Leader</button>';
    html += '<div id="opsLeaderStatus" style="margin-top:12px"></div>';
    content.innerHTML = html;
}

async function opsExecMoveLeader() {
    var targetID = document.getElementById('opsLeaderTarget').value;
    var targetName = document.getElementById('opsLeaderTarget').selectedOptions[0].textContent;

    var confirmed = await opsConfirm('Confirm Move Leader',
        'Transfer leader role to <strong>' + targetName + '</strong>?<br><br>This may cause brief client reconnections.');
    if (!confirmed) return;

    var status = document.getElementById('opsLeaderStatus');
    status.innerHTML = '<span class="ops-spinner"></span> Moving leader...';

    try {
        var resp = await opsFetchJSON('/api/ops/move-leader', {
            method: 'POST',
            body: JSON.stringify({ target_member_id: targetID })
        });
        var data = await resp.json();
        if (resp.ok) {
            status.innerHTML = '<div class="ops-result success">Leader moved to ' + data.target_name + ' in ' + data.duration_ms + 'ms</div>';
            opsToast('Leader moved successfully', 'success');
        } else {
            status.innerHTML = '<div class="ops-result error">Failed: ' + data.error + '</div>';
            opsToast('Move leader failed', 'error');
        }
    } catch (e) {
        status.innerHTML = '<div class="ops-result error">Failed: ' + e.message + '</div>';
    }
}

// === HashKV Panel ===
function opsShowHashKV() {
    var container = document.getElementById('opsContainer');
    container.innerHTML = '<div class="ops-panel">' + opsPanelHeader('HashKV Consistency Check') +
        '<p style="font-size:13px;color:var(--text-secondary);margin-bottom:16px">Compares data hash across all members at the same revision to detect inconsistencies.</p>' +
        '<button class="ops-btn ops-btn-primary" id="opsHashBtn" onclick="opsExecHashKV()">Run Consistency Check</button>' +
        '<div id="opsHashResult" style="margin-top:16px"></div></div>';
}

async function opsExecHashKV() {
    var btn = document.getElementById('opsHashBtn');
    var result = document.getElementById('opsHashResult');
    btn.disabled = true;
    btn.textContent = 'Checking...';
    result.innerHTML = '<span class="ops-spinner"></span> Running HashKV on all members...';

    try {
        var resp = await opsFetchJSON('/api/ops/hashkv', { method: 'POST' });
        var data = await resp.json();

        var banner = data.consistent
            ? '<div class="ops-result success">\u2713 Data is consistent across all members (revision: ' + data.revision + ', ' + data.duration_ms + 'ms)</div>'
            : '<div class="ops-result error">\u2717 DATA INCONSISTENCY DETECTED (revision: ' + data.revision + ')</div>';

        var html = banner + '<table class="ops-table"><thead><tr><th>Member</th><th>Revision</th><th>Hash</th><th>Status</th></tr></thead><tbody>';
        var refHash = null;
        (data.results || []).forEach(function(r) {
            if (!r.error && refHash === null) refHash = r.hash;
            var status, statusColor;
            if (r.error) {
                status = 'Unreachable'; statusColor = 'var(--text-secondary)';
            } else if (r.hash === refHash) {
                status = '\u2713'; statusColor = 'var(--accent-green)';
            } else {
                status = '\u2717'; statusColor = 'var(--accent-red)';
            }
            html += '<tr><td>' + r.member_name + '</td><td>' + r.revision + '</td>' +
                '<td><code>' + (r.error ? '-' : r.hash) + '</code></td>' +
                '<td style="color:' + statusColor + ';font-weight:600">' + status + (r.error ? ' (' + r.error + ')' : '') + '</td></tr>';
        });
        html += '</tbody></table>';
        result.innerHTML = html;
        opsToast(data.consistent ? 'Data consistent' : 'Inconsistency detected!', data.consistent ? 'success' : 'error');
    } catch (e) {
        result.innerHTML = '<div class="ops-result error">Failed: ' + e.message + '</div>';
    }
    btn.disabled = false;
    btn.textContent = 'Run Consistency Check';
}

// === Audit Log Panel ===
var opsAuditPage = 1;
var opsAuditFilter = '';

function opsShowAuditLog() {
    var container = document.getElementById('opsContainer');
    container.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Audit Log') +
        '<div class="ops-filter-bar">' +
        '<select class="ops-filter-select" id="opsAuditFilter" onchange="opsAuditFilterChange(this.value)">' +
        '<option value="">All Operations</option>' +
        '<option value="defragment">Defragment</option>' +
        '<option value="snapshot">Snapshot</option>' +
        '<option value="alarm_disarm">Alarm Disarm</option>' +
        '<option value="move_leader">Move Leader</option>' +
        '<option value="hashkv">HashKV</option></select></div>' +
        '<div id="opsAuditContent"><div class="ops-spinner"></div> Loading...</div></div>';
    opsAuditPage = 1;
    opsAuditFilter = '';
    opsLoadAuditLogs();
}

function opsAuditFilterChange(val) {
    opsAuditFilter = val;
    opsAuditPage = 1;
    opsLoadAuditLogs();
}

async function opsLoadAuditLogs() {
    var content = document.getElementById('opsAuditContent');
    var url = '/api/ops/audit-logs?page=' + opsAuditPage + '&page_size=15';
    if (opsAuditFilter) url += '&operation=' + opsAuditFilter;

    try {
        var resp = await opsFetchJSON(url);
        var data = await resp.json();
        var entries = data.entries || [];
        var total = data.total || 0;
        var pageSize = data.page_size || 15;
        var totalPages = Math.ceil(total / pageSize) || 1;

        if (!entries.length) {
            content.innerHTML = '<div class="ops-empty"><div class="ops-empty-icon">\ud83d\udcdd</div>No audit log entries found.</div>';
            return;
        }

        var html = '<table class="ops-table"><thead><tr><th>Time</th><th>User</th><th>Operation</th><th>Target</th><th>Result</th><th>Duration</th></tr></thead><tbody>';
        entries.forEach(function(e) {
            var dt = new Date(e.timestamp * 1000);
            var timeStr = dt.toLocaleDateString() + ' ' + dt.toLocaleTimeString();
            var resultClass = e.success ? 'color:var(--accent-green)' : 'color:var(--accent-red)';
            var resultText = e.success ? '\u2713 ' + (e.result || 'success') : '\u2717 ' + (e.result || 'failed');
            if (resultText.length > 60) resultText = resultText.substring(0, 60) + '...';
            html += '<tr><td style="white-space:nowrap">' + timeStr + '</td><td>' + (e.username || '-') + '</td>' +
                '<td><code>' + e.operation + '</code></td><td>' + (e.target || '-') + '</td>' +
                '<td style="' + resultClass + ';font-size:12px" title="' + (e.result || '').replace(/"/g, '&quot;') + '">' + resultText + '</td>' +
                '<td>' + e.duration_ms + 'ms</td></tr>';
        });
        html += '</tbody></table>';

        html += '<div class="ops-pagination">' +
            '<button onclick="opsAuditPage--;opsLoadAuditLogs()"' + (opsAuditPage <= 1 ? ' disabled' : '') + '>&laquo; Prev</button>' +
            '<span>Page ' + opsAuditPage + ' / ' + totalPages + ' (' + total + ' total)</span>' +
            '<button onclick="opsAuditPage++;opsLoadAuditLogs()"' + (opsAuditPage >= totalPages ? ' disabled' : '') + '>Next &raquo;</button></div>';

        content.innerHTML = html;
    } catch (e) {
        content.innerHTML = '<div class="ops-result error">Failed to load audit logs: ' + e.message + '</div>';
    }
}
