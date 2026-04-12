# Changelog

## [0.3.2] - 2026-04-12

### Added

- 监控面板可配置：Dashboard 新增齿轮按钮（⚙），点击弹出面板配置窗口
- 面板可见性选择：勾选/取消勾选控制 18 个监控面板的显示与隐藏，默认全选
- 面板拖拽排序：同分区内支持鼠标拖拽排序，禁止跨分区拖拽
- 保存与重置：配置窗口提供"Save"和"Reset"按钮，重置恢复默认（全选、原始顺序）
- 用户级持久化：面板配置与登录用户绑定，同一用户不同设备/浏览器看到相同配置
- 后端用户偏好存储：`internal/prefs/` 包，JSON 文件存储（`data/user-prefs/<username>.json`）
- 新增 API：`GET /api/user/panel-config`、`PUT /api/user/panel-config`，受认证中间件保护
- 免认证模式降级：未启用 etcd 认证时，面板配置存储在浏览器 localStorage
- 分区自动隐藏：分区内全部面板隐藏时，分区标题自动隐藏

### Changed

- 面板 HTML 结构添加 `data-panel-id` 和 `data-section` 属性，支持动态渲染
- `initAllCharts()` 仅对可见面板初始化 ECharts 实例，隐藏面板自动 dispose 释放资源
- `api.New()` 签名扩展，新增 `prefsStore` 参数注入

## [0.3.0] - 2026-04-12

### Added

- Dashboard 登录认证：etcd 启用认证时，运维人员需通过登录页验证身份才能访问大盘
- 启动时自动检测 etcd 认证状态，零配置，无需手动声明
- 登录页面（`web/login.html`），支持 dark/light 主题
- 会话管理：内存会话 + 可配置超时（`server.session_timeout`，默认 3600 秒）
- API 认证中间件：认证模式下 `/api/*` 接口受保护，未认证返回 401
- Dashboard 登出按钮（仅认证模式显示）
- 新增 API：`POST /api/auth/login`、`POST /api/auth/logout`、`GET /api/auth/status`
- 双轨认证：同时支持 `Authorization: Bearer <token>` 和 Cookie

### Changed

- 配置字段 `auth_enable` 重命名为 `discovery_via_ap`
- Logger 全局函数增加 nil 安全检查
- 前端所有 API 请求统一通过 `fetchWithAuth` 包装，401 自动跳转登录页

### Security

- 会话令牌：`crypto/rand` 生成 256 位随机数
- Cookie 属性：`HttpOnly`、`SameSite=Lax`、TLS 时 `Secure`
- 登录凭据一次性验证，不存储在会话中，密码不记录日志
- 过期会话后台自动清理
