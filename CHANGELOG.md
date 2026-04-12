# Changelog

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
