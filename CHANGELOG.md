# Go SDK 变更记录

SDK 与中台应用独立版本管理；Git 标签是版本的唯一权威来源，不额外维护版本常量或 `VERSION` 文件。标签格式为 `vMAJOR.MINOR.PATCH`，Go module 使用 `vMAJOR.MINOR.PATCH`。已发布标签不可覆盖或移动。

修复提升 PATCH，兼容功能提升 MINOR。`v0.x` 的破坏性变更提升 MINOR 并列出迁移步骤；`v1` 起破坏性变更提升 MAJOR，`v2` 起 module/import 路径增加 `/v2`。各产品线固定具体版本，升级与回退遵循自身验收流程。

## v0.1.0 - 2026-10-16

首个版本，Go 1.22+，只依赖标准库。

- 覆盖身份 8 个、图片 11 个、业务短信 4 个、微信公众号 7 个业务接口。
- 自动获取、并发缓存及提前续取 Client Token；用户登录、微信凭证与 Client Token 分离。
- 提供 Context、Request ID 透传、类型化响应与 APIError，保留 Retry-After。
- 支持流式图片上传、终端直传会话与公众号 Webhook 原始体验签。
- 默认不自动重试业务请求，避免重复发送短信、上传或消费一次性凭证。
- 提供详细接入文档、完整首调程序、SDK 单测和服务端 HTTP 契约测试。

首次安装：

```bash
go get github.com/ivanhxy/youwei-infra-sdk@v0.1.0
```

SDK 版本不表示目标中台已经开通所有能力。接入前确认目标环境接口与应用授权，真实短信、微信和云存储业务仍需产品线联调。
