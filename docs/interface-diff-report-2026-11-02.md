# 中台对外接口与 Go SDK 覆盖情况对比报告

- 对比对象：`youwei-infra-center`（VERSION 2.0.0，最近提交 f0df2b3）↔ `youwei-infra-sdk`（CHANGELOG 最新 v0.2.0，2026-10-22）
- 报告日期：2026-11-02
- 性质：仅记录与建议，不修改任何代码
- 路由依据：中台 `internal/httpserver/routes.go`、`internal/api/*_handlers.go` 路由注册，及 `docs/api/05-file-storage.md`、`docs/api/all-public-apis.md`

## 一、结论摘要

| 模块 | 中台对外接口数 | SDK 已覆盖 | 状态 |
|---|---|---|---|
| OAuth Client Token | 1（`POST /oauth/token`） | 内置自动管理 | ✅ 已覆盖 |
| 身份认证 Identity | 8 | 8 | ✅ 完全覆盖，无变更 |
| 图片 Images | 11 | 11 | ✅ 完全覆盖，无变更 |
| 业务短信 SMS | 4（+1 服务商回调） | 4 | ✅ 完全覆盖，无变更 |
| 微信公众号 WeChat MP | 7（+2 Portal 回调） | 7 | ✅ 完全覆盖，无变更 |
| **文件存储 Files** | **7** | **0** | ❌ **未覆盖（中台 2.0.0 新增）** |
| 版本/健康检查 | `GET /api/v1/version`、`GET /health` | 无（信息性接口，可不覆盖） | ➖ 无需覆盖 |

**需要更新的内容：仅文件存储（Files）一个新模块。** 其余四个既有模块自 SDK v0.2.0 以来路由零变更（`git log --since=2026-10-22` 对相应 handler 文件无提交）。

## 二、新增模块：文件存储（STS 客户端直传）

中台 2.0.0 新增，接口文档 `docs/api/05-file-storage.md`，近期大量提交（`8393216 feat(file-storage): add secure direct upload and role onboarding` 等）。中台文档已明确注明：**"本仓外的 youwei-infra-sdk 不在本次修改或发版范围；暂通过上述 HTTP 契约接入"** —— 即当前业务方只能裸调 HTTP，SDK 覆盖是明确的后续事项。

### 2.1 待新增的 7 个接口

统一前缀 `/api/v1/files`，需 `Authorization: Bearer <client_token>`，响应外壳为 `{data, request_id}`。

| # | 方法 路径 | 所需 Scope | 用途 | 建议 SDK 方法 |
|---|---|---|---|---|
| 1 | `POST /api/v1/files/upload-authorizations` | `file.upload` | 建立上传意图并领取 STS 临时凭证 + SDK 参数 | `Files.AuthorizeUpload(ctx, req)` |
| 2 | `POST /api/v1/files/{file_id}/upload-credentials` | `file.upload` | 原文件续签 STS（续传场景），返回同一上传目标 | `Files.RenewUploadCredentials(ctx, fileID)` |
| 3 | `POST /api/v1/files/{file_id}/reconcile` | `file.upload` | Head 核验云端对象，补偿回调丢失/失败 | `Files.Reconcile(ctx, fileID)` |
| 4 | `GET /api/v1/files/{file_id}` | `file.read` | 查询文件状态与元信息 | `Files.Get(ctx, fileID)` |
| 5 | `GET /api/v1/files` | `file.read` | 本 Client 分页查询（keyword/scene_code/status/page/page_size） | `Files.List(ctx, req)` |
| 6 | `POST /api/v1/files/{file_id}/access-urls` | `file.download` | 签发绑定确认版本的短期附件下载 URL（默认 5 分钟） | `Files.CreateAccessURL(ctx, fileID)` |
| 7 | `DELETE /api/v1/files/{file_id}` | `file.delete` | 取消/删除文件，停止续签，安排延迟清理 | `Files.Delete(ctx, fileID)` |

### 2.2 关键数据结构（依据接口文档）

申请上传授权请求体（接口 1）：

- 必填：`scene_code`、`original_filename`、`file_size`（字节）、`content_type`
- 可选：`operator_id`、`biz_ref_type`、`biz_ref_id`、`idempotency_key`（幂等键属于 Client，重放时文件元数据必须一致）

授权响应 `data` 主要字段：

| 字段 | 说明 |
|---|---|
| `file_id`、`object_key` | 中台分配的资源 ID 与 OSS Key |
| `provider` | 首版固定 `aliyun_oss` |
| `region`、`endpoint`、`bucket` | 官方 OSS SDK 初始化参数 |
| `credentials.access_key_id` / `access_key_secret` / `security_token` / `expires_at` | STS 临时凭证（默认 900 秒） |
| `callback` | 官方 SDK callback 参数，由中台生成 |
| `max_file_size_bytes` | 场景业务上限（非 STS 硬限制） |
| `upload_expires_at` | 允许恢复的截止时间（与凭证期限不同） |
| `part_size_bytes`、`parallel` | 默认 8MiB、3，供上传 SDK 使用 |

文件状态机：`UPLOADING` / `ACTIVE` / `FAILED` / `EXPIRED` / `DELETING` / `DELETED`；**只有中台侧 `ACTIVE` 才能作为业务可用附件**。

### 2.3 SDK 实现注意点（来自中台文档与路由代码）

1. **职责边界**：文件内容不经过中台；Go SDK 只封装上述控制面 7 个接口，实际 OSS 分片上传/续传由终端用官方 OSS SDK 完成（Web 用 `ali-oss`，桌面/服务端参考中台 `examples/file-upload/`）。SDK 文档中应明确这一分工，避免误解为 SDK 负责传输文件体。
2. **安全语义**：临时凭证与签名 URL 不持久化、不写日志、授权响应不可缓存 —— SDK 侧不应缓存 STS 凭证，`Cache-Control: no-store` 已由服务端强制。
3. **幂等与错误语义**：幂等键重放要求元数据一致；已删除/终态文件不能用旧幂等键创建新对象（近期修复 `d3205e6 fix(files): 修正上传授权错误码语义`、`ce4d0e5 fix(file-storage): 能力授权未知场景返回 400`，实现时按最新契约对齐错误码）。
4. **resource 校验**：`file_id` 为 UUID，路由层有 `requireUUIDParam` 校验；SDK 现有 `resource[T]` 辅助函数可直接复用。
5. **不发请求体的接口**：`reconcile`、`access-urls` 与图片模块同类接口一致，POST 空对象体即可。
6. **不自动重试**：与 SDK 现有原则一致 —— 上传授权/续签均为一次性凭证语义，不应自动重试。
7. **新增 OAuth scopes**：`file.upload`、`file.read`、`file.download`、`file.delete`；接入应用需在中台开通对应能力，SDK 文档需提示。

### 2.4 服务商/终端回调接口（SDK 无需覆盖，仅备案）

以下接口的调用方是云服务商或微信服务器，不是 Go SDK 的使用方：

- `POST /api/v1/storage-callbacks/{provider}`（统一存储回调；旧路径 `/api/v1/images/direct-upload-callbacks/{provider}` 仅转发兼容）
- `POST /api/v1/file-storage-callbacks/{target_id}/{file_id}`（文件上传完成回调）
- `POST /api/v1/sms/callbacks/aliyun/report`（阿里云短信回执）
- `GET/POST /api/v1/wechat/mp/{app_id}/portal`（微信 Portal 回调，事件经中台转发并以 HMAC 签名推送，SDK 已有 `VerifyWebhook`/`ParseWechatEvent` 侧支持）

## 三、既有模块核对结果（无需更新）

| 核对项 | 中台现状 | SDK 现状 | 结果 |
|---|---|---|---|
| Identity 8 路由 | password/register、password/login、sms/send-code、sms/login、wechat qrcode 创建/状态/登录、token/refresh | `IdentityService` 8 方法一一对应 | ✅ 一致 |
| Images 11 路由 | 上传、直传会话创建/查询/reconcile、列表/详情、access-url、variants、删除、usage、call-records | `ImageService` 11 方法一一对应 | ✅ 一致 |
| SMS 4 路由 | verifications/send、verify、notifications/send、messages/{id} | `SMSService` 4 方法对应 | ✅ 一致 |
| WeChat MP 7 路由 | access-token 读取/刷新、qrcode/temp、users/{openid}、messages/custom、channels 列表、channels/{key}/menu | `WechatService` 7 方法对应 | ✅ 一致 |
| Token 失效逻辑 | UNAUTHORIZED/TOKEN_EXPIRED 处理，refresh 路径豁免 | client.go 已按 `/api/v1/identity/token/refresh` 豁免 | ✅ 一致 |
| 错误信封 | 嵌套 `{error:{code,message}}` + RFC 6749 扁平格式 | v0.2.0 已兼容两种格式 | ✅ 一致 |

## 四、建议行动清单

1. **【必须】新增 `Files` 服务**：按 2.1 的 7 个接口新增 `files.go`（或在 client.go 注册 `c.Files`），含 `File`、`FileUploadAuthorization`、`FileCredentials`、`FileAccessURL`、`FileFilter` 等类型；版本建议 MINOR 提升并按 CHANGELOG 规范记录。
2. **【建议】文档同步**：`docs/usage.md` 补充文件服务章节，说明"SDK 仅封装控制面，上传/续传用官方 OSS SDK 完成"，并链接中台 `docs/api/05-file-storage.md` 与其 `examples/file-upload/`。
3. **【建议】契约测试**：中台仓已有 `internal/api/go_sdk_test.go` 作 HTTP 契约测试，SDK 侧为 Files 新增同等契约用例（可参考中台 `docs/acceptance/file-storage-api-test-2026-11-02.md` 的验收结论与遗留问题）。
4. **【可选】`GET /api/v1/version`、`GET /health`**：信息性接口，可不封装；如需部署巡检可加一个只读方法。

## 五、遗留风险提示（仅记录）

- 中台文件服务尚在快速修复期（2026-11-01/02 连续多份验收问题与修复报告：`docs/acceptance/file-storage-api-*`），SDK 接入实现前建议以最新 `docs/api/05-file-storage.md` 与路由代码为最终契约，而非仅依据本报告快照。
- 中台 `docs/api/05-file-storage.md` 第 187 行明确本 SDK "不在本次修改或发版范围"，说明该模块发版时业务方暂以 HTTP 契约接入；SDK 更新属于跟进事项，需与中台文件服务稳定版本对齐后再发 MINOR。
