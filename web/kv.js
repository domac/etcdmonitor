// ============================================
// etcdmonitor - KV Management Module
// Tree view, CRUD operations, ACE Editor
// ============================================

// === KV Global State ===
var kvState = {
    protocol: 'v3',           // 'v3' or 'v2'
    treeMode: 'tree',         // 'tree' or 'list'
    selectedKey: null,         // currently selected key path
    selectedNode: null,        // currently selected node data
    treeData: null,            // root node of tree data model
    aceEditor: null,           // ACE Editor instance
    initialized: false,        // whether kvInit has been called
    separator: '/',            // key path separator
    contextNode: null,         // node for context menu action
    deleteTarget: null,        // node pending delete confirmation
    // 各协议独立缓存树数据和选中状态
    cache: {
        v3: { treeData: null, selectedKey: null, selectedNode: null },
        v2: { treeData: null, selectedKey: null, selectedNode: null }
    }
};

// === API Base ===
function kvApiBase() {
    return '/api/kv/' + kvState.protocol;
}

// === Initialization ===
function kvInit() {
    if (kvState.initialized) return;
    kvState.initialized = true;

    // Initialize ACE Editor if available
    if (typeof ace !== 'undefined') {
        kvInitEditor();
    }

    // Close context menu on any click
    document.addEventListener('click', function(e) {
        var menu = document.getElementById('kvContextMenu');
        if (menu && !menu.contains(e.target)) {
            menu.style.display = 'none';
        }
    });

    // Initialize resize handle for tree panel
    kvInitResize();

    // Load initial data
    kvConnect();
}

// === Connect & Cluster Info ===

// === Resize Handle for Tree Panel ===
function kvInitResize() {
    var handle = document.getElementById('kvResizeHandle');
    var treePanel = document.getElementById('kvTreePanel');
    var layout = treePanel.parentElement; // .kv-layout

    var startX, startWidth;

    function onMouseDown(e) {
        e.preventDefault();
        startX = e.clientX;
        startWidth = treePanel.getBoundingClientRect().width;
        handle.classList.add('active');
        document.body.style.cursor = 'col-resize';
        document.body.style.userSelect = 'none';
        document.addEventListener('mousemove', onMouseMove);
        document.addEventListener('mouseup', onMouseUp);
    }

    function onMouseMove(e) {
        var dx = e.clientX - startX;
        var newWidth = startWidth + dx;
        var layoutWidth = layout.getBoundingClientRect().width;
        // Clamp: min 150px, max 70% of layout
        var minW = 150;
        var maxW = layoutWidth * 0.7;
        newWidth = Math.max(minW, Math.min(maxW, newWidth));
        treePanel.style.width = newWidth + 'px';
        // Resize ACE editor if visible
        if (kvState.aceEditor) {
            kvState.aceEditor.resize();
        }
    }

    function onMouseUp() {
        handle.classList.remove('active');
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        document.removeEventListener('mousemove', onMouseMove);
        document.removeEventListener('mouseup', onMouseUp);
    }

    handle.addEventListener('mousedown', onMouseDown);
}


async function kvConnect() {
    try {
        var resp = await kvFetch(kvApiBase() + '/connect', { method: 'POST' });
        if (resp.error) {
            kvShowTreeError(resp.error);
            return;
        }
        document.getElementById('kvVersion').textContent = resp.version || '-';
        document.getElementById('kvLeader').textContent = resp.name || '-';
        document.getElementById('kvDBSize').textContent = resp.size_str || '-';

        // Get separator
        var sepResp = await kvFetch(kvApiBase() + '/separator');
        if (sepResp.separator) {
            kvState.separator = sepResp.separator;
        }

        // Load tree
        kvLoadTree();
    } catch (e) {
        kvShowTreeError('Connection failed: ' + e.message);
    }
}

// === Protocol Switch ===
function kvSwitchProtocol(proto) {
    if (kvState.protocol === proto) return;

    // 保存当前协议的树状态到缓存
    var oldProto = kvState.protocol;
    kvState.cache[oldProto].treeData = kvState.treeData;
    kvState.cache[oldProto].selectedKey = kvState.selectedKey;
    kvState.cache[oldProto].selectedNode = kvState.selectedNode;

    kvState.protocol = proto;

    // Update toggle UI
    document.getElementById('kvTabV3').classList.toggle('active', proto === 'v3');
    document.getElementById('kvTabV2').classList.toggle('active', proto === 'v2');
    document.getElementById('kvProtoSlider').classList.toggle('right', proto === 'v2');

    // 尝试从缓存恢复目标协议的树状态
    var cached = kvState.cache[proto];
    if (cached.treeData) {
        kvState.treeData = cached.treeData;
        kvState.selectedKey = cached.selectedKey;
        kvState.selectedNode = cached.selectedNode;
        kvRenderTree();
        // 恢复选中状态的右侧面板
        if (kvState.selectedNode) {
            kvShowKeyInfo(kvState.selectedNode);
            if (kvState.selectedNode.value) {
                kvShowEditor(kvState.selectedNode);
            } else {
                kvHideEditor();
            }
        } else {
            kvClearSelection();
        }
        // 后台刷新连接信息（Version/Leader/DBSize）
        kvFetch(kvApiBase() + '/connect', { method: 'POST' }).then(function(resp) {
            if (!resp.error) {
                document.getElementById('kvVersion').textContent = resp.version || '-';
                document.getElementById('kvLeader').textContent = resp.name || '-';
                document.getElementById('kvDBSize').textContent = resp.size_str || '-';
            }
        });
    } else {
        // 首次切换到该协议，全量加载
        kvClearSelection();
        kvConnect();
    }
}

// === Tree Mode Toggle ===
function kvSwitchTreeMode(mode) {
    if (kvState.treeMode === mode) return;
    kvState.treeMode = mode;

    // Update toggle UI
    document.getElementById('kvViewTree').classList.toggle('active', mode === 'tree');
    document.getElementById('kvViewList').classList.toggle('active', mode === 'list');
    document.getElementById('kvViewSlider').classList.toggle('right', mode === 'list');

    kvRenderTree();
}

// === Tree Data Loading ===
async function kvLoadTree(key) {
    key = key || kvState.separator;
    kvShowTreeLoading(true);
    kvHideTreeError();

    try {
        var resp = await kvFetch(kvApiBase() + '/getpath?key=' + encodeURIComponent(key));
        if (resp.error) {
            kvShowTreeError(resp.error);
            return;
        }
        kvState.treeData = resp.node;
        kvState.cache[kvState.protocol].treeData = resp.node;
        kvRenderTree();
    } catch (e) {
        kvShowTreeError('Failed to load: ' + e.message);
    } finally {
        kvShowTreeLoading(false);
    }
}

// === Tree Data Model Operations ===
function kvFindNode(root, targetKey) {
    if (!root) return null;
    if (root.key === targetKey) return root;
    if (root.nodes) {
        for (var i = 0; i < root.nodes.length; i++) {
            var found = kvFindNode(root.nodes[i], targetKey);
            if (found) return found;
        }
    }
    return null;
}

function kvRemoveNode(root, targetKey) {
    if (!root || !root.nodes) return false;
    for (var i = 0; i < root.nodes.length; i++) {
        if (root.nodes[i].key === targetKey) {
            root.nodes.splice(i, 1);
            return true;
        }
        if (kvRemoveNode(root.nodes[i], targetKey)) return true;
    }
    return false;
}

function kvAddChild(parentNode, childNode) {
    if (!parentNode.nodes) parentNode.nodes = [];
    // Insert in sorted order
    var idx = parentNode.nodes.findIndex(function(n) { return n.key > childNode.key; });
    if (idx === -1) {
        parentNode.nodes.push(childNode);
    } else {
        parentNode.nodes.splice(idx, 0, childNode);
    }
    parentNode.dir = true;
}

// 移除已过期（TTL 到期）的节点，清除选中状态并刷新树
function kvRemoveExpiredNode(node) {
    kvShowToast('Key expired (TTL): ' + node.key, 'error');
    if (kvState.selectedKey === node.key) {
        kvClearSelection();
    }
    kvRemoveNode(kvState.treeData, node.key);
    kvRenderTree();
}

// === Tree Rendering (Data-Driven) ===
function kvRenderTree() {
    var container = document.getElementById('kvTree');
    var emptyEl = document.getElementById('kvTreeEmpty');

    container.innerHTML = '';

    if (!kvState.treeData || (!kvState.treeData.nodes || kvState.treeData.nodes.length === 0)) {
        emptyEl.style.display = '';
        return;
    }
    emptyEl.style.display = 'none';

    if (kvState.treeMode === 'list') {
        kvRenderListMode(container, kvState.treeData);
    } else {
        kvRenderPathMode(container, kvState.treeData.nodes, 0);
    }
}

function kvRenderPathMode(container, nodes, depth) {
    if (!nodes) return;
    nodes.forEach(function(node) {
        var el = kvCreateTreeNodeEl(node, depth, false);
        container.appendChild(el);
    });
}

function kvRenderListMode(container, root) {
    // Flatten all real keys (leaves + directories with values) — flat list, no hierarchy
    var items = [];
    function collect(node) {
        if (node.key !== kvState.separator) {
            if (!node.dir) {
                items.push(node);
            } else if (node.value || node.createdIndex || node.modifiedIndex) {
                items.push(node);
            }
        }
        if (node.nodes) {
            node.nodes.forEach(collect);
        }
    }
    if (root.nodes) root.nodes.forEach(collect);

    items.forEach(function(node) {
        var wrapper = document.createElement('div');
        wrapper.className = 'kv-tree-node';
        wrapper.dataset.key = node.key;

        var row = document.createElement('div');
        row.className = 'kv-list-row';
        if (kvState.selectedKey === node.key) {
            row.classList.add('selected');
        }

        // Icon
        var icon = document.createElement('span');
        icon.className = 'kv-tree-icon ' + (node.dir ? 'dir' : 'file');
        icon.textContent = node.dir ? '\uD83D\uDCC1' : '\uD83D\uDCC4';
        row.appendChild(icon);

        // Full key path
        var name = document.createElement('span');
        name.className = 'kv-tree-name';
        name.textContent = node.key;
        name.title = node.key;
        row.appendChild(name);

        // Click handler
        row.addEventListener('click', function() {
            kvSelectNode(node);
        });

        // Right-click context menu
        row.addEventListener('contextmenu', function(e) {
            e.preventDefault();
            kvShowContextMenu(e, node);
        });

        wrapper.appendChild(row);
        container.appendChild(wrapper);
    });
}

function kvCreateTreeNodeEl(node, depth, showFullPath) {
    var wrapper = document.createElement('div');
    wrapper.className = 'kv-tree-node';
    wrapper.dataset.key = node.key;

    // Row
    var row = document.createElement('div');
    row.className = 'kv-tree-row';
    if (kvState.selectedKey === node.key) {
        row.classList.add('selected');
    }
    row.style.paddingLeft = (8 + depth * 16) + 'px';

    // Arrow
    var arrow = document.createElement('span');
    arrow.className = 'kv-tree-arrow';
    if (node.dir) {
        arrow.textContent = '\u25B6'; // right triangle
        if (node._expanded) {
            arrow.classList.add('expanded');
        }
        arrow.addEventListener('click', function(e) {
            e.stopPropagation();
            kvToggleExpand(node, wrapper);
        });
    } else {
        arrow.classList.add('leaf');
    }
    row.appendChild(arrow);

    // Icon
    var icon = document.createElement('span');
    icon.className = 'kv-tree-icon ' + (node.dir ? 'dir' : 'file');
    icon.textContent = node.dir ? '\uD83D\uDCC1' : '\uD83D\uDCC4';
    row.appendChild(icon);

    // Name
    var name = document.createElement('span');
    name.className = 'kv-tree-name';
    if (showFullPath) {
        name.textContent = node.key;
    } else {
        // Show last segment
        var parts = node.key.split(kvState.separator);
        name.textContent = parts[parts.length - 1] || node.key;
    }
    name.title = node.key;
    row.appendChild(name);

    // Click handler — directories: toggle expand; leaves: select
    row.addEventListener('click', function() {
        if (node.dir) {
            kvToggleExpand(node, wrapper);
        }
        kvSelectNode(node);
    });

    // Right-click context menu
    row.addEventListener('contextmenu', function(e) {
        e.preventDefault();
        kvShowContextMenu(e, node);
    });

    wrapper.appendChild(row);

    // Children container (for path mode, pre-expanded nodes)
    if (node.dir && node.nodes && node.nodes.length > 0 && node._expanded) {
        var children = document.createElement('div');
        children.className = 'kv-tree-children';
        kvRenderPathMode(children, node.nodes, depth + 1);
        wrapper.appendChild(children);
    }

    return wrapper;
}

// === Tree Expand/Collapse ===
async function kvToggleExpand(node, wrapperEl) {
    if (node._expanded) {
        // Collapse
        node._expanded = false;
        kvRenderTree();
        return;
    }

    // Expand — always fetch fresh children from etcd (like etcdkeeper)
    await kvRefreshChildren(node);
    node._expanded = true;
    kvRenderTree();
}

// 从 etcd 实时刷新目录节点的子节点
async function kvRefreshChildren(node) {
    try {
        var resp = await kvFetch(kvApiBase() + '/getpath?key=' + encodeURIComponent(node.key));
        if (resp.error) {
            kvShowToast('Failed to load: ' + resp.error, 'error');
            return;
        }
        if (resp.node) {
            // 保留子节点的展开状态
            var expandedKeys = {};
            if (node.nodes) {
                (function collectExpanded(nodes) {
                    nodes.forEach(function(n) {
                        if (n._expanded) expandedKeys[n.key] = true;
                        if (n.nodes) collectExpanded(n.nodes);
                    });
                })(node.nodes);
            }

            // 用新数据替换子节点
            node.nodes = resp.node.nodes || [];

            // 更新目录本身的元信息（value/TTL/revision）
            // resp.node 是 getpath 返回的根节点，包含该 key 自身的信息
            node.value = resp.node.value || '';
            node.ttl = resp.node.ttl || 0;
            node.createdIndex = resp.node.createdIndex || node.createdIndex;
            node.modifiedIndex = resp.node.modifiedIndex || node.modifiedIndex;
            node.version = resp.node.version || node.version;

            // 恢复子节点展开状态
            (function restoreExpanded(nodes) {
                nodes.forEach(function(n) {
                    if (expandedKeys[n.key]) n._expanded = true;
                    if (n.nodes) restoreExpanded(n.nodes);
                });
            })(node.nodes);
        }
    } catch (e) {
        kvShowToast('Failed to load: ' + e.message, 'error');
    }
}

// === Node Selection ===
async function kvSelectNode(node) {
    kvState.selectedKey = node.key;
    kvState.selectedNode = node;

    // Update tree UI
    document.querySelectorAll('.kv-tree-row.selected').forEach(function(el) {
        el.classList.remove('selected');
    });
    var rows = document.querySelectorAll('.kv-tree-row');
    rows.forEach(function(r) {
        var wrapper = r.parentElement;
        if (wrapper && wrapper.dataset.key === node.key) {
            r.classList.add('selected');
        }
    });

    if (node.dir) {
        // Directory node — refresh children from etcd (handles TTL expiry)
        if (node._expanded) {
            await kvRefreshChildren(node);
            kvRenderTree();
            // Re-highlight selected row after re-render
            document.querySelectorAll('.kv-tree-row').forEach(function(r) {
                var wrapper = r.parentElement;
                if (wrapper && wrapper.dataset.key === node.key) {
                    r.classList.add('selected');
                }
            });
        }

        kvShowKeyInfo(node);
        if (node.value) {
            kvShowEditor(node);
        } else {
            kvHideEditor();
        }
        return;
    }

    // Leaf: always fetch latest data (TTL is realtime)
    try {
        var resp = await kvFetch(kvApiBase() + '/get?key=' + encodeURIComponent(node.key));
        if (resp.error) {
            if (resp.code === 'key_not_found') {
                // Key expired (TTL), remove from tree
                kvRemoveExpiredNode(node);
                return;
            }
            kvShowToast(resp.error, 'error');
            return;
        }
        var n = resp.node;
        // Update node data
        node.value = n.value;
        node.ttl = n.ttl;
        node.createdIndex = n.createdIndex;
        node.modifiedIndex = n.modifiedIndex;
        node.version = n.version;

        kvShowKeyInfo(node);
        kvShowEditor(node);
    } catch (e) {
        kvShowToast('Failed to get key: ' + e.message, 'error');
    }
}

// === Key Info Display ===
function kvShowKeyInfo(node) {
    var infoEl = document.getElementById('kvKeyInfo');
    infoEl.style.display = '';
    document.getElementById('kvKeyPath').textContent = node.key;

    var meta = [];

    if (node.dir) {
        meta.push('Type: Directory');
        if (node.nodes) meta.push('Children: ' + node.nodes.length);
    }

    // Show revision/version/TTL for all nodes (dir or leaf) that have data
    if (kvState.protocol === 'v3') {
        if (node.createdIndex) meta.push('CreateRev: ' + node.createdIndex);
        if (node.modifiedIndex) meta.push('ModRev: ' + node.modifiedIndex);
        if (node.version) meta.push('Version: ' + node.version);
    } else {
        if (node.createdIndex) meta.push('CreatedIdx: ' + node.createdIndex);
        if (node.modifiedIndex) meta.push('ModifiedIdx: ' + node.modifiedIndex);
    }
    if (node.ttl) meta.push('TTL: ' + node.ttl + 's');

    document.getElementById('kvKeyMeta').textContent = meta.join(' | ');
}

// === Editor ===
function kvInitEditor() {
    var editor = ace.edit('kvEditor');
    editor.session.setMode('ace/mode/json');
    editor.setFontSize(13);
    editor.setShowPrintMargin(false);
    editor.setOptions({
        enableBasicAutocompletion: false,
        enableLiveAutocompletion: false,
        tabSize: 2,
        useSoftTabs: true,
        wrap: true
    });
    kvState.aceEditor = editor;
    kvApplyEditorTheme();
}

// 应用编辑器主题（主题 JS 已在 HTML 中预加载，setTheme 同步生效）
function kvApplyEditorTheme() {
    if (!kvState.aceEditor) return;
    var isDark = document.documentElement.getAttribute('data-theme') === 'dark';
    kvState.aceEditor.setTheme(isDark ? 'ace/theme/monokai' : 'ace/theme/chrome');
}

function kvShowEditor(node) {
    document.getElementById('kvEditorPlaceholder').style.display = 'none';
    document.getElementById('kvEditor').style.display = '';
    document.getElementById('kvEditorToolbar').style.display = '';

    // Store raw value for format/restore
    kvState._rawValue = node.value || '';
    kvState._formatted = false;

    if (kvState.aceEditor) {
        kvApplyEditorTheme();
        kvState.aceEditor.setValue(kvState._rawValue, -1);
        kvState.aceEditor.clearSelection();

        // Auto-detect mode
        var mode = kvDetectMode(node.key, node.value);
        kvState.aceEditor.session.setMode('ace/mode/' + mode);
        document.getElementById('kvModeSelect').value = mode;

        // Show/hide Format button based on mode
        kvUpdateFormatBtn();

        // Resize after display
        kvState.aceEditor.resize();
    }
}

function kvHideEditor() {
    document.getElementById('kvEditorPlaceholder').style.display = '';
    document.getElementById('kvEditor').style.display = 'none';
    document.getElementById('kvEditorToolbar').style.display = 'none';
}

function kvDetectMode(key, value) {
    // By file extension
    var ext = key.split('.').pop().toLowerCase();
    var extMap = {
        'json': 'json', 'yaml': 'yaml', 'yml': 'yaml',
        'xml': 'xml', 'toml': 'toml', 'ini': 'ini',
        'js': 'javascript', 'lua': 'lua', 'sh': 'sh',
        'py': 'python', 'conf': 'ini', 'cfg': 'ini'
    };
    if (extMap[ext]) return extMap[ext];

    // By content
    if (value) {
        var trimmed = value.trim();
        if ((trimmed.startsWith('{') && trimmed.endsWith('}')) ||
            (trimmed.startsWith('[') && trimmed.endsWith(']'))) {
            return 'json';
        }
        if (trimmed.startsWith('<?xml') || trimmed.startsWith('<')) {
            return 'xml';
        }
    }

    return 'text';
}

function kvChangeMode(mode) {
    if (kvState.aceEditor) {
        // If currently formatted, restore raw value before switching mode
        if (kvState._formatted && kvState._rawValue !== null) {
            kvState.aceEditor.setValue(kvState._rawValue, -1);
            kvState.aceEditor.clearSelection();
            kvState._formatted = false;
        }
        kvState.aceEditor.session.setMode('ace/mode/' + mode);
    }
    kvUpdateFormatBtn();
}

// === Format Button (JSON only, view-only formatting) ===
// Format only changes the display for readability.
// Switching mode or loading a new key restores the raw value.
// Only clicking Save will persist the current editor content (formatted or not).
function kvFormatJSON() {
    if (!kvState.aceEditor) return;
    if (kvState._formatted) return; // already formatted, don't re-format

    var content = kvState.aceEditor.getValue();
    try {
        var obj = JSON.parse(content);
        // Save raw before formatting (in case user edited after load but before format)
        kvState._rawValue = content;
        kvState.aceEditor.setValue(JSON.stringify(obj, null, 2), -1);
        kvState.aceEditor.clearSelection();
        kvState._formatted = true;
        kvShowToast('JSON formatted (view only, switch mode to restore)', 'success');
    } catch (e) {
        kvShowToast('Invalid JSON format', 'error');
    }
}

// Show/hide Format button based on current editor mode
function kvUpdateFormatBtn() {
    var mode = document.getElementById('kvModeSelect').value;
    var btn = document.getElementById('kvFormatBtn');
    btn.style.display = (mode === 'json') ? '' : 'none';
}

// === Update ACE theme when system theme changes ===
// Hook into toggleTheme from app.js
var _origToggleTheme = typeof toggleTheme === 'function' ? toggleTheme : null;
if (_origToggleTheme) {
    // We'll re-hook after DOMContentLoaded via a different mechanism
}
// Instead, watch for theme attribute changes
(function() {
    var observer = new MutationObserver(function(mutations) {
        mutations.forEach(function(m) {
            if (m.attributeName === 'data-theme') {
                kvApplyEditorTheme();
            }
        });
    });
    observer.observe(document.documentElement, { attributes: true });
})();

// === CRUD: Save Value ===
async function kvSaveValue() {
    if (!kvState.selectedNode) return;
    if (!kvState.aceEditor) return;

    var value = kvState.aceEditor.getValue();
    try {
        var resp = await kvFetch(kvApiBase() + '/put', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                key: kvState.selectedNode.key,
                value: value
            })
        });
        if (resp.error) {
            if (resp.code === 'permission_denied') {
                kvShowToast('Permission denied: the configured etcd user does not have permission for this operation', 'error');
            } else {
                kvShowToast('Save failed: ' + resp.error, 'error');
            }
            return;
        }
        // Update local node data
        if (resp.node) {
            kvState.selectedNode.value = resp.node.value;
            kvState.selectedNode.modifiedIndex = resp.node.modifiedIndex;
            kvState.selectedNode.version = resp.node.version;
            kvShowKeyInfo(kvState.selectedNode);
        }
        kvShowToast('Saved successfully', 'success');
    } catch (e) {
        kvShowToast('Save failed: ' + e.message, 'error');
    }
}

// === CRUD: Create Key ===
function kvOpenCreateDialog(parentKey, isDir) {
    document.getElementById('kvCreateDialog').style.display = '';
    document.getElementById('kvCreateDialogTitle').textContent = 'Create Node';

    // 显示父路径为不可编辑前缀，用户只需输入名称
    var prefix = parentKey || kvState.separator;
    if (prefix !== kvState.separator && !prefix.endsWith(kvState.separator)) {
        prefix = prefix + kvState.separator;
    }
    document.getElementById('kvCreatePrefix').textContent = prefix;
    document.getElementById('kvCreatePrefix').title = prefix;
    document.getElementById('kvCreateKey').value = '';

    // Dir 选项始终显示（对齐 etcdkeeper：V3 后端虽忽略，但前端保留选项）
    document.getElementById('kvCreateDirGroup').style.display = '';
    document.getElementById('kvCreateIsDir').checked = isDir || false;

    document.getElementById('kvCreateTTL').value = '';
    document.getElementById('kvCreateValue').value = '';
    kvToggleDirMode();

    // 自动聚焦到名称输入框
    setTimeout(function() { document.getElementById('kvCreateKey').focus(); }, 100);
}

function kvCloseCreateDialog() {
    document.getElementById('kvCreateDialog').style.display = 'none';
}

function kvToggleDirMode() {
    var isDir = document.getElementById('kvCreateIsDir').checked;
    // V3 目录也可以有值，始终显示 Value 输入区域
    // V2 目录不能有值，勾选 Dir 时隐藏 Value
    if (kvState.protocol === 'v2') {
        document.getElementById('kvCreateValueGroup').style.display = isDir ? 'none' : '';
    } else {
        document.getElementById('kvCreateValueGroup').style.display = '';
    }
}

async function kvSubmitCreate() {
    var name = document.getElementById('kvCreateKey').value.trim();
    var prefix = document.getElementById('kvCreatePrefix').textContent;
    var isDir = document.getElementById('kvCreateIsDir').checked;
    var ttl = parseInt(document.getElementById('kvCreateTTL').value) || 0;
    var value = document.getElementById('kvCreateValue').value;

    if (!name) {
        kvShowToast('Key name is required', 'error');
        return;
    }

    // 拼接完整路径：前缀 + 名称
    var key = prefix + name;

    try {
        var resp = await kvFetch(kvApiBase() + '/put', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                key: key,
                value: value,
                ttl: ttl,
                dir: isDir
            })
        });
        if (resp.error) {
            if (resp.code === 'permission_denied') {
                kvShowToast('Permission denied: the configured etcd user does not have permission for this operation', 'error');
            } else {
                kvShowToast('Create failed: ' + resp.error, 'error');
            }
            return;
        }
        kvCloseCreateDialog();
        kvShowToast('Created successfully', 'success');

        // 局部更新树：将新节点插入到父节点下，保留展开状态
        var newNode = resp.node || {
            key: key,
            value: isDir ? '' : value,
            dir: isDir,
            ttl: ttl
        };
        if (isDir) {
            newNode.dir = true;
            newNode.nodes = [];
        }

        // 找到父节点并插入
        var sep = kvState.separator;
        var parentKey = key.substring(0, key.lastIndexOf(sep)) || sep;
        var parentNode = kvFindNode(kvState.treeData, parentKey);
        if (parentNode) {
            // 检查是否已存在（避免重复）
            var existing = kvFindNode(parentNode, key);
            if (!existing) {
                kvAddChild(parentNode, newNode);
            }
            parentNode._expanded = true;
            kvRenderTree();
        } else {
            // 父节点不在树中（可能未展开），回退到重新加载父节点的子树
            kvLoadTree();
        }
    } catch (e) {
        kvShowToast('Create failed: ' + e.message, 'error');
    }
}

// === CRUD: Delete Key ===
function kvOpenDeleteDialog(node) {
    kvState.deleteTarget = node;
    var msg = node.dir
        ? 'Are you sure you want to delete directory "' + node.key + '" and ALL its children? This action cannot be undone.'
        : 'Are you sure you want to delete key "' + node.key + '"?';
    document.getElementById('kvDeleteMessage').textContent = msg;
    document.getElementById('kvDeleteDialog').style.display = '';
}

function kvCloseDeleteDialog() {
    document.getElementById('kvDeleteDialog').style.display = 'none';
    kvState.deleteTarget = null;
}

async function kvConfirmDelete() {
    var node = kvState.deleteTarget;
    if (!node) return;

    try {
        var resp = await kvFetch(kvApiBase() + '/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                key: node.key,
                dir: node.dir
            })
        });
        if (resp.error) {
            if (resp.code === 'permission_denied') {
                kvShowToast('Permission denied: the configured etcd user does not have permission for this operation', 'error');
            } else {
                kvShowToast('Delete failed: ' + resp.error, 'error');
            }
            return;
        }
        kvCloseDeleteDialog();
        kvShowToast('Deleted successfully', 'success');

        // Remove from data model and re-render
        if (kvState.selectedKey === node.key) {
            kvClearSelection();
        }
        kvRemoveNode(kvState.treeData, node.key);
        kvRenderTree();
    } catch (e) {
        kvShowToast('Delete failed: ' + e.message, 'error');
    }
}

// === Context Menu ===
function kvShowContextMenu(e, node) {
    kvState.contextNode = node;
    var menu = document.getElementById('kvContextMenu');

    // 对齐 etcdkeeper 的右键菜单逻辑：
    // items[0] = Create Node, items[1] = Remove Node
    var items = menu.querySelectorAll('.kv-context-item');
    if (kvState.protocol === 'v3') {
        if (kvState.treeMode === 'tree') {
            // V3 Tree(Path) 模式：所有节点都显示 Create Node + Remove Node
            items[0].style.display = '';
        } else {
            // V3 List 模式：只有 dir 节点显示 Create Node
            items[0].style.display = node.dir ? '' : 'none';
        }
    } else {
        // V2：只有目录才能创建子节点
        items[0].style.display = node.dir ? '' : 'none';
    }
    // Remove Node 始终显示
    items[1].style.display = '';

    menu.style.display = '';
    menu.style.left = e.clientX + 'px';
    menu.style.top = e.clientY + 'px';

    // Ensure menu stays within viewport
    var rect = menu.getBoundingClientRect();
    if (rect.right > window.innerWidth) {
        menu.style.left = (e.clientX - rect.width) + 'px';
    }
    if (rect.bottom > window.innerHeight) {
        menu.style.top = (e.clientY - rect.height) + 'px';
    }
}

function kvContextAction(action) {
    document.getElementById('kvContextMenu').style.display = 'none';
    var node = kvState.contextNode;
    if (!node) return;

    switch (action) {
        case 'create':
            kvOpenCreateDialog(node.key, false);
            break;
        case 'delete':
            kvOpenDeleteDialog(node);
            break;
    }
}

// === Clear Selection ===
function kvClearSelection() {
    kvState.selectedKey = null;
    kvState.selectedNode = null;
    document.getElementById('kvKeyInfo').style.display = 'none';
    kvHideEditor();
    document.querySelectorAll('.kv-tree-row.selected').forEach(function(el) {
        el.classList.remove('selected');
    });
}

// === UI Helpers ===
function kvShowTreeLoading(show) {
    document.getElementById('kvTreeLoading').style.display = show ? '' : 'none';
    if (show) {
        document.getElementById('kvTree').innerHTML = '';
        document.getElementById('kvTreeEmpty').style.display = 'none';
    }
}

function kvShowTreeError(msg) {
    var el = document.getElementById('kvTreeError');
    el.textContent = msg;
    el.style.display = '';
    document.getElementById('kvTreeLoading').style.display = 'none';
    document.getElementById('kvTreeEmpty').style.display = 'none';
}

function kvHideTreeError() {
    document.getElementById('kvTreeError').style.display = 'none';
}

var _toastTimer = null;
function kvShowToast(msg, type) {
    var toast = document.getElementById('kvToast');
    toast.textContent = msg;
    toast.className = 'kv-toast' + (type ? ' ' + type : '');
    // 触发 reflow 后添加 show 类实现淡入动画
    void toast.offsetWidth;
    toast.classList.add('show');
    clearTimeout(_toastTimer);
    _toastTimer = setTimeout(function() {
        toast.classList.remove('show');
    }, 3000);
}

// === Fetch with Auth ===
async function kvFetch(url, opts) {
    opts = opts || {};
    if (!opts.headers) opts.headers = {};

    // Reuse auth from app.js
    var token = sessionStorage.getItem('etcdmonitor_token');
    if (token) {
        opts.headers['Authorization'] = 'Bearer ' + token;
    }

    var resp = await fetch(url, opts);
    var data = await resp.json();

    if (resp.status === 401) {
        // Auth failed
        kvShowToast('Authentication required', 'error');
        return { error: 'Authentication required' };
    }

    return data;
}
