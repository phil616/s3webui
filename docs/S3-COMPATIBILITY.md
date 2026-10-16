# S3 语义与实现边界

本项目是 S3 管理客户端，不是 S3 对象存储服务器。`/api/*` 是给本界面使用的 JSON API，
不声称兼容 S3 的 HTTP/XML 服务端协议。对象和权限的最终事实来源是 S3；PostgreSQL 不模拟对象、桶、版本或 IAM。

## 支持矩阵

| 能力 | Go 实现与界面行为 |
| --- | --- |
| SigV4、区域、临时凭证 | 官方 AWS SDK Go v2；支持 region、endpoint、path style、session token；TLS 证书验证始终启用 |
| 列桶 | ListBuckets，透传 ContinuationToken，界面逐页显示；没有列出所有桶权限时，可手动打开已知桶名 |
| 建桶、删桶 | CreateBucket 在 us-east-1 省略 LocationConstraint，其他区域传递区域；DeleteBucket 由 S3 判断是否为空，不递归清桶 |
| 对象列表、文件夹 | ListObjectsV2；默认 delimiter=/，递归时省略；文件夹只是前缀或以 / 结尾的零字节对象 |
| 搜索 | 扫描当前桶的 S3 页面；响应保留扫描 token；没有结果的页不代表没有后续结果；不伪装服务端全局索引 |
| 对象键 | 1–1024 UTF-8 字节；查询参数原样携带；不调用 path.Clean，不清理 //、..、空格、+、% 或中文 |
| 下载、元数据 | GetObject/HeadObject；Range、ETag、versionId；强制附件下载，避免把不可信对象作为平台 HTML 执行 |
| 上传 | 小对象 PutObject；大对象 CreateMultipartUpload → UploadPart → CompleteMultipartUpload；浏览器上传进度、取消及失败后 AbortMultipartUpload |
| 未完成上传 | 列出未完成上传并手动终止；ListParts API 可用于检查；不提供断点续传界面 |
| 分段限制 | S3 的分段编号和 ETag 原样传递；完成列表要求编号递增；最终合法性由 S3 校验；ETag 不作为 MD5 使用 |
| 应用上传上限 | 单次 PUT 16 MiB，单段 64 MiB，浏览器最多 10,000 段、625 GiB；这是本应用资源上限，不是 S3 对象大小上限 |
| 复制、移动 | CopyObject 使用编码后的 CopySource，源 ETag 条件检查；移动复制成功后才执行条件删除；删除失败明确报告部分成功，不宣称原子 rename |
| 删除与版本 | DeleteObject 可携带 versionId；不带版本时由 S3 决定生成删除标记或实际删除；支持 versionId 字符串 null；列出版本和删除标记并分页 |
| 桶配置 | policy、CORS、versioning、lifecycle、encryption、tagging、public access block、ownership；按 AWS SDK 输入结构编辑；实际支持情况和权限由服务端决定 |
| 错误 | 返回结构化 code 和 S3 HTTP 错误状态；禁止把拒绝访问或不支持的配置伪装成功；界面显示本地化说明和原始技术错误码 |

## 不在当前实现范围

AWS IAM 是独立服务，RustFS 管理 API 是厂商扩展，均不属于 S3 数据 API。本版本不提供这些面板，
也不提供 OIDC、禁用认证模式或普通用户自行填写任意端点。连接由管理员维护，并按用户、应用角色或所有登录用户授权。
这属于下游产品范围选择，不表示这些原仓库功能已完成迁移。

本界面不覆盖全部 AWS S3 产品：超过单请求大小的 multipart copy、目录桶/S3 Express、Access Points、
Outposts、Object Lock 管理、对象 ACL/标签编辑、归档恢复、复制规则、事件通知、网站托管、跨区域自动路由等
不在已验证范围；不能据此宣称“完整 S3 兼容认证”。不支持的功能不会写入 PostgreSQL 模拟成功。
相同名称的对象复制、上传可能覆盖目标或产生新版本，界面在操作前说明。

## 验证

`go test ./...` 包含加密篡改、配置失败关闭、键编码测试。
设置 `TEST_DATABASE_URL` 后启用真实 PostgreSQL 集成测试：认证、会话重启、授权撤销立即生效、密钥不泄漏、
以及以官方 SDK 访问测试 HTTP S3 端点时的 SigV4、分页、中文键、Range、版本和删除标记语义。
测试创建独立随机 schema 并在结束时删除，仅在隔离测试数据库执行。
前端测试覆盖双语词条与占位符、浏览器语言回退、对象 URL、上传分段边界。

自动测试端点不等于真实 AWS。发布前还需在目标 AWS/兼容存储上执行桶的创建/列表/配置，
包含 >1000 对象的分页、空文件和大文件上传、取消后清理、启用版本控制后的下载/删除和权限拒绝测试；
未提供云账号和目标服务时不标记为已通过。

## 标准依据

- [AWS ListObjectsV2](https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListObjectsV2.html)
- [AWS Multipart limits](https://docs.aws.amazon.com/AmazonS3/latest/userguide/qfacts.html)
- [AWS CopyObject](https://docs.aws.amazon.com/AmazonS3/latest/API/API_CopyObject.html)
- [AWS DeleteObject](https://docs.aws.amazon.com/AmazonS3/latest/API/API_DeleteObject.html)
- [Go SDK endpoint configuration](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-endpoints.html)

采用官方 SDK 处理签名、XML、校验和及完成/复制操作中可能包含在 HTTP 200 内的 S3 错误。
上游行为与 AWS 文档不同时，以官方 API 的操作语义为依据，厂商不支持的特性保留其错误结果。
