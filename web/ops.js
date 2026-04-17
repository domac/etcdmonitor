// ============================================
// Ops Panel Module - etcdmonitor
// ============================================

var opsInitialized = false;
var opsActivePanel = '';
var opsVersion = '-';

function opsInit() {
    if (opsInitialized) return;
    opsInitialized = true;
    opsRenderLayout();
    opsLoadVersion();
    opsSelectPanel('audit');
}

// === Menu Configuration ===
var opsMenuItems = [
    { id: 'audit', label: 'Audit Log' },
    { id: 'defrag', label: 'Defragment' },
    { id: 'snapshot', label: 'Snapshot' },
    { id: 'alarm', label: 'Alarms' },
    { id: 'leader', label: 'Move Leader' },
    { id: 'hashkv', label: 'HashKV Check' },
    { id: 'compact', label: 'Compact' }
];

// === Layout ===
function opsRenderLayout() {
    var container = document.getElementById('opsContainer');
    var sidebarHTML = '<div class="ops-sidebar">';
    opsMenuItems.forEach(function(item) {
        sidebarHTML += '<button class="ops-sidebar-item" data-panel="' + item.id + '" onclick="opsSelectPanel(\'' + item.id + '\')">' + item.label + '</button>';
    });
    sidebarHTML += '</div>';
    container.innerHTML = '<div class="ops-layout">' + sidebarHTML + '<div class="ops-content" id="opsContentPanel"></div></div>';
}

function opsSelectPanel(id) {
    opsActivePanel = id;
    // Update active state
    var items = document.querySelectorAll('.ops-sidebar-item');
    items.forEach(function(el) {
        if (el.dataset.panel === id) {
            el.classList.add('active');
        } else {
            el.classList.remove('active');
        }
    });
    // Render panel
    var panels = {
        'defrag': opsShowDefragment,
        'snapshot': opsShowSnapshot,
        'alarm': opsShowAlarm,
        'leader': opsShowMoveLeader,
        'hashkv': opsShowHashKV,
        'compact': opsShowCompact,
        'audit': opsShowAuditLog
    };
    if (panels[id]) panels[id]();
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


// === Version Management ===
async function opsGetStatus() {
    var resp = await opsFetchJSON('/api/status');
    if (!resp) return { etcd_version: '-' };
    var data = await resp.json();
    return data || { etcd_version: '-' };
}

async function opsLoadVersion() {
    var status = await opsGetStatus();
    opsVersion = (status && status.etcd_version) ? status.etcd_version : '-';
    // Update version display in the ops info bar
    var versionEl = document.getElementById('opsVersion');
    if (versionEl) {
        versionEl.textContent = opsVersion;
    }
}

async function opsGetMembers() {
    var resp = await opsFetchJSON('/api/members');
    if (!resp) return [];
    var data = await resp.json();
    return data.members || [];
}

function opsPanelHeader(title) {
    return '<div class="ops-panel-header">' +
        '<div class="ops-panel-title">' + title + '</div>' +
        '</div>';
}

// === Defragment Panel ===
async function opsShowDefragment() {
    var content = document.getElementById('opsContentPanel');
    content.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Defragment') +
        '<div id="opsDefragContent"><div class="ops-spinner"></div> Loading members...</div></div>';

    var members = await opsGetMembers();
    var el = document.getElementById('opsDefragContent');
    if (!members.length) { el.innerHTML = '<div class="ops-empty">No members available</div>'; return; }

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
    el.innerHTML = html;
}

function opsDefragToggleAll(checked) {
    document.querySelectorAll('.ops-defrag-cb').forEach(function(cb) { cb.checked = checked; });
}

async function opsExecDefragment() {
    var cbs = document.querySelectorAll('.ops-defrag-cb:checked');
    if (!cbs.length) { opsToast('Please select at least one member', 'error'); return; }

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
    var content = document.getElementById('opsContentPanel');
    content.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Snapshot Backup') +
        '<div id="opsSnapContent"><div class="ops-spinner"></div> Loading...</div></div>';

    var members = await opsGetMembers();
    var el = document.getElementById('opsSnapContent');

    var html = '<div class="ops-info"><div class="ops-info-row"><span class="ops-info-label">Source Node</span>' +
        '<select class="ops-filter-select" id="opsSnapMember">';
    members.forEach(function(m) {
        html += '<option value="' + m.id + '">' + m.name + (m.is_leader ? ' (Leader)' : '') + '</option>';
    });
    html += '</select></div></div>';
    html += '<p style="font-size:13px;color:var(--text-secondary);margin-bottom:16px">Snapshot will be streamed directly to your browser. No temporary files are created on the server.</p>';
    html += '<button class="ops-btn ops-btn-primary" id="opsSnapBtn" onclick="opsExecSnapshot()">Create Snapshot</button>';
    html += '<div id="opsSnapStatus" style="margin-top:12px"></div>';
    el.innerHTML = html;
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
    var content = document.getElementById('opsContentPanel');
    content.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Cluster Alarms') +
        '<div id="opsAlarmContent"><div class="ops-spinner"></div> Loading...</div></div>';
    await opsRefreshAlarms();
}

async function opsRefreshAlarms() {
    var el = document.getElementById('opsAlarmContent');
    try {
        var resp = await opsFetchJSON('/api/ops/alarms');
        var data = await resp.json();
        var alarms = data.alarms || [];

        if (!alarms.length) {
            el.innerHTML = '<div class="ops-empty"><div class="ops-empty-icon">\u2705</div>Cluster is healthy. No active alarms.</div>';
            return;
        }

        var html = '<table class="ops-table"><thead><tr><th>Type</th><th>Member</th><th>Action</th></tr></thead><tbody>';
        alarms.forEach(function(a) {
            html += '<tr><td><strong>' + a.alarm_type + '</strong></td><td>' + a.member_name + ' (' + a.member_id + ')</td>' +
                '<td><button class="ops-btn ops-btn-danger" style="padding:4px 10px;font-size:12px" ' +
                'onclick="opsDisarmAlarm(\'' + a.member_id + '\',\'' + a.alarm_type + '\')">Disarm</button></td></tr>';
        });
        html += '</tbody></table>';
        el.innerHTML = html;
    } catch (e) {
        el.innerHTML = '<div class="ops-result error">Failed to load alarms: ' + e.message + '</div>';
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
    var content = document.getElementById('opsContentPanel');
    content.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Move Leader') +
        '<div id="opsLeaderContent"><div class="ops-spinner"></div> Loading...</div></div>';

    var members = await opsGetMembers();
    var el = document.getElementById('opsLeaderContent');
    var leader = members.find(function(m) { return m.is_leader; });
    var followers = members.filter(function(m) { return !m.is_leader; });

    if (members.length <= 1) {
        el.innerHTML = '<div class="ops-empty"><div class="ops-empty-icon">\u2139\ufe0f</div>Single-node cluster. Leader migration is not applicable.</div>';
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
    el.innerHTML = html;
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
    var content = document.getElementById('opsContentPanel');
    content.innerHTML = '<div class="ops-panel">' + opsPanelHeader('HashKV Consistency Check') +
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

// === Compact Panel ===
async function opsShowCompact() {
    var content = document.getElementById('opsContentPanel');
    content.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Compact') +
        '<div id="opsCompactContent"><div class="ops-spinner"></div> Loading...</div></div>';

    var el = document.getElementById('opsCompactContent');

    // Fetch current revision
    var revision = '-';
    try {
        var resp = await opsFetchJSON('/api/ops/compact/revision');
        if (resp && resp.ok) {
            var data = await resp.json();
            revision = data.revision;
        }
    } catch (e) { /* ignore, show '-' */ }

    var html = '<div class="ops-info">' +
        '<div class="ops-info-row"><span class="ops-info-label">Current Revision</span>' +
        '<span class="ops-info-value"><span id="opsCompactRevision">' + revision + '</span>' +
        ' <span class="ops-refresh-icon" onclick="opsRefreshCompactRevision()" title="Refresh revision" style="cursor:pointer;font-size:14px;opacity:0.7">&#x1f504;</span>' +
        ' <span style="font-size:11px;color:var(--text-secondary)">(reference only)</span></span></div></div>';

    html += '<div style="margin-bottom:16px">' +
        '<label style="font-size:13px;color:var(--text-secondary);font-weight:600;display:block;margin-bottom:6px">Retain Recent Revisions</label>' +
        '<input type="number" id="opsCompactRetain" class="ops-filter-select" min="1" placeholder="e.g. 1000" style="min-width:200px;padding:6px 10px">' +
        '</div>';

    html += '<div style="margin-bottom:16px">' +
        '<label style="font-size:13px;cursor:pointer"><input type="checkbox" id="opsCompactPhysical"> Physical Compaction ' +
        '<span class="ops-tooltip" style="position:relative;display:inline-block">' +
        '<span style="font-size:12px;color:var(--text-secondary);border:1px solid var(--text-secondary);border-radius:50%;width:16px;height:16px;display:inline-flex;align-items:center;justify-content:center;cursor:help">i</span>' +
        '<span class="ops-tooltip-text" style="visibility:hidden;position:absolute;bottom:125%;left:50%;transform:translateX(-50%);background:var(--bg-tertiary);color:var(--text-primary);padding:8px 12px;border-radius:6px;font-size:12px;white-space:nowrap;z-index:100;box-shadow:0 2px 8px rgba(0,0,0,0.3)">' +
        'Wait for physical deletion to complete before returning. Slower, but ensures compaction is fully applied.</span></span></label></div>';

    html += '<button class="ops-btn ops-btn-primary" id="opsCompactBtn" onclick="opsExecCompact()">Execute Compact</button>';
    html += '<div id="opsCompactResult" style="margin-top:12px"></div>';
    el.innerHTML = html;

    // Tooltip hover behavior
    var tooltip = el.querySelector('.ops-tooltip');
    if (tooltip) {
        var tip = tooltip.querySelector('.ops-tooltip-text');
        tooltip.onmouseenter = function() { tip.style.visibility = 'visible'; };
        tooltip.onmouseleave = function() { tip.style.visibility = 'hidden'; };
    }
}

async function opsRefreshCompactRevision() {
    try {
        var resp = await opsFetchJSON('/api/ops/compact/revision');
        if (resp && resp.ok) {
            var data = await resp.json();
            var el = document.getElementById('opsCompactRevision');
            if (el) el.textContent = data.revision;
        }
    } catch (e) { /* silent fail */ }
}

async function opsExecCompact() {
    var retainInput = document.getElementById('opsCompactRetain');
    var retainCount = parseInt(retainInput.value, 10);
    if (!retainCount || retainCount <= 0) {
        opsToast('Please enter a valid retain count (positive integer)', 'error');
        return;
    }

    var physical = document.getElementById('opsCompactPhysical').checked;

    var msg = 'Execute cluster-wide compact?<br><br>' +
        'Retain recent revisions: <strong>' + retainCount + '</strong><br>' +
        'Physical compaction: <strong>' + (physical ? 'Yes' : 'No') + '</strong><br><br>' +
        '<span style="font-size:12px;color:var(--text-secondary)">This will remove historical revisions older than the retained count.</span>';

    var confirmed = await opsConfirm('Confirm Compact', msg);
    if (!confirmed) return;

    var btn = document.getElementById('opsCompactBtn');
    var result = document.getElementById('opsCompactResult');
    btn.disabled = true;
    btn.textContent = 'Compacting...';
    result.innerHTML = '<span class="ops-spinner"></span> Executing compact...';

    try {
        var resp = await opsFetchJSON('/api/ops/compact', {
            method: 'POST',
            body: JSON.stringify({ retain_count: retainCount, physical: physical })
        });
        var data = await resp.json();
        if (resp.ok) {
            result.innerHTML = '<div class="ops-result success">' +
                '\u2713 Compact completed in ' + data.duration_ms + 'ms<br>' +
                '<table class="ops-table" style="margin-top:8px"><tbody>' +
                '<tr><td style="font-weight:600">Revision at execution</td><td>' + data.current_revision + '</td></tr>' +
                '<tr><td style="font-weight:600">Compacted to revision</td><td>' + data.target_revision + '</td></tr>' +
                '<tr><td style="font-weight:600">Retained</td><td>' + data.retain_count + ' revisions</td></tr>' +
                '<tr><td style="font-weight:600">Physical</td><td>' + (data.physical ? 'Yes' : 'No') + '</td></tr>' +
                '</tbody></table></div>';
            opsToast('Compact completed', 'success');
            // Refresh the displayed revision
            opsRefreshCompactRevision();
        } else {
            result.innerHTML = '<div class="ops-result error">\u2717 ' + (data.error || 'Compact failed') + '</div>';
            opsToast('Compact failed', 'error');
        }
    } catch (e) {
        result.innerHTML = '<div class="ops-result error">\u2717 ' + e.message + '</div>';
        opsToast('Compact failed: ' + e.message, 'error');
    }

    btn.disabled = false;
    btn.textContent = 'Execute Compact';
}

// === Audit Log Panel ===
var opsAuditPage = 1;
var opsAuditFilter = '';
var opsAuditSortCol = '';
var opsAuditSortAsc = true;
var opsAuditCurrentEntries = [];

function opsShowAuditLog() {
    var content = document.getElementById('opsContentPanel');
    content.innerHTML = '<div class="ops-panel">' + opsPanelHeader('Audit Log') +
        '<div class="ops-filter-bar">' +
        '<select class="ops-filter-select" id="opsAuditFilter" onchange="opsAuditFilterChange(this.value)">' +
        '<option value="">All Operations</option>' +
        '<option value="login">Login</option>' +
        '<option value="logout">Logout</option>' +
        '<option value="put">KV Put</option>' +
        '<option value="delete">KV Delete</option>' +
        '<option value="defragment">Defragment</option>' +
        '<option value="snapshot">Snapshot</option>' +
        '<option value="alarm_disarm">Alarm Disarm</option>' +
        '<option value="move_leader">Move Leader</option>' +
        '<option value="hashkv">HashKV</option>' +
        '<option value="compact">Compact</option></select>' +
        '<button class="ops-export-btn" id="opsExportBtn" onclick="opsExportCSV()">Export CSV</button>' +
        '</div>' +
        '<div id="opsAuditContent"><div class="ops-spinner"></div> Loading...</div></div>';
    opsAuditPage = 1;
    opsAuditFilter = '';
    opsAuditSortCol = '';
    opsAuditSortAsc = true;
    opsAuditCurrentEntries = [];
    opsLoadAuditLogs();
}

function opsAuditFilterChange(val) {
    opsAuditFilter = val;
    opsAuditPage = 1;
    opsAuditSortCol = '';
    opsAuditSortAsc = true;
    opsLoadAuditLogs();
}

function opsAuditSortBy(col) {
    if (opsAuditSortCol === col) {
        opsAuditSortAsc = !opsAuditSortAsc;
    } else {
        opsAuditSortCol = col;
        opsAuditSortAsc = true;
    }
    opsRenderAuditTable();
}

function opsGetSortedEntries() {
    if (!opsAuditSortCol) return opsAuditCurrentEntries.slice();
    var col = opsAuditSortCol;
    var asc = opsAuditSortAsc;
    return opsAuditCurrentEntries.slice().sort(function(a, b) {
        var va, vb;
        if (col === 'timestamp') { va = a.timestamp; vb = b.timestamp; }
        else if (col === 'username') { va = (a.username || '').toLowerCase(); vb = (b.username || '').toLowerCase(); }
        else if (col === 'operation') { va = a.operation; vb = b.operation; }
        else if (col === 'target') { va = (a.target || '').toLowerCase(); vb = (b.target || '').toLowerCase(); }
        else if (col === 'result') { va = (a.success ? '0' : '1') + (a.result || ''); vb = (b.success ? '0' : '1') + (b.result || ''); }
        else if (col === 'duration_ms') { va = a.duration_ms; vb = b.duration_ms; }
        else { return 0; }
        if (va < vb) return asc ? -1 : 1;
        if (va > vb) return asc ? 1 : -1;
        return 0;
    });
}

function opsAuditSortArrow(col) {
    if (opsAuditSortCol !== col) return '';
    return '<span class="sort-arrow">' + (opsAuditSortAsc ? '\u25b2' : '\u25bc') + '</span>';
}

function opsRenderAuditTable() {
    var el = document.getElementById('opsAuditContent');
    if (!el) return;

    var sorted = opsGetSortedEntries();

    var html = '<table class="ops-table"><thead><tr>' +
        '<th class="sortable" onclick="opsAuditSortBy(\'timestamp\')">Time' + opsAuditSortArrow('timestamp') + '</th>' +
        '<th class="sortable" onclick="opsAuditSortBy(\'username\')">User' + opsAuditSortArrow('username') + '</th>' +
        '<th class="sortable" onclick="opsAuditSortBy(\'operation\')">Operation' + opsAuditSortArrow('operation') + '</th>' +
        '<th class="sortable" onclick="opsAuditSortBy(\'target\')">Target' + opsAuditSortArrow('target') + '</th>' +
        '<th class="sortable" onclick="opsAuditSortBy(\'result\')">Result' + opsAuditSortArrow('result') + '</th>' +
        '<th class="sortable" onclick="opsAuditSortBy(\'duration_ms\')">Duration' + opsAuditSortArrow('duration_ms') + '</th>' +
        '</tr></thead><tbody>';
    sorted.forEach(function(e) {
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

    // Restore pagination from stored values
    var paginationEl = document.getElementById('opsAuditPagination');
    if (paginationEl) {
        el.innerHTML = html;
        el.appendChild(paginationEl);
    } else {
        el.innerHTML = html;
    }
}

var opsAuditTotal = 0;
var opsAuditTotalPages = 1;

async function opsLoadAuditLogs() {
    var el = document.getElementById('opsAuditContent');
    var url = '/api/ops/audit-logs?page=' + opsAuditPage + '&page_size=15';
    if (opsAuditFilter) url += '&operation=' + opsAuditFilter;

    try {
        var resp = await opsFetchJSON(url);
        var data = await resp.json();
        var entries = data.entries || [];
        opsAuditTotal = data.total || 0;
        var pageSize = data.page_size || 15;
        opsAuditTotalPages = Math.ceil(opsAuditTotal / pageSize) || 1;

        if (!entries.length) {
            el.innerHTML = '<div class="ops-empty"><div class="ops-empty-icon">\ud83d\udcdd</div>No audit log entries found.</div>';
            return;
        }

        opsAuditCurrentEntries = entries;
        opsRenderAuditTable();

        // Append pagination
        var pagHTML = '<div class="ops-pagination" id="opsAuditPagination">' +
            '<button onclick="opsAuditPage--;opsLoadAuditLogs()"' + (opsAuditPage <= 1 ? ' disabled' : '') + '>&laquo; Prev</button>' +
            '<span>Page ' + opsAuditPage + ' / ' + opsAuditTotalPages + ' (' + opsAuditTotal + ' total)</span>' +
            '<button onclick="opsAuditPage++;opsLoadAuditLogs()"' + (opsAuditPage >= opsAuditTotalPages ? ' disabled' : '') + '>Next &raquo;</button></div>';
        el.insertAdjacentHTML('beforeend', pagHTML);
    } catch (e) {
        el.innerHTML = '<div class="ops-result error">Failed to load audit logs: ' + e.message + '</div>';
    }
}

// === CSV Export ===
async function opsExportCSV() {
    var btn = document.getElementById('opsExportBtn');
    if (!btn) return;
    btn.disabled = true;
    btn.textContent = 'Exporting...';

    try {
        var url = '/api/ops/audit-logs?page=1&page_size=10000';
        if (opsAuditFilter) url += '&operation=' + opsAuditFilter;
        var resp = await opsFetchJSON(url);
        var data = await resp.json();
        var entries = data.entries || [];

        if (!entries.length) {
            opsToast('No data to export', 'error');
            btn.disabled = false;
            btn.textContent = 'Export CSV';
            return;
        }

        var csvRows = [];
        csvRows.push('Time,User,Operation,Target,Result,Duration(ms)');
        entries.forEach(function(e) {
            var dt = new Date(e.timestamp * 1000);
            var timeStr = dt.toISOString();
            var result = e.success ? 'success' : 'failed';
            if (e.result) result += ': ' + e.result;
            // Escape CSV fields
            var escapeCsv = function(val) {
                val = String(val || '');
                if (val.indexOf(',') >= 0 || val.indexOf('"') >= 0 || val.indexOf('\n') >= 0) {
                    return '"' + val.replace(/"/g, '""') + '"';
                }
                return val;
            };
            csvRows.push([
                escapeCsv(timeStr),
                escapeCsv(e.username || ''),
                escapeCsv(e.operation),
                escapeCsv(e.target || ''),
                escapeCsv(result),
                e.duration_ms
            ].join(','));
        });

        var csvContent = '\ufeff' + csvRows.join('\n');
        var blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
        var url2 = URL.createObjectURL(blob);
        var a = document.createElement('a');
        a.href = url2;
        a.download = 'audit-log-' + new Date().toISOString().slice(0, 10) + '.csv';
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url2);

        opsToast('CSV exported (' + entries.length + ' records)', 'success');
    } catch (e) {
        opsToast('Export failed: ' + e.message, 'error');
    }

    btn.disabled = false;
    btn.textContent = 'Export CSV';
}
