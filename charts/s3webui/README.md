# Helm — Go / PostgreSQL 下游

本 chart 部署 Go 镜像，不部署数据库。需预先准备 PostgreSQL、镜像仓库和 Kubernetes Secret。
上游 Java 镜像不能与本 values 混用；请构建本分支 Dockerfile，并将 image.registry/repository/tag 指向自己的镜像。

Secret 至少包含：DATABASE_URL、ADMIN_PASSWORD、APP_ENCRYPTION_KEY。
数据库 URL 使用正确的 TLS 验证参数，加密密钥为 32 字节随机值的 Base64。
管理员邮箱通过 env.ADMIN_EMAIL 配置，PUBLIC_URL 设置为浏览器实际 HTTPS origin。
默认启用 Secure Cookie；Ingress 应配置 TLS。内置源码归档由 /source 免费提供给用户。

```sh
helm upgrade --install s3webui ./charts/s3webui \
  --set image.registry=ghcr.io \
  --set image.repository=YOUR_ORG/s3webui-go \
  --set image.tag=YOUR_IMMUTABLE_VERSION \
  --set existingSecrets[0]=s3webui-runtime \
  --set env.PUBLIC_URL=https://s3.example.com
```

多个副本共享 PostgreSQL 会话，无需粘性会话和应用 PVC。每个副本必须使用相同加密密钥。
/livez 仅表示进程存活，/healthz 检查 PostgreSQL。上传采用分段请求，代理请求上限应至少为 64 MiB。
数据库备份、可用性及权限由部署方维护。生产升级前先备份并核对迁移；不要把密钥值提交到 values 文件。

原作者署名和 AGPL 许可保留在项目根目录的 NOTICE.md 和 LICENSE。
