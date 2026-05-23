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
        if (resp === null) return; // 401 已跳转
        if (resp.error) {
            kvShowTreeError(resp.error);
            if (typeof kvOnTabRequestError === 'function') kvOnTabRequestError(resp);
            return;
        }
        kvState.treeData = resp.node;
        kvState.cache[kvState.protocol].treeData = resp.node;
        // 拿到数据 → 当前 Tab 一定健康，立刻摘掉可能的 ⚠️
        if (typeof kvOnTabRequestSuccess === 'function') kvOnTabRequestSuccess();
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
// kvShowToast 显示一个顶部 toast。
//
// 签名：kvShowToast(msg)                    — 默认中性
//      kvShowToast(msg, 'success'|'error'|'info')
//      kvShowToast(msg, 'info', { action: { label, onClick }, durationMs })
//
// 第三参数（options）为可选；含 action 时 toast 右侧渲染按钮，点击后 onClick + 关闭 toast。
function kvShowToast(msg, type, options) {
    var toast = document.getElementById('kvToast');
    if (!toast) return;
    options = options || {};

    // 清空旧内容（可能有上一轮的 action button）
    toast.innerHTML = '';
    var msgSpan = document.createElement('span');
    msgSpan.textContent = msg;
    toast.appendChild(msgSpan);

    var classes = ['kv-toast'];
    if (type) classes.push(type);
    if (options.action) {
        classes.push('has-action');
        var btn = document.createElement('button');
        btn.className = 'kv-toast-action';
        btn.textContent = options.action.label || '操作';
        btn.onclick = function() {
            try { options.action.onClick && options.action.onClick(); } catch(_) {}
            toast.classList.remove('show');
            clearTimeout(_toastTimer);
        };
        toast.appendChild(btn);
    }
    toast.className = classes.join(' ');

    void toast.offsetWidth;
    toast.classList.add('show');
    clearTimeout(_toastTimer);
    var dur = (options.durationMs && options.durationMs > 0) ? options.durationMs : 3000;
    _toastTimer = setTimeout(function() {
        toast.classList.remove('show');
    }, dur);
}

// === KV Multi-Cluster Helpers ===
// activeTabID 由 kvSession 维护（见文件末尾），这里只读取。
function kvCurrentTabID() {
    return (typeof kvSession !== 'undefined' && kvSession.activeTabID) || 'default';
}

// kvAppendTabID 把当前活动 Tab 的 tab_id 拼入 URL（默认 Tab 不传，向下兼容）。
function kvAppendTabID(url) {
    var tabID = kvCurrentTabID();
    if (!tabID || tabID === 'default') return url;
    var sep = url.indexOf('?') >= 0 ? '&' : '?';
    return url + sep + 'tab_id=' + encodeURIComponent(tabID);
}

// === Fetch with Auth ===
//
// 复用 app.js 的 fetchWithAuth（含 401 → 清 token + 跳转 /login.html）。
// 自动追加 tab_id query 参数。
async function kvFetch(url, opts) {
    var fullURL = kvAppendTabID(url);
    if (typeof fetchWithAuth === 'function') {
        var resp = await fetchWithAuth(fullURL, opts);
        if (resp === null) {
            // 已经被 fetchWithAuth 跳转到登录页；返回 null 让调用方早退
            return null;
        }
        try {
            return await resp.json();
        } catch (e) {
            return { error: 'invalid response' };
        }
    }
    // 兜底：fetchWithAuth 不可用时——理论上不会发生
    opts = opts || {};
    if (!opts.headers) opts.headers = {};
    var token = sessionStorage.getItem('etcdmonitor_token');
    if (token) opts.headers['Authorization'] = 'Bearer ' + token;
    var raw = await fetch(fullURL, opts);
    if (raw.status === 401) {
        sessionStorage.removeItem('etcdmonitor_token');
        window.location.href = '/login.html';
        return null;
    }
    try { return await raw.json(); } catch (e) { return { error: 'invalid response' }; }
}

// =============================================================
// KV Multi-Cluster Tab Module (Phase C)
// =============================================================
//
// 状态机说明：
//   kvState 仍然作为"当前活动 Tab + 当前协议"的运行时视图——所有原有 KV 操作
//   读写 kvState（树、选中节点、搜索词、协议、模式）保持不变。
//
//   kvSession 是持久化层：tabs Map 中每个 Tab 都有自己独立的快照
//   （treeMode / protocol / treeData / selectedKey / searchKeyword / scrollTop / cache.v3 / cache.v2 ...），
//   切换 Tab 时把当前 kvState 写回 kvSession.tabs[oldID]，再从 kvSession.tabs[newID]
//   装载到 kvState。这样旧逻辑无需改动。
//
//   每个 Tab 内部仍然有 cache.v3 / cache.v2，与现有 kvState.cache 保持兼容。

var kvSession = {
    tabs: {},          // { [tabID]: KVTabState }
    tabOrder: [],      // 按 sort_order 顺序的 tabID 数组（含 'default' 在首位）
    activeTabID: 'default',
    pollTimer: null,   // 60s last_status 轮询 timer
    cacheTimer: null,  // 60s 缓存超时清理 timer
    toastThrottle: {}, // { [tabID + ':' + errorCode]: lastShownAtMs }
    lastFailedRequest: null  // { tabID, retry: function() } 用于"重连后自动重试上次失败"
};

// === KVTabState 工厂 ===
function kvNewTabState(meta) {
    return {
        id: meta.id,
        name: meta.name || '',
        endpoint: meta.endpoint || '',
        username: meta.username || '',
        hasPassword: !!meta.has_password,
        sortOrder: typeof meta.sort_order === 'number' ? meta.sort_order : 0,
        lastStatus: meta.last_status || 'unknown',
        lastError: meta.last_error || '',
        isDefault: !!meta.is_default,
        // 视图状态——切到本 Tab 时装载到 kvState
        protocol: 'v3',
        treeMode: 'tree',
        treeData: null,
        selectedKey: null,
        selectedNode: null,
        searchKeyword: '',
        scrollTop: 0,
        cache: {
            v3: { treeData: null, selectedKey: null, selectedNode: null },
            v2: { treeData: null, selectedKey: null, selectedNode: null }
        },
        lastActiveAt: Date.now(),
        loaded: false  // 是否已经加载过 keys（首次激活才发起请求）
    };
}

// === 状态快照与装载 ===

// 把当前 kvState 写回到指定 Tab 的 KVTabState（切 Tab 时调）
function kvSnapshotKVStateTo(tabState) {
    if (!tabState) return;
    tabState.protocol = kvState.protocol;
    tabState.treeMode = kvState.treeMode;
    tabState.treeData = kvState.treeData;
    tabState.selectedKey = kvState.selectedKey;
    tabState.selectedNode = kvState.selectedNode;
    tabState.searchKeyword = kvState.searchKeyword;
    // cache 直接共享引用即可（kvState.cache 与 tabState.cache 是同一对象只在切换瞬间换出）
    tabState.cache = kvState.cache;
    var container = document.getElementById('kvTreeContainer');
    if (container) tabState.scrollTop = container.scrollTop;
    tabState.lastActiveAt = Date.now();
}

// 从 KVTabState 装载到 kvState（切到该 Tab 时调）
function kvLoadKVStateFrom(tabState) {
    if (!tabState) return;
    kvState.protocol = tabState.protocol || 'v3';
    kvState.treeMode = tabState.treeMode || 'tree';
    kvState.treeData = tabState.treeData;
    kvState.selectedKey = tabState.selectedKey;
    kvState.selectedNode = tabState.selectedNode;
    kvState.searchKeyword = tabState.searchKeyword || '';
    kvState.cache = tabState.cache || {
        v3: { treeData: null, selectedKey: null, selectedNode: null },
        v2: { treeData: null, selectedKey: null, selectedNode: null }
    };
    tabState.lastActiveAt = Date.now();
}

// === 启动加载 Tab 列表 ===
async function kvLoadTabs() {
    var resp = await kvFetch('/api/kv/tabs');
    if (resp === null || !resp.tabs) {
        // 401 跳转或后端异常——降级为只显示默认 Tab
        kvSession.tabs = { 'default': kvNewTabState({ id: 'default', name: 'default', is_default: true }) };
        kvSession.tabOrder = ['default'];
        kvSession.activeTabID = 'default';
        kvRenderTabBar();
        return;
    }
    var newTabs = {};
    var newOrder = [];
    for (var i = 0; i < resp.tabs.length; i++) {
        var meta = resp.tabs[i];
        // 复用既有 KVTabState（保留 treeData 等运行时数据）
        var existing = kvSession.tabs[meta.id];
        if (existing) {
            existing.name = meta.name;
            existing.endpoint = meta.endpoint;
            existing.username = meta.username;
            existing.hasPassword = !!meta.has_password;
            existing.lastStatus = meta.last_status || 'unknown';
            existing.lastError = meta.last_error || '';
            existing.sortOrder = meta.sort_order;
            newTabs[meta.id] = existing;
        } else {
            newTabs[meta.id] = kvNewTabState(meta);
        }
        newOrder.push(meta.id);
    }
    kvSession.tabs = newTabs;
    kvSession.tabOrder = newOrder;
    if (!kvSession.tabs[kvSession.activeTabID]) {
        kvSession.activeTabID = 'default';
    }
    kvRenderTabBar();
    kvUpdateAddBtnState();
}

// === 渲染 Tab Bar ===
function kvRenderTabBar() {
    var bar = document.getElementById('kvClusterTabBar');
    if (!bar) return;
    bar.innerHTML = '';
    for (var i = 0; i < kvSession.tabOrder.length; i++) {
        var id = kvSession.tabOrder[i];
        var tab = kvSession.tabs[id];
        if (!tab) continue;
        bar.appendChild(kvBuildTabElement(tab));
    }
}

function kvBuildTabElement(tab) {
    var el = document.createElement('div');
    el.className = 'kv-cluster-tab';
    if (tab.id === kvSession.activeTabID) el.classList.add('active');
    if (tab.isDefault) el.classList.add('is-default');
    el.dataset.tabId = tab.id;
    el.setAttribute('role', 'tab');
    el.setAttribute('aria-selected', tab.id === kvSession.activeTabID ? 'true' : 'false');
    el.title = tab.endpoint + (tab.lastError ? '\n\n' + tab.lastError : '');

    if (tab.lastStatus === 'error') {
        var warn = document.createElement('span');
        warn.className = 'kv-tab-warning';
        warn.textContent = '⚠';
        warn.setAttribute('aria-label', 'Connection error');
        el.appendChild(warn);
    }

    var name = document.createElement('span');
    name.className = 'kv-tab-name';
    name.textContent = tab.name || tab.endpoint || tab.id;
    el.appendChild(name);

    if (!tab.isDefault) {
        var close = document.createElement('button');
        close.className = 'kv-tab-close';
        close.innerHTML = '&times;';
        close.setAttribute('aria-label', 'Close tab');
        close.title = '关闭该 Tab';
        close.onclick = function(e) {
            e.stopPropagation();
            kvCloseTab(tab.id);
        };
        el.appendChild(close);
    }

    el.onclick = function() { kvActivateTab(tab.id); };

    // 拖拽——仅非默认 Tab 可拖
    if (!tab.isDefault) {
        el.draggable = true;
        el.ondragstart = function(e) { kvOnTabDragStart(e, tab.id); };
        el.ondragend = function(e) { kvOnTabDragEnd(e, tab.id); };
    }
    el.ondragover = function(e) { kvOnTabDragOver(e, tab.id); };
    el.ondragleave = function(e) { kvOnTabDragLeave(e, tab.id); };
    el.ondrop = function(e) { kvOnTabDrop(e, tab.id); };

    return el;
}

function kvUpdateAddBtnState() {
    var btn = document.getElementById('kvAddTabBtn');
    if (!btn) return;
    // 非默认 Tab 数量 = tabOrder 总数 - 1
    var nonDefault = Math.max(0, kvSession.tabOrder.length - 1);
    if (nonDefault >= 10) {
        btn.disabled = true;
        btn.title = '已达每用户 10 个上限';
    } else {
        btn.disabled = false;
        btn.title = '添加远程 etcd 集群';
    }
}

// === Tab 切换 ===
async function kvActivateTab(tabID) {
    if (!kvSession.tabs[tabID]) return;
    if (tabID === kvSession.activeTabID) return;

    // (1) 把当前 kvState 写回旧 Tab
    var oldTab = kvSession.tabs[kvSession.activeTabID];
    kvSnapshotKVStateTo(oldTab);

    // (2) 切到新 Tab
    kvSession.activeTabID = tabID;
    var newTab = kvSession.tabs[tabID];
    kvLoadKVStateFrom(newTab);

    kvRenderTabBar();

    // (3) 同步 UI 控件状态（V3/V2 toggle、Tree/List toggle、搜索框、右侧编辑器面板）
    kvSyncToggleControls();
    kvSyncSearchInput();
    kvSyncEditorPanel();

    // (4) 刷新 Info Bar（重新调 connect 拿版本/leader/dbsize）
    kvRefreshInfoBar();

    // (5) 渲染或加载树
    if (newTab.treeData) {
        // 已有缓存，直接渲染
        kvRenderTree();
        kvRestoreScrollTop(newTab.scrollTop);
    } else if (!newTab.loaded || newTab.lastStatus === 'error') {
        // 首次激活或上次失败 → 重新加载
        kvLoadTreeForActive();
    } else {
        // 已加载但缓存被清（5min 超时）→ 重新加载
        kvLoadTreeForActive();
    }
}

function kvSyncToggleControls() {
    // V3/V2
    var protoV3 = document.getElementById('kvTabV3');
    var protoV2 = document.getElementById('kvTabV2');
    var protoSlider = document.getElementById('kvProtoSlider');
    if (protoV3 && protoV2) {
        protoV3.classList.toggle('active', kvState.protocol === 'v3');
        protoV2.classList.toggle('active', kvState.protocol === 'v2');
        if (protoSlider) protoSlider.style.transform = kvState.protocol === 'v2' ? 'translateX(100%)' : 'translateX(0)';
    }
    // Tree/List
    var viewT = document.getElementById('kvViewTree');
    var viewL = document.getElementById('kvViewList');
    var viewSlider = document.getElementById('kvViewSlider');
    if (viewT && viewL) {
        viewT.classList.toggle('active', kvState.treeMode === 'tree');
        viewL.classList.toggle('active', kvState.treeMode === 'list');
        if (viewSlider) viewSlider.style.transform = kvState.treeMode === 'list' ? 'translateX(100%)' : 'translateX(0)';
    }
}

function kvSyncSearchInput() {
    var input = document.getElementById('kvSearchInput');
    var clear = document.getElementById('kvSearchClear');
    if (input) input.value = kvState.searchKeyword || '';
    if (clear) clear.style.display = kvState.searchKeyword ? '' : 'none';
}

// 切 Tab 时把右侧编辑器面板（key 路径 / 元信息 / ACE 编辑器 / 工具栏）
// 重新渲染为新 Tab 的 selectedNode；若新 Tab 没有选中节点，回到占位符状态。
//
// 没有这个函数时，切 Tab 只换了 kvState 里的值，DOM 仍是上一个 Tab 的内容。
function kvSyncEditorPanel() {
    var node = kvState.selectedNode;
    if (!node) {
        // 新 Tab 还没有任何选择 → 回到占位符
        var info = document.getElementById('kvKeyInfo');
        if (info) info.style.display = 'none';
        kvHideEditor();
        return;
    }
    kvShowKeyInfo(node);
    // 与 kvSelectNode 行为一致：仅在节点确实有 value 时打开编辑器
    if (node.value) {
        kvShowEditor(node);
    } else {
        kvHideEditor();
    }
}

function kvRefreshInfoBar() {
    kvFetch(kvApiBase() + '/connect', { method: 'POST' }).then(function(resp) {
        if (resp === null) return;
        if (resp && !resp.error) {
            var v = document.getElementById('kvVersion');
            var l = document.getElementById('kvLeader');
            var s = document.getElementById('kvDBSize');
            if (v) v.textContent = resp.version || '-';
            if (l) l.textContent = resp.name || '-';
            if (s) s.textContent = resp.size_str || '-';
            // connect 成功也算活跃，摘掉可能的 ⚠️
            if (typeof kvOnTabRequestSuccess === 'function') kvOnTabRequestSuccess();
        } else {
            // 显示占位
            var v2 = document.getElementById('kvVersion');
            var l2 = document.getElementById('kvLeader');
            var s2 = document.getElementById('kvDBSize');
            if (v2) v2.textContent = '-';
            if (l2) l2.textContent = '-';
            if (s2) s2.textContent = '-';
            kvOnTabRequestError(resp);
        }
    });
}

function kvRestoreScrollTop(top) {
    var container = document.getElementById('kvTreeContainer');
    if (container && typeof top === 'number') container.scrollTop = top;
}

async function kvLoadTreeForActive() {
    var tab = kvSession.tabs[kvSession.activeTabID];
    if (!tab) return;
    tab.loaded = true;
    tab.lastActiveAt = Date.now();
    if (typeof kvLoadTree === 'function') {
        await kvLoadTree();
    }
}

// === 缓存超时清理（5 min 不活跃释放 treeData，节省内存）===
function kvStartCacheTimer() {
    if (kvSession.cacheTimer) return;
    kvSession.cacheTimer = setInterval(function() {
        var now = Date.now();
        var fiveMin = 5 * 60 * 1000;
        for (var id in kvSession.tabs) {
            if (id === kvSession.activeTabID) continue;
            var tab = kvSession.tabs[id];
            if (tab.lastActiveAt && (now - tab.lastActiveAt) > fiveMin) {
                tab.treeData = null;
                if (tab.cache) {
                    tab.cache.v3 = { treeData: null, selectedKey: null, selectedNode: null };
                    tab.cache.v2 = { treeData: null, selectedKey: null, selectedNode: null };
                }
            }
        }
    }, 60 * 1000);
}

// === 60s 轮询 last_status ===
function kvStartStatusPolling() {
    if (kvSession.pollTimer) return;
    kvSession.pollTimer = setInterval(async function() {
        var resp = await kvFetch('/api/kv/tabs');
        if (resp === null) return; // 401 已跳转
        if (!resp.tabs) return;
        var changed = false;
        for (var i = 0; i < resp.tabs.length; i++) {
            var meta = resp.tabs[i];
            var tab = kvSession.tabs[meta.id];
            if (!tab) continue;
            var prev = tab.lastStatus;
            var now = meta.last_status || 'unknown';
            if (prev !== now) {
                tab.lastStatus = now;
                tab.lastError = meta.last_error || '';
                changed = true;
                if (now === 'error' && !tab.isDefault) {
                    // ok→error：触发 toast
                    var code = (tab.lastError || '').toLowerCase().indexOf('auth_failed') >= 0
                        ? 'KV_TAB_AUTH_FAILED' : 'KV_TAB_UNREACHABLE';
                    kvShowTabErrorToast(tab.id, code);
                }
                // error→ok：仅刷新 ⚠️，不弹 toast
            }
        }
        if (changed) kvRenderTabBar();
    }, 60 * 1000);
}

// === Toast 节流 + 触发 ===
function kvShowTabErrorToast(tabID, errorCode) {
    var key = tabID + ':' + errorCode;
    var now = Date.now();
    if (kvSession.toastThrottle[key] && (now - kvSession.toastThrottle[key]) < 60 * 1000) {
        return; // 60 秒内已弹过同 tab+code 的 toast
    }
    kvSession.toastThrottle[key] = now;

    var tab = kvSession.tabs[tabID];
    var name = (tab && tab.name) || tabID;
    var msg = (errorCode === 'KV_TAB_AUTH_FAILED')
        ? '凭据已失效 "' + name + '"'
        : '无法连接 "' + name + '"';
    kvShowToast(msg, 'error', {
        durationMs: 6000,
        action: {
            label: '重新输入',
            onClick: function() { kvOpenReconnectDialog(tabID); }
        }
    });
}

// 业务请求路径上：检查响应是否含 KV_TAB_UNREACHABLE / KV_TAB_AUTH_FAILED 错误码并触发 ⚠️ + toast。
function kvOnTabRequestError(resp) {
    if (!resp || !resp.code) return;
    var tabID = kvCurrentTabID();
    if (tabID === 'default') return;
    if (resp.code === 'KV_TAB_UNREACHABLE' || resp.code === 'KV_TAB_AUTH_FAILED') {
        var tab = kvSession.tabs[tabID];
        if (tab) {
            tab.lastStatus = 'error';
            tab.lastError = (resp.error || '') + ' (' + resp.code + ')';
            kvRenderTabBar();
        }
        kvShowTabErrorToast(tabID, resp.code);
    } else if (resp.code === 'KV_TAB_NOT_FOUND') {
        // tab_id 不在 DB 里——可能其他会话删掉了；重新拉列表
        kvLoadTabs();
    }
}

// 业务请求成功路径上：立即把当前 Tab 的 lastStatus 同步为 ok 并刷新 ⚠️。
//
// 后端 markTabOK 已经把 DB 写为 "ok"，但前端 kvSession.tabs[id].lastStatus
// 仍是上一次 /api/kv/tabs poll 的值（最长滞后 60 秒）。这里就地纠正，
// 让用户在第一次拿到数据时立刻看到 ⚠️ 消失。
function kvOnTabRequestSuccess() {
    var tabID = kvCurrentTabID();
    if (!tabID || tabID === 'default') return;
    var tab = kvSession.tabs[tabID];
    if (!tab) return;
    if (tab.lastStatus === 'error' || tab.lastError) {
        tab.lastStatus = 'ok';
        tab.lastError = '';
        // 同时清掉 toast 节流，让真的下次失败能立刻提示
        delete kvSession.toastThrottle[tabID + ':KV_TAB_UNREACHABLE'];
        delete kvSession.toastThrottle[tabID + ':KV_TAB_AUTH_FAILED'];
        kvRenderTabBar();
    }
}

// === 添加 Tab 对话框 ===
function kvOpenAddTabDialog() {
    var dlg = document.getElementById('kvAddTabDialog');
    if (!dlg) return;
    document.getElementById('kvAddTabEndpoint').value = '';
    document.getElementById('kvAddTabName').value = '';
    document.getElementById('kvAddTabUsername').value = '';
    document.getElementById('kvAddTabPassword').value = '';
    document.getElementById('kvAddTabSchemeHint').style.display = 'none';
    document.getElementById('kvAddTabHttpsWarn').style.display = 'none';
    document.getElementById('kvAddTabError').style.display = 'none';
    document.getElementById('kvAddTabSubmit').disabled = false;
    document.getElementById('kvAddTabSubmit').textContent = '确定';
    dlg.style.display = 'flex';
    setTimeout(function() {
        var ep = document.getElementById('kvAddTabEndpoint');
        if (ep) ep.focus();
    }, 50);
    // 当前对话框模式：'add' 或 'reconnect'
    dlg.dataset.mode = 'add';
    dlg.dataset.tabId = '';
}

function kvCloseAddTabDialog() {
    var dlg = document.getElementById('kvAddTabDialog');
    if (dlg) dlg.style.display = 'none';
}

function kvOnAddTabEndpointInput() {
    var ep = document.getElementById('kvAddTabEndpoint').value.trim();
    var hint = document.getElementById('kvAddTabSchemeHint');
    var warn = document.getElementById('kvAddTabHttpsWarn');
    if (!ep) {
        hint.style.display = 'none';
        warn.style.display = 'none';
        return;
    }
    var lower = ep.toLowerCase();
    if (lower.indexOf('http://') === 0) {
        hint.style.display = 'none';
        warn.style.display = 'none';
    } else if (lower.indexOf('https://') === 0) {
        hint.style.display = 'none';
        warn.style.display = '';
    } else {
        hint.textContent = '仅支持 http:// 或 https:// 开头的地址';
        hint.style.display = '';
        warn.style.display = 'none';
    }
}

function kvOnAddTabEndpointBlur() {
    var ep = document.getElementById('kvAddTabEndpoint').value.trim();
    var nameInput = document.getElementById('kvAddTabName');
    if (!ep || !nameInput) return;
    if (!nameInput.value) {
        // 解析 host 作为占位提示
        var host = kvParseHost(ep);
        if (host) nameInput.placeholder = host;
    }
}

function kvParseHost(endpoint) {
    try {
        var u = new URL(endpoint);
        return u.hostname || '';
    } catch (e) {
        var s = endpoint.replace(/^https?:\/\//i, '');
        var i = s.search(/[:\/]/);
        return i >= 0 ? s.substring(0, i) : s;
    }
}

async function kvSubmitAddTab() {
    var dlg = document.getElementById('kvAddTabDialog');
    var mode = dlg.dataset.mode || 'add';
    var endpoint = document.getElementById('kvAddTabEndpoint').value.trim();
    var name = document.getElementById('kvAddTabName').value.trim();
    var username = document.getElementById('kvAddTabUsername').value;
    var password = document.getElementById('kvAddTabPassword').value;
    var errBox = document.getElementById('kvAddTabError');
    var btn = document.getElementById('kvAddTabSubmit');

    errBox.style.display = 'none';

    if (!endpoint) {
        errBox.textContent = 'Endpoint 不能为空';
        errBox.style.display = '';
        return;
    }
    var lower = endpoint.toLowerCase();
    if (lower.indexOf('http://') !== 0 && lower.indexOf('https://') !== 0) {
        errBox.textContent = '仅支持 http:// 或 https:// 开头的地址';
        errBox.style.display = '';
        return;
    }

    btn.disabled = true;
    btn.textContent = '校验中...';

    if (mode === 'reconnect') {
        await kvSubmitReconnect(dlg.dataset.tabId, endpoint, name, username, password, errBox, btn);
        return;
    }

    var resp = await kvFetch('/api/kv/tabs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
            endpoint: endpoint,
            name: name,
            username: username,
            password: password
        })
    });
    btn.disabled = false;
    btn.textContent = '确定';
    if (resp === null) return; // 401 已跳转
    if (resp.error || resp.code) {
        if (resp.code === 'KV_TAB_BELONGS_TO_DEFAULT') {
            // 友好 info 提示——不关对话框
            errBox.style.display = 'none';
            kvShowToast(resp.message || ('该地址属于默认集群（' + (resp.matched_member_url || '') + '）'),
                'info', { durationMs: 6000 });
            return;
        }
        errBox.textContent = resp.message || resp.error || resp.code;
        errBox.style.display = '';
        return;
    }
    if (resp.warning === 'degraded_member_check') {
        if (!confirm('警告：默认集群当前不可达，无法准确比对成员。继续添加？')) {
            return;
        }
    }
    // 成功——关闭对话框、刷新 Tab 列表、激活新 Tab
    kvCloseAddTabDialog();
    await kvLoadTabs();
    if (resp.tab && resp.tab.id) {
        kvActivateTab(resp.tab.id);
    }
    kvShowToast('Tab 添加成功', 'success');
}

// === 重连对话框 ===
function kvOpenReconnectDialog(tabID) {
    var tab = kvSession.tabs[tabID];
    if (!tab || tab.isDefault) return;
    kvOpenAddTabDialog();
    var dlg = document.getElementById('kvAddTabDialog');
    dlg.dataset.mode = 'reconnect';
    dlg.dataset.tabId = tabID;
    document.getElementById('kvAddTabTitle').textContent = '重新输入凭据：' + (tab.name || tabID);
    document.getElementById('kvAddTabEndpoint').value = tab.endpoint || '';
    document.getElementById('kvAddTabName').value = tab.name || '';
    document.getElementById('kvAddTabUsername').value = tab.username || '';
    document.getElementById('kvAddTabPassword').value = '';
    kvOnAddTabEndpointInput();
}

async function kvSubmitReconnect(tabID, endpoint, name, username, password, errBox, btn) {
    // 先 test
    var testResp = await kvFetch('/api/kv/tabs/' + encodeURIComponent(tabID) + '/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: username, password: password })
    });
    if (testResp === null) return;
    if (testResp.status !== 'ok') {
        btn.disabled = false;
        btn.textContent = '确定';
        errBox.textContent = '连接测试失败：' + (testResp.error || testResp.status);
        errBox.style.display = '';
        return;
    }

    // 再 PATCH 落库
    var patchBody = { endpoint: endpoint, name: name, username: username };
    if (password) patchBody.password = password;
    var patchResp = await kvFetch('/api/kv/tabs/' + encodeURIComponent(tabID), {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patchBody)
    });
    btn.disabled = false;
    btn.textContent = '确定';
    if (patchResp === null) return;
    if (patchResp.error || patchResp.code) {
        errBox.textContent = patchResp.message || patchResp.error || patchResp.code;
        errBox.style.display = '';
        return;
    }
    // 成功
    kvCloseAddTabDialog();
    var tab = kvSession.tabs[tabID];
    if (tab) {
        tab.lastStatus = 'ok';
        tab.lastError = '';
        // 清节流，让下次失联能立刻提示
        delete kvSession.toastThrottle[tabID + ':KV_TAB_UNREACHABLE'];
        delete kvSession.toastThrottle[tabID + ':KV_TAB_AUTH_FAILED'];
    }
    await kvLoadTabs();
    kvShowToast('凭据已更新，正在重试...', 'info');
    // 自动重试上次失败的请求
    if (kvSession.lastFailedRequest && kvSession.lastFailedRequest.tabID === tabID) {
        var retry = kvSession.lastFailedRequest.retry;
        kvSession.lastFailedRequest = null;
        if (typeof retry === 'function') {
            try { retry(); } catch (_) {}
        }
    } else {
        // 默认重新加载当前活动 Tab 的 keys
        if (kvSession.activeTabID === tabID) {
            kvLoadTreeForActive();
        }
    }
}

// === 关闭 Tab（含确认对话框 + "不再提示" localStorage）===
var _kvPendingCloseTabID = null;

function kvCloseTab(tabID) {
    var tab = kvSession.tabs[tabID];
    if (!tab || tab.isDefault) return;
    var pref = '';
    try { pref = localStorage.getItem('kv_close_tab_confirm') || ''; } catch (_) {}
    if (pref === 'never') {
        kvDoCloseTab(tabID);
        return;
    }
    _kvPendingCloseTabID = tabID;
    var dlg = document.getElementById('kvCloseConfirmDialog');
    var text = document.getElementById('kvCloseConfirmText');
    var ck = document.getElementById('kvCloseConfirmDontAsk');
    if (text) text.textContent = '确定关闭 Tab "' + (tab.name || tabID) +
        '"？此操作会从数据库删除该连接配置，下次登录将不再加载。';
    if (ck) ck.checked = false;
    if (dlg) dlg.style.display = 'flex';
}

function kvCancelCloseTab() {
    _kvPendingCloseTabID = null;
    var dlg = document.getElementById('kvCloseConfirmDialog');
    if (dlg) dlg.style.display = 'none';
}

async function kvConfirmCloseTab() {
    var tabID = _kvPendingCloseTabID;
    var ck = document.getElementById('kvCloseConfirmDontAsk');
    if (ck && ck.checked) {
        try {
            localStorage.setItem('kv_close_tab_confirm', 'never');
            kvShowToast('已开启免确认关闭。如需恢复，请清除浏览器数据', 'info', { durationMs: 5000 });
        } catch (_) {}
    }
    var dlg = document.getElementById('kvCloseConfirmDialog');
    if (dlg) dlg.style.display = 'none';
    _kvPendingCloseTabID = null;
    if (tabID) await kvDoCloseTab(tabID);
}

async function kvDoCloseTab(tabID) {
    var resp = await kvFetch('/api/kv/tabs/' + encodeURIComponent(tabID), { method: 'DELETE' });
    if (resp === null) return; // 401 已跳转
    // 即使 resp 含 error 也尝试本地清理（删的可能只是远端孤儿）
    var idx = kvSession.tabOrder.indexOf(tabID);
    delete kvSession.tabs[tabID];
    if (idx >= 0) kvSession.tabOrder.splice(idx, 1);
    if (kvSession.activeTabID === tabID) {
        // 切到左侧 Tab（默认 Tab 兜底）
        var newActive = (idx > 0 && kvSession.tabOrder[idx - 1])
            ? kvSession.tabOrder[idx - 1]
            : 'default';
        kvSession.activeTabID = '__placeholder__'; // 让 kvActivateTab 不早退
        kvActivateTab(newActive);
    } else {
        kvRenderTabBar();
    }
    kvUpdateAddBtnState();
}

// === 拖拽排序 ===
var _kvDragSrcID = null;

function kvOnTabDragStart(e, tabID) {
    _kvDragSrcID = tabID;
    if (e.dataTransfer) {
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', tabID); // 某些浏览器需要
    }
    e.currentTarget.classList.add('dragging');
}
function kvOnTabDragEnd(e, tabID) {
    e.currentTarget.classList.remove('dragging');
    document.querySelectorAll('.kv-cluster-tab').forEach(function(el) {
        el.classList.remove('drop-target-before', 'drop-target-after');
    });
    _kvDragSrcID = null;
}
function kvOnTabDragOver(e, tabID) {
    if (!_kvDragSrcID || _kvDragSrcID === tabID) return;
    var tab = kvSession.tabs[tabID];
    var src = kvSession.tabs[_kvDragSrcID];
    if (!tab || !src) return;
    // 阻止把非默认 Tab 拖到默认 Tab 左侧——即对默认 Tab 不能 drop-before
    e.preventDefault();
    var rect = e.currentTarget.getBoundingClientRect();
    var midX = rect.left + rect.width / 2;
    var before = e.clientX < midX;
    if (tab.isDefault && before) return; // 阻止
    e.currentTarget.classList.toggle('drop-target-before', before);
    e.currentTarget.classList.toggle('drop-target-after', !before);
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
}
function kvOnTabDragLeave(e, tabID) {
    e.currentTarget.classList.remove('drop-target-before', 'drop-target-after');
}
function kvOnTabDrop(e, tabID) {
    e.preventDefault();
    e.currentTarget.classList.remove('drop-target-before', 'drop-target-after');
    if (!_kvDragSrcID || _kvDragSrcID === tabID) { _kvDragSrcID = null; return; }
    var tab = kvSession.tabs[tabID];
    if (!tab) return;
    var rect = e.currentTarget.getBoundingClientRect();
    var midX = rect.left + rect.width / 2;
    var before = e.clientX < midX;
    if (tab.isDefault && before) { _kvDragSrcID = null; return; }

    // 把 _kvDragSrcID 从原位置移除，插入到目标位置
    var srcID = _kvDragSrcID;
    _kvDragSrcID = null;
    var srcIdx = kvSession.tabOrder.indexOf(srcID);
    if (srcIdx < 0) return;
    kvSession.tabOrder.splice(srcIdx, 1);
    var dstIdx = kvSession.tabOrder.indexOf(tabID);
    if (!before) dstIdx += 1;
    kvSession.tabOrder.splice(dstIdx, 0, srcID);
    kvRenderTabBar();
    kvSubmitOrder();
}

async function kvSubmitOrder() {
    // 默认 Tab 必须排首位——若用户拖动让它不在首位，强制纠正
    if (kvSession.tabOrder[0] !== 'default') {
        var i = kvSession.tabOrder.indexOf('default');
        if (i > 0) {
            kvSession.tabOrder.splice(i, 1);
            kvSession.tabOrder.unshift('default');
            kvRenderTabBar();
        }
    }
    var resp = await kvFetch('/api/kv/tabs/order', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: kvSession.tabOrder })
    });
    if (resp === null) return;
    if (resp.error || resp.code) {
        kvShowToast('排序保存失败：' + (resp.message || resp.code), 'error');
        // 回滚——重新拉
        kvLoadTabs();
    }
}

// === 启动入口（在 kvInit 内调用）===
async function kvInitMultiClusterTabs() {
    // 先确保有一个默认 Tab 作为兜底
    if (!kvSession.tabs['default']) {
        kvSession.tabs['default'] = kvNewTabState({
            id: 'default', name: 'default', is_default: true
        });
        kvSession.tabOrder = ['default'];
    }
    await kvLoadTabs();
    kvStartStatusPolling();
    kvStartCacheTimer();
}

// 暴露给原有 kvInit 调用——hook 在 kvInit 末尾
(function patchKvInit() {
    if (typeof kvInit !== 'function') return;
    var orig = kvInit;
    window.kvInit = function() {
        orig.apply(this, arguments);
        // kvInit 中已有"已初始化"守卫，多次调用安全
        kvInitMultiClusterTabs();
    };
})();
