# PortNoteProMax

PortNoteProMax 是一款面向小型团队与个人资产盘点场景的端口资产管理平台。项目采用 **Go + SQLite** 的轻量化栈，集成 ProjectDiscovery 的 **naabu v2** 端口扫描能力，配合前端实时卡片视图、批量操作、端口隐藏、指纹标注等功能，帮助你随时掌握服务器与云资产的端口暴露情况。

> :sparkles: 初衷：市面上的 port 资产工具普遍比较重或者 UI 不够友好，本项目希望在保证功能完备的情况下，提供简洁易用、中文友好的体验。

---

## ✨ 功能特性

- **一站式端口盘点**
  - naabu 全端口扫描（支持域名自动解析与 IP 列表）
  - 扫描状态实时推送，刷新状态跨浏览器同步
  - 指纹、备注、状态卡片化展示，支持搜索 / 排序 / 分页
- **主机管理更便捷**
  - 主机表单重新设计，创建 / 编辑更直观
  - 名称重复自动提示，避免 SQLite 约束报错
  - 一键刷新端口、批量隐藏 / 删除、查看隐藏列表
- **端口运维辅助**
  - 手动新增端口后自动调度扫描
  - 建议“未使用端口”功能，弹窗支持复制
  - 扫描完成后自动更新 Open / Closed 状态
- **实时体验**
  - 后端通过 SSE 推送事件，前端 UI 秒级响应
  - 多浏览器多用户同时操作保持数据一致
- **易部署，易维护**
  - 内置身份认证、CSRF 防护
  - 默认 SQLite 存储，无需外部依赖
  - 提供多阶段 Dockerfile / Compose 模板

---

## 🏗️ 技术架构

```
cmd/server            # 服务入口（Chi + Gorilla CSRF）
internal/auth         # 会话/登录管理
internal/config       # 环境变量配置
internal/server       # HTTP 服务与 REST API
internal/scanner      # naabu 调用与调度
internal/store        # SQLite DAO 与事务
internal/targets      # 域名/IP 规范化与解析
internal/realtime     # SSE Broker
internal/services     # 指纹工具等业务扩展
web/templates         # 登录、仪表盘 HTML 模板
web/static            # Tailwind 风格 CSS & Vanilla JS
```

---

## 🚀 快速开始

### 1. 克隆仓库

```bash
git clone https://github.com/hitushen/portnotepro.git PortNoteProMax
cd PortNoteProMax
```

### 2. 准备环境变量

默认情况下，应用会使用 `admin / admin123` 作为初始账户与 32 字节示例密钥。**生产环境请务必覆盖：**

```bash
export PORTNOTE_ADMIN_USER=your_user
export PORTNOTE_ADMIN_PASS=your_pass
export PORTNOTE_SESSION_KEY=$(openssl rand -hex 32)
export PORTNOTE_CSRF_KEY=$(openssl rand -hex 32)
```

### 3. 启动服务

```bash
go run ./cmd/server
```

访问 `http://localhost:8080` 并使用上述账号登录。首次启动会在 `./data/portnote.db` 创建 SQLite 数据库。  
若未自定义环境变量，可直接使用 **默认账户 `admin` / 密码 `admin123`** 登录。

> TIP：如果本地没有安装 naabu，也可以直接运行，项目使用 Naabu 库调用；部署环境需确保具备网络探测权限。

---

## ⚙️ 配置项

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `PORTNOTE_HTTP_ADDR` | `:8080` | 服务监听地址 |
| `PORTNOTE_DB_PATH` | `data/portnote.db` | SQLite 文件路径 |
| `PORTNOTE_ADMIN_USER` | `admin` | 初始管理员账号 |
| `PORTNOTE_ADMIN_PASS` | `admin123` | 初始管理员密码 |
| `PORTNOTE_SESSION_KEY` | 示例 32 字节 | Cookie 会话密钥，建议自定义 |
| `PORTNOTE_CSRF_KEY` | 示例 32 字节 | CSRF 防护密钥，建议自定义 |
| `PORTNOTE_SCAN_TIMEOUT` | `2s` | 单端口探测超时时间 |
| `PORTNOTE_SCAN_CONCURRENCY` | `50` | 并发扫描端口数量 |

---

## 🖥️ 前端体验速览

- 使用左侧侧栏切换主机，点击“刷新端口”触发扫描
- 顶部按钮支持新增主机、手动添加端口、批量隐藏/删除
- 右上角“获取未使用端口”会调起弹窗并支持一键复制
- 搜索框支持模糊匹配端口号、指纹、备注
- 隐藏/取消隐藏等操作会实时更新可见/隐藏列表

---

## 🐳 Docker 与编排

### 方法一：直接构建镜像

```bash
docker build -t portnotepromax:latest .

docker run -d \
  --name portnote \
  -p 8080:8080 \
  -e PORTNOTE_ADMIN_USER=your_user \
  -e PORTNOTE_ADMIN_PASS=your_pass \
  -e PORTNOTE_SESSION_KEY=0123456789abcdef0123456789abcdef \
  -e PORTNOTE_CSRF_KEY=abcdef0123456789abcdef0123456789 \
  -v portnote-data:/app/data \
  portnotepromax:latest
```

容器中同样会在缺省情况下创建默认账户 **admin / admin123**，请部署后尽快修改环境变量。

### 方法二：Docker Compose

```yaml
services:
  portnote:
    build: .
    image: portnote/promax:latest
    restart: unless-stopped
    environment:
      PORTNOTE_ADMIN_USER: ${PORTNOTE_ADMIN_USER}
      PORTNOTE_ADMIN_PASS: ${PORTNOTE_ADMIN_PASS}
      PORTNOTE_SESSION_KEY: ${PORTNOTE_SESSION_KEY}
      PORTNOTE_CSRF_KEY: ${PORTNOTE_CSRF_KEY}
    volumes:
      - portnote-data:/app/data
    ports:
      - "8080:8080"

volumes:
  portnote-data:
```

### 推送到镜像仓库

```bash
docker login
docker build -t <hub-user>/portnote-promax:latest .
docker push <hub-user>/portnote-promax:latest
```

---

## 🔧 常见问题

1. **主机名称重复报错**
   - 前端会得到“主机名称已存在，请更换名称”的友好提示；修改名称后重试即可。

2. **扫描结果不准确 / 未解析域名**
   - 项目会对输入地址进行统一归一化，并通过 `net.LookupHost` 解析域名，确保 naabu 可以获取所有 IP。请确认目标域名在部署环境可被正确解析。

3. **扫描长时间无响应**
   - 检查部署环境是否允许原始数据包发送；必要时降低 `PORTNOTE_SCAN_CONCURRENCY` 或延长 `PORTNOTE_SCAN_TIMEOUT`。

4. **如何备份数据？**
   - 所有业务数据均在 SQLite 中，直接备份 `data/portnote.db` 即可。容器部署请挂载数据卷。

---

## 🤝 贡献

欢迎提交 Issue、PR 或优化建议。你可以：

- 改进 UI 与交互体验
- 丰富服务指纹与端口标签
- 增加更多导出 / 报表能力
- 适配更多部署场景与数据库

---

PortNoteProMax 旨在成为“轻量但好用”的端口资产管家。欢迎 Star 支持，也期待你把实践经验分享给我们。Happy hacking! 🚀
