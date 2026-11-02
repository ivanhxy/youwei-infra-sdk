# Go SDK 变更记录

SDK 与中台应用独立版本管理；Git 标签是版本的唯一权威来源，不额外维护版本常量或 `VERSION` 文件。标签格式为 `vMAJOR.MINOR.PATCH`，Go module 使用 `vMAJOR.MINOR.PATCH`。已发布标签不可覆盖或移动。

修复提升 PATCH，兼容功能提升 MINOR。`v0.x` 的破坏性变更提升 MINOR 并列出迁移步骤；`v1` 起破坏性变更提升 MAJOR，`v2` 起 module/import 路径增加 `/v2`。各产品线固定具体版本，升级与回退遵循自身验收流程。

## v0.3.0 - 2026-11-02

新增文件存储（STS 客户端直传）控制面接口，对应中台 2.0.0 的 `/api/v1/files` 能力，共 7 个接口。接入前需管理员为应用开通 `file.upload` / `file.read` / `file.download` / `file.delete` 能力。

- 新增 `client.Files`（`FilesService`）：
  - `AuthorizeUpload`：建立上传意图并领取 STS 临时凭证与官方 OSS SDK 参数（`file.upload`）。
  - `RenewUploadCredentials`：原文件续签凭证，用于分片续传（`file.upload`）。
  - `Reconcile`：回调丢失/失败后的 Head 核验补偿（`file.upload`）。
  - `Get` / `List`：文件状态与元信息查询、本 Client 分页查询（`file.read`）。
  - `CreateAccessURL`：签发绑定确认版本的短期下载 URL（`file.download`）。
  - `Delete`：取消/删除文件并安排延迟清理（`file.delete`）。
- 新增类型：`FileUploadAuthorizationRequest`、`FileUploadAuthorization`、`FileUploadCredentials`、`FileCallback`、`File`、`FileAccessURL`、`ListFilesRequest`、`FileList`，以及文件状态常量 `FileStatusUploading` 等。
- `File` 新增 `ReconcileResult` 字段（`reconcile_result`，omitempty）：仅 `Reconcile` 响应携带，给出本次 Head 核验结论；配套常量 `FileReconcileObjectConfirmed` / `FileReconcileObjectNotFound` / `FileReconcileMismatchFailed`（对应中台 2026-11-02 联调验收 P-03 方案 A）。
- file_id 为中台生成的 `file_` + 32 位十六进制编号（非 UUID），按不透明字符串处理（对应联调验收 P-01）。
- 职责边界：SDK 仅封装控制面，文件内容不经过中台，实际上传/续传由终端用阿里云官方 OSS SDK 完成；STS 凭证与签名 URL 不持久化、不缓存。只有中台状态 `ACTIVE` 才可作为业务附件。
- 文档：`docs/usage.md` 新增 4.5 节（文件存储）、方法覆盖表补充 7 个接口；接入示例与关键约束见文档。

新增方法均为纯增量，不影响既有方法签名；从 v0.2.0 升级只需更新版本号。

## v0.2.0 - 2026-10-22

安全加固与兼容性增强，Go 1.26+。相对 v0.1.0 无接口签名变更；以下输入从本版起被明确拒绝，调用方需按新错误信息修正入参：

- 修复上传文件名 CRLF 头注入：`Images.Upload` 的 `Filename` 拒绝包含控制字符（CR/LF/NUL/0x7f）的值，防止在 multipart 部件头中伪造额外字段。
- `infra.WithRequestID` 传入的 Request ID 在发送前校验：仅接受 1-128 个可打印 ASCII 字符（不含空格等空白），非法值返回明确错误而不是笼统的传输失败。
- 兼容 RFC 6749 扁平错误信封：OAuth 端点返回 `{"error":"invalid_client","error_description":...}` 时，`APIError.Code/Message` 现在保留 `invalid_client` 等错误码，不再退化为 `HTTP_ERROR`；原有嵌套 `{"error":{"code":...}}` 格式不受影响。
- 最低 Go 版本从 1.22 升至 1.26，并固定 `toolchain go1.26.10`，修复 24 个可通过 HTTPS 调用路径触达的标准库漏洞（crypto/tls、net/http、crypto/x509、net/url 等）；CI 矩阵同步改为 1.26.x 与 stable。

迁移说明：

- 曾依赖含换行/控制字符文件名"原样上传"的调用会收到 `infra: Filename must not contain control characters`；文件名来自终端用户时应先在业务侧清洗。
- Request ID 含空格、中文或超过 128 字节的调用会收到明确错误；请改用网关侧的合法追踪 ID。
- 产品项目自身 `go.mod` 无需改动即可继续使用本 SDK；仅在产品自建 Go 版本低于 1.26 时需先升级其工具链。

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
