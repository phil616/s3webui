# S3 Web UI — Go / PostgreSQL 下游版本

基于 [wenisch-tech/s3webui](https://github.com/wenisch-tech/s3webui)。
Copyright (C) 2026 Jean-Fabian Wenisch / wenisch.tech。
本版本为独立维护的修改版，2026-10-16 将运行栈改为 Go + PostgreSQL，并加入中文/英文界面。
继续采用 [GNU AGPLv3](LICENSE)，不提供担保；见 [修改与署名说明](NOTICE.md)。

这是连接 AWS S3 或兼容存储的管理客户端。对象保存在 S3，PostgreSQL 保存用户、会话、加密连接、授权和审计。
Go 可执行文件内置前端，无需 Java、H2 或 Node 浏览器运行依赖。
原 Java/Thymeleaf 源码暂保留在 `src/`，作为来源和迁移参考，不参与 Go 构建或部署。
[上游原始 README](docs/upstream/README.original.md) 仅供历史参考，原配置不适用于本版本。

## 已实现

- 本地账号、管理员用户管理、PostgreSQL 持久化会话，凭证授权逐请求重新校验。
- S3 连接管理、AES-256-GCM 密钥及临时令牌加密，支持 AWS/自定义端点、区域和路径寻址。
- 桶与对象分页、前缀导航、逐页搜索、上传/分段上传/取消、下载、元数据、复制/移动、删除及版本。
- 桶策略、CORS、版本控制、生命周期、加密、标签、公共访问阻止和所有权配置。
- 中英文切换，记住语言偏好，浏览器语言回退，本地化日期与数字，明暗主题。
- 审计历史、AGPL 源码入口、只生成审查报告的手动上游更新工具。

[S3 支持矩阵与限制](docs/S3-COMPATIBILITY.md) 列出完整边界。当前不提供 OIDC、厂商 IAM 和全部 AWS S3 产品功能，不能宣称已覆盖所有 S3 API。
本版本采用新 `ui_*` 表，不自动导入上游账号、密钥和审计数据；如需迁移现有生产数据，应先备份并设计专门导入流程。

## Docker Compose

Linux amd64 原生运行包见 [v1.0.0 Release](https://github.com/phil616/s3webui/releases/tag/v1.0.0)。
包内包含可执行文件、对应源码和运行说明；使用前可通过 `SHA256SUMS` 校验。

1. 复制 `.env.example` 为 `.env`，填写唯一管理员密码、数据库密码和加密密钥。
2. 生成密钥：`node -e "console.log(require('node:crypto').randomBytes(32).toString('base64'))"`。
3. 执行 `docker compose up --build -d`，访问 http://localhost:8080。
4. 登录后在 **S3 连接** 创建连接，为普通用户填写授权：`*`、邮箱、`role:USER`、`role:ADMIN`，逗号分隔。
   AWS endpoint 留空、区域填桶所在区域；兼容服务填写完整端点并按服务要求启用路径寻址。
5. 选择连接后管理存储桶。已有私有桶也可通过当前连接下的服务端权限访问。

容器内置该构建的完整对应源码及 Go 依赖，登录页的源码链接指向 `/source`。
不要把 `.env`、数据库或密钥文件提交到仓库。备份 PostgreSQL 时也必须安全保存加密密钥。

## 原生运行

需要 Go 1.26+ 和 PostgreSQL 18（集成验证版本）。前端为原生 ES Modules，直接嵌入 Go，无额外打包产物。
设置以下环境变量后运行 `go run ./cmd/s3webui` 或 `go build -o bin/s3webui ./cmd/s3webui`。

| 变量 | 含义 |
| --- | --- |
| DATABASE_URL | 必填，PostgreSQL 连接 URL；生产环境校验证书 |
| ADMIN_EMAIL / ADMIN_PASSWORD | 必填，首次空用户表时创建管理员；密码 12–72 字节；之后不覆盖已有密码 |
| APP_ENCRYPTION_KEY | 必填，32 字节随机 AES 密钥的 Base64；不能随重启变化 |
| PUBLIC_URL | 必填，实际浏览器访问 origin，例如 https://s3.example.com；不支持子路径部署 |
| SOURCE_URL | 原生部署必填，实际运行版本的免费源码归档地址；不能只链接原作者主分支 |
| SOURCE_ARCHIVE | 可替代 SOURCE_URL，指向本机完整源码归档；自动提供 /source 下载 |
| HTTP_ADDR | 默认 :8080 |
| COOKIE_SECURE | 默认 true；仅本地 HTTP 调试设 false，生产 HTTPS 保持 true |

所有修改请求检查 Origin 与 PUBLIC_URL 是否一致。反向代理需保留浏览器 Origin，不根据任意 X-Forwarded 头推断可信来源。
部署在 HTTPS 后，配置代理请求大小、上传超时及限流；本应用每次上传段最多占用 64 MiB 内存。
登录有数据库中的账号尝试窗口及进程级密码计算并发限制。会话为 12 小时绝对有效期，密码重置会撤销该用户会话。
新建表和首次管理员初始化在事务内执行，使用 PostgreSQL advisory lock，允许多个副本启动；无需粘性会话。

## 验证与维护

```text
npm ci
npm test
npm run build
go vet ./...
go test ./...
```

设置隔离测试数据库的 `TEST_DATABASE_URL` 后，`go test -count=1 ./...` 才会运行 PostgreSQL 集成测试；未设置会明确跳过。
CI 提供测试数据库并配置集成测试、Go race、前端测试、浏览器检查和容器构建，只有验证权限，不发布版本。
真实 AWS/目标兼容服务验收要求见支持矩阵，尚不能由 HTTP 测试端点替代。

浏览器检查：构建 `bin/s3webui` 后，设置隔离数据库 `BROWSER_DATABASE_URL`，执行 `npx playwright install chromium`
和 `npm run test:browser`。测试会启动独立端口的 Go 进程，并在 `.local/screenshots/` 生成桌面/移动端截图。
也可通过 `BROWSER_SERVER_BINARY` 和 `BROWSER_EXECUTABLE` 指定已有程序与浏览器。测试中的 S3 响应经过模拟，
用于验证界面流程；不要在生产数据库运行这些测试。

[Helm 部署](charts/s3webui/README.md) · [手动审查上游更新](docs/UPSTREAM.md) · [验证记录](docs/VALIDATION.md) · [第三方声明](THIRD_PARTY_LICENSES.md)

上游同步完全自选：手动输入固定提交 SHA 生成报告，逐项记录直接采用/移植/跳过/延期，再通过普通 PR 引入。
不存在自动合并或定时上游更新。替换后端不改变 AGPL 的对应源码和署名义务。
