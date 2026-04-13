// ============================================
// etcdmonitor - KV Management Module
// Tree view, CRUD operations, ACE Editor
// ============================================

// === SVG Icons ===
var KV_ICON_FOLDER = '<svg viewBox="0 0 20 20" xmlns="http://www.w3.org/2000/svg"><path d="M2 4.5A1.5 1.5 0 013.5 3h3.379a1.5 1.5 0 011.06.44l1.122 1.12A1.5 1.5 0 0010.12 5H16.5A1.5 1.5 0 0118 6.5v9a1.5 1.5 0 01-1.5 1.5h-13A1.5 1.5 0 012 15.5v-11z" fill="currentColor"/></svg>';
var KV_ICON_FOLDER_OPEN = '<svg viewBox="0 0 20 20" xmlns="http://www.w3.org/2000/svg"><path d="M2 4.5A1.5 1.5 0 013.5 3h3.379a1.5 1.5 0 011.06.44l1.122 1.12A1.5 1.5 0 0010.12 5H16.5A1.5 1.5 0 0118 6.5V8H5.5a2 2 0 00-1.904 1.385l-1.403 4.492A1 1 0 003.146 15h13.208a1.5 1.5 0 001.428-1.039l1.6-4.8A1 1 0 0018.43 8H18V6.5A1.5 1.5 0 0016.5 5h-6.379a1.5 1.5 0 01-1.06-.44L7.939 3.44A1.5 1.5 0 006.879 3H3.5A1.5 1.5 0 002 4.5v11A1.5 1.5 0 003.5 17h13a1.5 1.5 0 001.5-1.5" fill="currentColor"/></svg>';
var KV_ICON_FILE = '<svg viewBox="0 0 20 20" xmlns="http://www.w3.org/2000/svg"><path d="M4 4.5A1.5 1.5 0 015.5 3h5.879a1.5 1.5 0 011.06.44l3.122 3.12a1.5 1.5 0 01.439 1.061V15.5A1.5 1.5 0 0114.5 17h-9A1.5 1.5 0 014 15.5v-11z" fill="currentColor" opacity="0.85"/><path d="M11 3v3.5A1.5 1.5 0 0012.5 8H16" fill="none" stroke="var(--bg-card, #1c2333)" stroke-width="1.5"/></svg>';

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
    searchKeyword: '',         // current search filter keyword
    _expandedSnapshot: null,   // snapshot of _expanded states before search
    _autoRefreshTimer: null,   // 60s auto-refresh timer ID
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

    // Start 60s auto-refresh
    kvStartAutoRefresh();
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
            kvState.treeData = null;
            kvState.cache[kvState.protocol].treeData = null;
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
        kvState.treeData = null;
        kvState.cache[kvState.protocol].treeData = null;
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

    // 切换协议时清空搜索状态
    kvState.searchKeyword = '';
    kvState._expandedSnapshot = null;
    var searchInput = document.getElementById('kvSearchInput');
    if (searchInput) searchInput.value = '';
    var searchClear = document.getElementById('kvSearchClear');
    if (searchClear) searchClear.style.display = 'none';

    // Update toggle UI
    document.getElementById('kvTabV3').classList.toggle('active', proto === 'v3');
    document.getElementById('kvTabV2').classList.toggle('active', proto === 'v2');
    document.getElementById('kvProtoSlider').classList.toggle('right', proto === 'v2');

    // 切换协议时，先清理错误和树显示，避免跨协议残留
    kvHideTreeError();
    document.getElementById('kvTree').innerHTML = '';
    document.getElementById('kvTreeEmpty').style.display = 'none';

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

    // 重启自动刷新定时器（新协议）
    kvStartAutoRefresh();
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
async function kvLoadTree() {
    kvShowTreeLoading(true);
    kvHideTreeError();

    try {
        var resp = await kvFetch(kvApiBase() + '/keys');
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
    // 移除旧的 no-match 提示
    var oldNoMatch = document.getElementById('kvTreeNoMatch');
    if (oldNoMatch) oldNoMatch.remove();

    container.innerHTML = '';

    if (!kvState.treeData) {
        emptyEl.style.display = '';
        return;
    }
    emptyEl.style.display = 'none';

    // 搜索模式下预计算匹配集合
    var matchSet = null;
    if (kvState.searchKeyword) {
        matchSet = new Set();
        kvBuildMatchSet(kvState.treeData, kvState.searchKeyword, matchSet);
        // 无匹配结果
        if (matchSet.size === 0) {
            var noMatch = document.createElement('div');
            noMatch.className = 'kv-tree-no-match';
            noMatch.id = 'kvTreeNoMatch';
            noMatch.textContent = 'No matching keys found';
            container.parentElement.appendChild(noMatch);
            return;
        }
    }

    if (kvState.treeMode === 'list') {
        kvRenderListMode(container, kvState.treeData, matchSet);
    } else {
        // 渲染根节点 "/" 自身，再渲染其子节点
        var rootNode = kvState.treeData;
        rootNode.dir = true;
        if (rootNode._expanded === undefined) {
            rootNode._expanded = true; // 根节点默认展开
        }
        var rootEl = kvCreateTreeNodeEl(rootNode, 0, false, matchSet);
        container.appendChild(rootEl);
    }
}

// kvBuildMatchSet: 构建匹配节点集合（包含匹配节点自身及其所有祖先的 key）
// 如果目录自身匹配，则其所有子孙节点也全部纳入
function kvBuildMatchSet(node, keyword, matchSet) {
    var lowerKeyword = keyword.toLowerCase();
    var hasMatch = false;

    // 检查自身是否匹配（最后一段名称）
    var parts = node.key.split(kvState.separator);
    var displayName = parts[parts.length - 1] || node.key;
    var selfMatch = displayName.toLowerCase().indexOf(lowerKeyword) !== -1;

    if (selfMatch && node.dir) {
        // 目录自身匹配 → 纳入自身及全部子孙
        kvAddAllDescendants(node, matchSet);
        return true;
    }

    if (selfMatch) {
        hasMatch = true;
    }

    // 递归检查子节点
    if (node.nodes) {
        for (var i = 0; i < node.nodes.length; i++) {
            if (kvBuildMatchSet(node.nodes[i], keyword, matchSet)) {
                hasMatch = true;
            }
        }
    }

    if (hasMatch) {
        matchSet.add(node.key);
    }

    return hasMatch;
}

// kvAddAllDescendants: 将节点及其全部子孙加入 matchSet
function kvAddAllDescendants(node, matchSet) {
    matchSet.add(node.key);
    if (node.nodes) {
        for (var i = 0; i < node.nodes.length; i++) {
            kvAddAllDescendants(node.nodes[i], matchSet);
        }
    }
}

function kvRenderPathMode(container, nodes, depth, matchSet) {
    if (!nodes) return;
    nodes.forEach(function(node) {
        // 搜索模式下跳过不匹配的节点
        if (matchSet && !matchSet.has(node.key)) return;
        var el = kvCreateTreeNodeEl(node, depth, false, matchSet);
        container.appendChild(el);
    });
}

function kvRenderListMode(container, root, matchSet) {
    // Flatten all real keys (leaves + directories with values) — flat list, no hierarchy
    var items = [];
    function collect(node) {
        if (node.key !== kvState.separator) {
            // 搜索模式下，只匹配最后一段名称
            if (matchSet) {
                var parts = node.key.split(kvState.separator);
                var displayName = parts[parts.length - 1] || node.key;
                var lowerKeyword = kvState.searchKeyword.toLowerCase();
                if (displayName.toLowerCase().indexOf(lowerKeyword) === -1) {
                    // 不匹配，但仍需递归子节点
                    if (node.nodes) node.nodes.forEach(collect);
                    return;
                }
            }
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
        icon.innerHTML = node.dir ? KV_ICON_FOLDER : KV_ICON_FILE;
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

function kvCreateTreeNodeEl(node, depth, showFullPath, matchSet) {
    // 搜索模式下，目录节点如果有匹配后代则强制展开
    var isSearchMode = !!matchSet;
    var effectiveExpanded = node._expanded;
    if (isSearchMode && node.dir) {
        effectiveExpanded = true; // 搜索模式下强制展开匹配路径
    }

    var wrapper = document.createElement('div');
    wrapper.className = 'kv-tree-node';
    wrapper.dataset.key = node.key;

    // Row
    var row = document.createElement('div');
    row.className = 'kv-tree-row';
    if (kvState.selectedKey === node.key) {
        row.classList.add('selected');
    }
    row.style.paddingLeft = '8px';

    // Arrow
    var arrow = document.createElement('span');
    arrow.className = 'kv-tree-arrow';
    if (node.dir) {
        arrow.textContent = '\u25B6'; // right triangle
        if (effectiveExpanded) {
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
    if (node.dir) {
        icon.innerHTML = effectiveExpanded ? KV_ICON_FOLDER_OPEN : KV_ICON_FOLDER;
    } else {
        icon.innerHTML = KV_ICON_FILE;
    }
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
    if (node.dir && node.nodes && node.nodes.length > 0 && effectiveExpanded) {
        var children = document.createElement('div');
        children.className = 'kv-tree-children';
        kvRenderPathMode(children, node.nodes, depth + 1, matchSet);
        wrapper.appendChild(children);
    }

    return wrapper;
}

// === Tree Expand/Collapse ===
function kvToggleExpand(node, wrapperEl) {
    node._expanded = !node._expanded;
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

// === Auto Refresh (60s) ===
function kvStartAutoRefresh() {
    kvStopAutoRefresh();
    kvState._autoRefreshTimer = setInterval(function() {
        kvAutoRefreshKeys();
    }, 60000);
}

function kvStopAutoRefresh() {
    if (kvState._autoRefreshTimer) {
        clearInterval(kvState._autoRefreshTimer);
        kvState._autoRefreshTimer = null;
    }
}

async function kvAutoRefreshKeys() {
    try {
        var resp = await kvFetch(kvApiBase() + '/keys');
        if (resp.error || !resp.node) return;

        var oldTree = kvState.treeData;
        if (!oldTree) {
            kvState.treeData = resp.node;
            kvState.cache[kvState.protocol].treeData = resp.node;
            kvRenderTree();
            return;
        }

        // 检测编辑器是否有未保存的修改，如果有则静默合并但不刷新 selectedNode
        var isEditing = kvState.aceEditor && kvState.selectedNode &&
            document.getElementById('kvEditor').style.display !== 'none';

        var changed = kvMergeTreeData(oldTree, resp.node);
        if (changed) {
            kvState.cache[kvState.protocol].treeData = kvState.treeData;
            // 检查 selectedKey 是否仍然存在
            if (kvState.selectedKey && !kvFindNode(kvState.treeData, kvState.selectedKey)) {
                kvClearSelection();
            }
            kvRenderTree();
        }
    } catch (e) {
        // 静默失败，不干扰用户
    }
}

// kvMergeTreeData 将新树合并到旧树，保留 _expanded 和已缓存的 value
// 返回 true 如果树结构有变化（新增/删除节点）
function kvMergeTreeData(oldTree, newTree) {
    var changed = false;

    // 递归合并：以 newTree 的结构为准，保留 oldTree 的交互状态
    function mergeNode(oldNode, newNode) {
        // 保留 _expanded 状态
        if (oldNode._expanded !== undefined) {
            newNode._expanded = oldNode._expanded;
        }
        // 保留已缓存的 value（keys API 返回的 value 为空）
        if (oldNode.value && !newNode.value) {
            newNode.value = oldNode.value;
        }

        if (!newNode.nodes || !oldNode.nodes) {
            // 检测子节点变化
            var oldHasChildren = oldNode.nodes && oldNode.nodes.length > 0;
            var newHasChildren = newNode.nodes && newNode.nodes.length > 0;
            if (oldHasChildren !== newHasChildren) changed = true;
            return;
        }

        // 构建 old 节点的 key → node 映射
        var oldMap = {};
        for (var i = 0; i < oldNode.nodes.length; i++) {
            oldMap[oldNode.nodes[i].key] = oldNode.nodes[i];
        }

        // 检测新增和修改的节点
        for (var j = 0; j < newNode.nodes.length; j++) {
            var newChild = newNode.nodes[j];
            var oldChild = oldMap[newChild.key];
            if (oldChild) {
                mergeNode(oldChild, newChild);
                delete oldMap[newChild.key];
            } else {
                // 新增节点
                changed = true;
            }
        }

        // oldMap 中剩余的是被删除的节点
        for (var k in oldMap) {
            if (oldMap.hasOwnProperty(k)) {
                changed = true;
                break;
            }
        }
    }

    mergeNode(oldTree, newTree);

    // 用合并后的 newTree 替换 oldTree（newTree 已携带保留的状态）
    kvState.treeData = newTree;

    return changed;
}

// === Search Filter ===
// kvFilterTree: 递归判断节点自身或任一后代是否匹配关键词
// 匹配的是 key 的最后一段名称（树中显示的文本），大小写不敏感子串匹配
// 返回 true 如果该节点或任一后代匹配
function kvFilterTree(node, keyword) {
    var lowerKeyword = keyword.toLowerCase();

    // 获取节点显示名称（最后一段）
    var parts = node.key.split(kvState.separator);
    var displayName = parts[parts.length - 1] || node.key;
    var selfMatch = displayName.toLowerCase().indexOf(lowerKeyword) !== -1;

    if (selfMatch) return true;

    // 检查子节点
    if (node.nodes) {
        for (var i = 0; i < node.nodes.length; i++) {
            if (kvFilterTree(node.nodes[i], keyword)) return true;
        }
    }

    return false;
}

// kvSaveExpandedState: 进入搜索模式时保存各节点的 _expanded 状态快照
function kvSaveExpandedState() {
    var snapshot = {};
    function collect(node) {
        if (node._expanded !== undefined) {
            snapshot[node.key] = node._expanded;
        }
        if (node.nodes) {
            for (var i = 0; i < node.nodes.length; i++) {
                collect(node.nodes[i]);
            }
        }
    }
    if (kvState.treeData) collect(kvState.treeData);
    return snapshot;
}

// kvRestoreExpandedState: 退出搜索模式时恢复各节点的 _expanded 状态
function kvRestoreExpandedState(snapshot) {
    if (!snapshot) return;
    function restore(node) {
        if (snapshot.hasOwnProperty(node.key)) {
            node._expanded = snapshot[node.key];
        } else {
            delete node._expanded;
        }
        if (node.nodes) {
            for (var i = 0; i < node.nodes.length; i++) {
                restore(node.nodes[i]);
            }
        }
    }
    if (kvState.treeData) restore(kvState.treeData);
}

// kvOnSearchInput: 带 200ms debounce 的搜索入口
var _kvSearchTimer = null;
function kvOnSearchInput(value) {
    clearTimeout(_kvSearchTimer);
    _kvSearchTimer = setTimeout(function() {
        var trimmed = value.trim();

        // 进入搜索模式时保存展开状态
        if (trimmed && !kvState.searchKeyword) {
            kvState._expandedSnapshot = kvSaveExpandedState();
        }

        // 退出搜索模式时恢复展开状态
        if (!trimmed && kvState.searchKeyword) {
            kvRestoreExpandedState(kvState._expandedSnapshot);
            kvState._expandedSnapshot = null;
        }

        kvState.searchKeyword = trimmed;

        // 更新清除按钮显示
        document.getElementById('kvSearchClear').style.display = trimmed ? '' : 'none';

        kvRenderTree();
    }, 200);
}

// kvClearSearch: 清空搜索框并恢复树
function kvClearSearch() {
    document.getElementById('kvSearchInput').value = '';
    if (kvState.searchKeyword) {
        kvRestoreExpandedState(kvState._expandedSnapshot);
        kvState._expandedSnapshot = null;
    }
    kvState.searchKeyword = '';
    document.getElementById('kvSearchClear').style.display = 'none';
    kvRenderTree();
}

// === Node Selection ===
async function kvSelectNode(node) {
    kvState.selectedKey = node.key;
    kvState.selectedNode = node;

    // Update tree UI — highlight selected row (both tree and list mode)
    document.querySelectorAll('.kv-tree-row.selected, .kv-list-row.selected').forEach(function(el) {
        el.classList.remove('selected');
    });
    document.querySelectorAll('.kv-tree-node').forEach(function(wrapper) {
        if (wrapper.dataset.key === node.key) {
            var row = wrapper.querySelector('.kv-tree-row, .kv-list-row');
            if (row) row.classList.add('selected');
        }
    });

    // 根节点是虚拟容器，不 fetch 自身的值，不显示编辑器
    if (node.key === kvState.separator) {
        kvShowKeyInfo(node);
        kvHideEditor();
        return;
    }

    // 按需加载最新 value/TTL/revision（叶子和目录统一处理）
    try {
        var resp = await kvFetch(kvApiBase() + '/get?key=' + encodeURIComponent(node.key));
        if (resp.error) {
            if (resp.code === 'key_not_found') {
                if (node.dir) {
                    // 虚拟目录（V3 路径前缀，无自身 key）— 正常情况，不移除
                    kvShowKeyInfo(node);
                    kvHideEditor();
                    return;
                }
                // 叶子节点：Key 已过期（TTL），从树中移除
                kvRemoveExpiredNode(node);
                return;
            }
            // 目录节点可能没有自身的 value（虚拟目录），忽略错误
            if (node.dir) {
                kvShowKeyInfo(node);
                kvHideEditor();
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
        if (node.value) {
            kvShowEditor(node);
        } else {
            kvHideEditor();
        }
    } catch (e) {
        if (node.dir) {
            // 虚拟目录无 value，正常
            kvShowKeyInfo(node);
            kvHideEditor();
        } else {
            kvShowToast('Failed to get key: ' + e.message, 'error');
        }
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
