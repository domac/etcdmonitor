# etcd TLS 证书工具

一键生成 etcd 集群所需的全套 TLS 证书，并配置 etcd + etcdmonitor 安全通信。

## 生成证书

### 前置条件

- Linux 服务器（CentOS / Ubuntu / Debian 等）
- OpenSSL >= 1.1.1

```bash
# CentOS / RHEL
yum install -y openssl

# Ubuntu / Debian
apt install -y openssl
```

### 快速开始

```bash
cd tools/etcd_certs

# 单节点（开发/测试）
./generate.sh

# 三节点集群
./generate.sh -n 10.0.1.1,10.0.1.2,10.0.1.3

# 自定义有效期（2 年）和输出目录
./generate.sh -n 10.0.1.1,10.0.1.2,10.0.1.3 -d 730 -o /etc/etcd/ssl
```

### 参数说明

| 参数 | 说明 | 默认值 |
|---|---|---|
| `-n, --nodes` | etcd 节点 IP 列表，逗号分隔 | `127.0.0.1` |
| `-d, --days` | 证书有效期（天） | `365` |
| `-o, --output` | 证书输出目录 | `./output/` |

### 生成的文件

```
output/
├── ca.pem              # CA 根证书（分发给所有节点和客户端）
├── ca-key.pem          # CA 私钥（仅用于签发证书，妥善保管）
├── server.pem          # etcd 服务端证书
├── server-key.pem      # etcd 服务端私钥
├── peer.pem            # etcd 节点间通信证书
├── peer-key.pem        # etcd 节点间通信私钥
├── client.pem          # 客户端证书（etcdctl / etcdmonitor 使用）
└── client-key.pem      # 客户端私钥
```

> **安全提示：** 私钥文件（`*-key.pem`）权限为 `600`，仅 owner 可读。CA 私钥签发完成后建议离线保存，不要留在生产节点上。

---

## 配置 etcd

### 分发证书

将证书复制到每个 etcd 节点：

```bash
# 在每个 etcd 节点上
mkdir -p /etc/etcd/ssl
scp output/{ca,server,peer}.pem output/{server,peer}-key.pem root@<node>:/etc/etcd/ssl/
chmod 600 /etc/etcd/ssl/*-key.pem
```

### etcd 配置文件

以下为 etcd 3.4+ 的 YAML 配置格式（`/etc/etcd/etcd.conf.yml`），以三节点集群为例：

```yaml
name: 'etcd-1'
data-dir: '/var/lib/etcd'

# 客户端监听地址（HTTPS）
listen-client-urls: 'https://10.0.1.1:2379,https://127.0.0.1:2379'
advertise-client-urls: 'https://10.0.1.1:2379'

# 节点间通信地址（HTTPS）
listen-peer-urls: 'https://10.0.1.1:2380'
initial-advertise-peer-urls: 'https://10.0.1.1:2380'

# 集群成员
initial-cluster: 'etcd-1=https://10.0.1.1:2380,etcd-2=https://10.0.1.2:2380,etcd-3=https://10.0.1.3:2380'
initial-cluster-state: 'new'
initial-cluster-token: 'etcd-cluster-1'

# 客户端 TLS（etcdctl / etcdmonitor 连接时验证）
client-transport-security:
  cert-file: '/etc/etcd/ssl/server.pem'
  key-file: '/etc/etcd/ssl/server-key.pem'
  client-cert-auth: true
  trusted-ca-file: '/etc/etcd/ssl/ca.pem'

# 节点间 TLS（集群内部通信）
peer-transport-security:
  cert-file: '/etc/etcd/ssl/peer.pem'
  key-file: '/etc/etcd/ssl/peer-key.pem'
  peer-client-cert-auth: true
  trusted-ca-file: '/etc/etcd/ssl/ca.pem'
```

### systemd 服务

```ini
# /etc/systemd/system/etcd.service
[Unit]
Description=etcd
After=network.target

[Service]
Type=notify
ExecStart=/usr/local/bin/etcd --config-file /etc/etcd/etcd.conf.yml
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
systemctl enable --now etcd
```

---

## 配置 etcdctl

### 方式一：环境变量（推荐）

设置环境变量后即可使用 etcdctl 安全连接：

```bash
export ETCDCTL_API=3
export ETCDCTL_ENDPOINTS=https://10.0.1.1:2379,https://10.0.1.2:2379,https://10.0.1.3:2379
export ETCDCTL_CACERT=/etc/etcd/ssl/ca.pem
export ETCDCTL_CERT=/etc/etcd/ssl/client.pem
export ETCDCTL_KEY=/etc/etcd/ssl/client-key.pem
```

建议写入 `/etc/profile.d/etcdctl.sh`，对所有用户生效：

```bash
cat > /etc/profile.d/etcdctl.sh << 'EOF'
export ETCDCTL_API=3
export ETCDCTL_ENDPOINTS=https://10.0.1.1:2379,https://10.0.1.2:2379,https://10.0.1.3:2379
export ETCDCTL_CACERT=/etc/etcd/ssl/ca.pem
export ETCDCTL_CERT=/etc/etcd/ssl/client.pem
export ETCDCTL_KEY=/etc/etcd/ssl/client-key.pem
EOF
source /etc/profile.d/etcdctl.sh
```

### 方式二：命令行参数

不设置环境变量时，可以在每条命令中直接传入证书参数：

```bash
etcdctl --endpoints=https://10.0.1.1:2379,https://10.0.1.2:2379,https://10.0.1.3:2379 \
        --cacert=/etc/etcd/ssl/ca.pem \
        --cert=/etc/etcd/ssl/client.pem \
        --key=/etc/etcd/ssl/client-key.pem \
        endpoint health
```

**参数说明：**

| 参数 | 对应环境变量 | 说明 |
|---|---|---|
| `--endpoints` | `ETCDCTL_ENDPOINTS` | etcd 集群地址，逗号分隔，必须使用 `https://` |
| `--cacert` | `ETCDCTL_CACERT` | CA 证书路径，用于验证 etcd 服务端身份 |
| `--cert` | `ETCDCTL_CERT` | 客户端证书路径，用于向 etcd 证明客户端身份 |
| `--key` | `ETCDCTL_KEY` | 客户端私钥路径，与客户端证书配对使用 |

> **提示：** 环境变量和命令行参数可以混用。命令行参数优先级更高，会覆盖同名的环境变量。

### 验证连接

```bash
# 集群健康检查
etcdctl endpoint health

# 查看成员列表
etcdctl member list --write-out=table

# 读写测试
etcdctl put /test/hello "world"
etcdctl get /test/hello

# 查看集群状态
etcdctl endpoint status --write-out=table
```

---

## 配置 etcdmonitor

将客户端证书复制到 etcdmonitor 部署目录：

```bash
cp /etc/etcd/ssl/ca.pem      /data/services/etcdmonitor/certs/
cp /etc/etcd/ssl/client.pem  /data/services/etcdmonitor/certs/
cp /etc/etcd/ssl/client-key.pem /data/services/etcdmonitor/certs/
```

编辑 `config.yaml`：

```yaml
etcd:
  endpoint: "https://10.0.1.1:2379,https://10.0.1.2:2379,https://10.0.1.3:2379"
  tls_enable: true
  tls_cert: "certs/client.pem"
  tls_key: "certs/client-key.pem"
  tls_ca_cert: "certs/ca.pem"
```

重启服务：

```bash
systemctl restart etcdmonitor
```

---

## 证书续期

证书到期前需要重新生成并替换。生成新证书后：

```bash
# 1. 重新生成证书（使用相同参数）
./generate.sh -n 10.0.1.1,10.0.1.2,10.0.1.3

# 2. 分发到各节点，替换旧证书
scp output/{ca,server,peer}.pem output/{server,peer}-key.pem root@<node>:/etc/etcd/ssl/

# 3. 逐节点滚动重启 etcd（避免集群中断）
ssh root@10.0.1.1 "systemctl restart etcd"
# 等待节点恢复后再重启下一个
ssh root@10.0.1.2 "systemctl restart etcd"
ssh root@10.0.1.3 "systemctl restart etcd"

# 4. 更新 etcdmonitor 客户端证书
cp output/{ca,client}.pem output/client-key.pem /data/services/etcdmonitor/certs/
systemctl restart etcdmonitor
```

### 查看证书到期时间

```bash
openssl x509 -in /etc/etcd/ssl/server.pem -noout -enddate
openssl x509 -in /etc/etcd/ssl/client.pem -noout -enddate
```

---

## 常见问题

### Q: etcdctl 报错 `certificate signed by unknown authority`

CA 证书路径不正确或未指定。确认 `ETCDCTL_CACERT` 指向正确的 `ca.pem` 文件。

### Q: etcd 启动报错 `tls: bad certificate`

Server 证书的 SAN（Subject Alternative Names）不包含当前节点 IP。重新生成证书时确保 `-n` 参数包含所有节点 IP。

### Q: etcdmonitor 连接 etcd 超时

1. 确认 endpoint 使用 `https://`（不是 `http://`）
2. 确认 `tls_enable: true`
3. 确认证书文件路径正确且文件可读

### Q: 如何在不中断服务的情况下更换证书？

etcd 支持逐节点滚动重启。按照"证书续期"章节的步骤，一次只重启一个节点，等待其恢复后再操作下一个。只要集群多数节点（N/2+1）保持运行，服务不会中断。
