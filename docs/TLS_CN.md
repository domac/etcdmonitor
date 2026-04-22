← 返回 [README](../README_CN.md)

# TLS / HTTPS 指南

本文档涵盖 etcdmonitor 中 TLS 的两端：

1. **Dashboard HTTPS** — 将 Dashboard 通过 TLS 提供服务
2. **etcd 客户端 TLS（mTLS）** — 连接开启 TLS 的 etcd 集群

生产上线前，请同时过一遍 [安全检查清单](./SECURITY_CHECKLIST.md)。

---

## Dashboard HTTPS

发布包 **不** 自带证书 —— 由你在每台目标机器上本地生成自签名密钥。使用自带脚本：

```bash
# 最简形式（SAN：localhost、127.0.0.1、0.0.0.0）
./tools/gen-certs.sh

# 生产：加入用户访问 Dashboard 的主机名与 IP。
# --host 与 --ip 均可重复（见 ./tools/gen-certs.sh --help）。
./tools/gen-certs.sh \
    --host monitor.corp.local \
    --host etcd-dashboard.internal \
    --ip 10.0.1.5 \
    --ip 10.0.1.6 \
    --days 730

# 覆盖已有证书（当前会话会失效）
./tools/gen-certs.sh --force
```

脚本在项目目录写入 `certs/server.key`（`0600`）与 `certs/server.crt`（`0644`）。

在 `config.yaml` 中启用 HTTPS：

```yaml
server:
  tls_enable: true
  tls_cert: "certs/server.crt"
  tls_key:  "certs/server.key"
```

重启服务后通过 `https://<server-ip>:9090` 访问。

> `install.sh` 在 `tls_enable: true` 但证书文件缺失时拒绝启动，并给出
> 指回 `./tools/gen-certs.sh` 的一行提示。

### 使用 CA 签发证书

把 `certs/` 目录下的文件替换为你自己的（或软链到集中证书管理目录 ——
`install.sh` 尊重软链，不会 `chmod` 其目标）：

```bash
cp /path/to/your/cert.crt certs/server.crt
cp /path/to/your/cert.key certs/server.key
sudo systemctl restart etcdmonitor
```

> 自签名证书会触发浏览器告警，点击 "高级" → "继续" 可继续访问；生产请使用受信 CA 签发的证书。

---

## etcd 客户端 TLS（mTLS）

如果你的 etcd 部署启用了 `client-transport-security.client-cert-auth: true`，etcdmonitor 支持通过客户端证书连接。

### 配置

```yaml
etcd:
  endpoint: "https://10.0.1.1:2379,https://10.0.1.2:2379,https://10.0.1.3:2379"
  tls_enable: true
  tls_cert: "certs/etcd-client.pem"         # 客户端证书
  tls_key: "certs/etcd-client-key.pem"      # 客户端私钥
  tls_ca_cert: "certs/ca.pem"               # CA 证书
```

### 操作步骤

```bash
# 将 etcd 客户端证书复制到 etcdmonitor 的 certs 目录
cp /etc/etcd/ssl/etcd-client.pem     certs/etcd-client.pem
cp /etc/etcd/ssl/etcd-client-key.pem certs/etcd-client-key.pem
cp /etc/etcd/ssl/ca.pem              certs/ca.pem

# 编辑 config.yaml（设置 tls_enable: true 和证书路径）
vim config.yaml

# 重启服务
systemctl restart etcdmonitor
```

### 支持场景

| 场景 | `tls_enable` | `tls_cert` / `tls_key` | `tls_ca_cert` | `username` / `password` |
|---|---|---|---|---|
| 纯 HTTP（无鉴权） | `false` | — | — | — |
| 纯 HTTP + 密码鉴权 | `false` | — | — | ✅ |
| HTTPS + 仅 CA（校验服务端） | `true` | — | ✅ | 可选 |
| HTTPS + mTLS（客户端证书） | `true` | ✅ | ✅ | 可选 |
| HTTPS + mTLS + 密码鉴权 | `true` | ✅ | ✅ | ✅ |

> **注意：** `tls_enable: true` 时端点必须以 `https://` 开头。TLS 应用于
> 所有 etcd 连接：健康探测、指标采集、成员发现、KV 管理、鉴权。

---

## 相关文档

- [配置参考](./CONFIGURATION_CN.md)
- [API 参考](./API_CN.md)
- [安全检查清单](./SECURITY_CHECKLIST.md)
- [返回 README](../README_CN.md)

<!-- 更新此文件时，请同步更新主 README；任何 TLS 变更必须反映在此文件中。 -->
