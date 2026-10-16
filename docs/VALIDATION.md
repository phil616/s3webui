# 改造验证记录

日期：2026-10-16。测试环境：Windows、Go 1.27、Node.js 24、独立本地 PostgreSQL、Chrome 无头浏览器。

已在本地执行并通过：

- `go vet ./...` 和 Linux/amd64、CGO 禁用的 Go 构建。
- 设置 `TEST_DATABASE_URL` 后执行 Go 测试，包含真实 PostgreSQL 认证/会话/授权、重启、密钥保护、
  S3 SDK 请求语义、分段上传、复制后的删除失败、桶配置及公开源码/许可证入口；没有跳过数据库测试。
- `npm test`：26 项通过，其中 20 项为保留的上游前端工具测试，6 项为本次新界面测试。
- `npm run build`：校验所有实际服务的浏览器模块。
- `npm run test:browser`：实际启动 Go 服务并连接 PostgreSQL，验证中英文偏好、登录/退出、
  连接创建、对象键安全显示、元数据、版本、17 MiB 分段上传、桶配置及移动端布局。
  桌面与移动端截图位于忽略目录 `.local/screenshots/`。S3 响应在浏览器测试中模拟。
- `helm lint charts/s3webui` 及带现有 Secret 的模板渲染。
- 使用 actionlint 检查新的 CI 和手动上游审查工作流。
- 对基线 SHA 运行手动上游审查脚本，确认可以生成报告、空差异补丁与许可副本，不合并变更。
- `git diff --check`，原 LICENSE 内容保持不变，内嵌许可证与其逐字节一致。

尚未在本机执行：

- Docker 构建及 Compose 启动：本机没有 Docker；已写入 CI 容器构建步骤。
- Go race：本机没有相应 C 工具链；已写入 Linux CI。
- GitHub Actions 云端执行、Kubernetes 实际部署，以及 AWS/指定兼容存储实机验收。

这份记录区分了实际通过、配置待执行和目标服务待验收；它不代表完整 AWS S3 认证。
具体实现范围与发布前检查见 [S3 支持矩阵](S3-COMPATIBILITY.md)。
