# S3 Web UI — Linux amd64

This package is the Go/PostgreSQL downstream of https://github.com/wenisch-tech/s3webui.
Copyright (C) 2026 Jean-Fabian Wenisch / wenisch.tech. GNU AGPLv3; no warranty.
See LICENSE, NOTICE.md, THIRD_PARTY_LICENSES.md and the included source.tar.gz.

## 中文运行说明

需要 Linux x86-64 和可访问的 PostgreSQL；对象数据保存在外部 S3 服务中。
解压后先检查 `./s3webui --version`，再配置环境变量：

```sh
export DATABASE_URL='postgres://s3webui:YOUR_PASSWORD@localhost:5432/s3webui?sslmode=verify-full'
export ADMIN_EMAIL='admin@example.com'
export ADMIN_PASSWORD='REPLACE_WITH_A_UNIQUE_PASSWORD'
# 生成一次并安全保存，后续启动使用相同的密钥，不要每次重新生成。
export APP_ENCRYPTION_KEY="$(openssl rand -base64 32)"
export PUBLIC_URL='https://s3.example.com'
export COOKIE_SECURE='true'
export SOURCE_ARCHIVE="$PWD/source.tar.gz"
./s3webui
```

管理员密码为 12–72 字节；只有空用户表会创建首次管理员。
生产环境使用 HTTPS 和有效的数据库 TLS 证书。仅本机 HTTP 调试时，可将 PUBLIC_URL 改为
`http://localhost:8080`，COOKIE_SECURE 改为 `false`，数据库 TLS 参数按本地数据库配置。
环境变量样例不会被程序自动读取；通过 shell、systemd 或部署工具注入。
请持久保存加密密钥和 PostgreSQL 数据。登录后由管理员创建 S3 连接并分配授权。

## English quick start

Requires Linux x86-64, PostgreSQL and an external S3 service. Configure the environment as above,
then run `./s3webui`. Set SOURCE_ARCHIVE to the absolute path of the included source.tar.gz;
the sign-in screen and application will offer it at `/source`.
The archive contains the exact release source, build/deployment scripts and vendored Go dependencies.

This release does not automatically migrate upstream Java/H2 data and does not provide OIDC,
vendor-specific IAM, or every AWS S3 product feature. Read docs/S3-COMPATIBILITY.md and
docs/VALIDATION.md inside the source archive for supported behavior and verification boundaries.

## Rebuild

Extract source.tar.gz, enter its source directory, and see BUILD-INFO.json for the exact toolchain,
commit, version and build date. On that Go version:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -mod=vendor -buildvcs=false -trimpath \
  -ldflags '-s -w -X main.version=VERSION -X main.commit=COMMIT -X main.buildDate=BUILD_DATE' \
  -o s3webui ./cmd/s3webui
```

Replace VERSION, COMMIT and BUILD_DATE with the values from BUILD-INFO.json.
When changing frontend sources, run `npm ci && npm run build` before the Go build so the original
templates and stylesheet, their i18n transformations and bundled browser modules are refreshed.
