# 手动审查上游更新

上游：[wenisch-tech/s3webui](https://github.com/wenisch-tech/s3webui)。
原作者仍保有原作品版权；下游修改继续遵循 AGPLv3。本流程不自动合并、不自动 cherry-pick、不自动发布，没有定时任务。

## 获取更新候选

1. 查看上游发布说明，或运行 `git ls-remote https://github.com/wenisch-tech/s3webui refs/heads/main` 获取候选 SHA。
2. 使用 PowerShell 7：`./scripts/review-upstream.ps1 -Target <完整40位SHA>`。
   也可手动运行 GitHub Actions 的 **Manual upstream review**，填写 SHA 后下载报告。
3. 工具仅 fetch 指定提交、读取 Git 对象并生成报告，不修改工作分支、应用补丁或推送。
   报告包含提交列表、路径差异、完整补丁和许可证变化。

## 逐项判断

在报告的决策表中逐项记录 `adopt`（直接采用）、`port`（等价移植）、`skip`（不适用）
或 `defer`（延期），以及原因、对应下游提交和验证结果。

| 上游改动 | 下游审查方式 |
| --- | --- |
| Java/Spring/H2 依赖 | 通常不直接移植；仍需判断是否揭示同类安全问题 |
| S3 签名、分页、键编码、权限、上传修复 | 对照 AWS 官方 API；验证 Go 实现是否受影响，必要时移植语义和回归用例 |
| 前端交互、无障碍 | 继续使用原作者的页面与组件；中英文同时补齐；保留设计回归检查 |
| RustFS IAM、OIDC | 当前不提供；记录不适用或延期，不能标记为已支持 |
| LICENSE、版权、第三方声明 | 人工检查；许可不明确时不引入相关更新 |

原样采用少量提交时，在专用集成分支上运行 `git cherry-pick -x <SHA>` 保留来源。
结构不同时，在下游实现等价修复，并在提交正文保留上游 SHA/URL。
不要直接应用整个差异补丁，不伪造原作者背书，不覆盖版权或修改原 LICENSE 文本。

## 合并、发布、回滚

发起普通 PR，附报告和验证结果。完成审查后手动更新 `docs/upstream/state.json` 的
`lastReviewed` 和 `decisions`；该 SHA 表示已审查范围，不代表全量合入。延期项保留追踪记录，`baseline` 不变。

CI 只验证下游，不拉取上游变更、不创建标签、不推送镜像、不发布 release。发布由维护者决定。
网络部署和二进制分发需提供实际运行版本的完整对应源码，包含构建与安装脚本。
容器内置 `/source` 归档及 Go 依赖；修改版需重新构建。
原生部署须设置精确归档的 `SOURCE_URL` 或配置 `SOURCE_ARCHIVE`。
登录页和应用页显示源码、上游作者、许可及无担保声明。
详见 [LICENSE](../LICENSE) 第 4–6、13 条及 [NOTICE](../NOTICE.md)。

回滚使用 Git revert 或上一版本镜像。数据库升级前备份，不自动执行删除性反向迁移。
本次采用 `ui_*` 新表，不修改上游 Hibernate 表；旧数据迁移需另行审查和演练。
