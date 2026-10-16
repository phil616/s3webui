# 项目约束

前端沿用 wenisch-tech/s3webui 原作者的设计。当前基线为
`a820d5945127cfc60b48e3c03c5eb1e54e09fc17`。

- 实际页面来源是 `src/main/resources/templates/`，实际浏览器代码与样式来源是 `src/main/frontend/`。
- 除非用户明确要求重新设计，否则保留原有布局、CSS、Logo、图标、组件、控件 ID 和交互；前端改动以中英文 i18n 及必要的后端适配为限。
- Go 后端通过模型和接口适配原前端。不能因 Thymeleaf 属于 Java 技术栈而另外实现一套界面。
- 仅支持中文、英文。翻译 UI 文案，不翻译对象键、文件名、账号、S3 协议值或 JSON 策略内容。
- `src/main/resources/rendered/` 与 `static/css/`、`static/js/`、`static/licenses/` 是构建产物，不手工编辑。
- 保留原作者署名、AGPLv3、第三方许可证及当前运行版本的源码入口。
- 先运行 `npm ci && npm run build`，再进行 Go 构建或测试。`npm test` 检查原 CSS、Logo 和核心模板相对基线保持不变。
- 上游更新仍然只按维护者手动选择引入；前端修正不构成自动合并或重写发布标签的授权。
