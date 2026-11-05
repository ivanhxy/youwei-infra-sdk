# 中台对外接口与 Go SDK 覆盖情况对比报告

- 对比对象：`youwei-infra-center`（VERSION 2.0.0，最近提交 `7325c95 feat(identity): add password reset via SMS verification`）↔ `youwei-infra-sdk`（CHANGELOG 最新 v0.4.0，2026-11-05）
- 报告日期：2026-11-05
- 性质：核对结论 + 本次已落地的同步内容
- 路由依据：中台 `internal/api/*_handlers.go` 路由注册（`identity_handlers.go`、`file_handlers.go`、`public_handlers.go`、`sms_handlers.go`、`wechat_mp_handlers.go`、`wechat_token_handlers.go`）及 `docs/api/` 下契约文档

## 一、结论摘要

中台自 SDK v0.3.0（2026-11-02，files 模块）之后新增了 **1 个** 对外接口：`POST /api/v1/identity/password/reset`。其余全部对外路由、DTO 与既有 SDK 方法逐条核对一致，**无需其他改动**。

| 模块 | 中台对外接口数 | SDK 覆盖 | 状态 |
|---|---|---|---|
| OAuth Client Token | 1（`POST /oauth/token`） | 内置自动管理 | ✅ 已覆盖 |
| 身份认证 Identity | 9 | 9 | ✅ 本次补齐第 9 个（password/reset） |
| 图片 Images | 11 | 11 | ✅ 一致，无变更 |
| 业务短信 SMS | 4（+1 服务商回调） | 4 | ✅ 一致，无变更 |
| 微信公众号 WeChat MP | 7（+2 Portal 回调） | 7 | ✅ 一致，无变更 |
| 文件存储 Files | 7 | 7 | ✅ 一致，无变更 |
| 版本/健康检查 | `GET /api/v1/version`、`GET /health` | 无（信息性接口，可不覆盖） | ➖ 无需覆盖 |

**本次已落地**：SDK v0.4.0 新增 `Identity.ResetPassword` 与 `PasswordResetRequest`，并同步契约单测与文档。

## 二、新增接口：密码重置

中台提交 `7325c95 feat(identity): add password reset via SMS verification`（2026-11-04）新增，需求来源见中台 `docs/api/password-reset-requirement.md`（产品线「哒哒答」）。

### 2.1 接口契约

| 项 | 值 |
|---|---|
| 方法 路径 | `POST /api/v1/identity/password/reset` |
| 所需 Scope | `identity.login`（与密码注册、密码登录共用同一 scope） |
| 鉴权 | `Authorization: Bearer <client_token>` |
| 请求体 | `mobile`、`code`、`new_password`（均为必填） |
| 响应 | 与密码登录相同的 `UserTokens` 包 |

服务端请求体定义（`internal/api/identity_handlers.go` 的 `passwordResetBody`）：

```go
type passwordResetBody struct {
	Mobile      string `json:"mobile"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}
```

中台解码使用 `DisallowUnknownFields` 严格模式，字段名写错会被直接拒绝 —— SDK 的 `new_password` json tag 必须精确匹配。

### 2.2 行为与状态语义

依据 `internal/service/identity_user_service.go` 的 `ResetPassword` 实现：

1. **渠道前置条件**：中台应用必须同时开启 `ChannelPassword` 与 `ChannelSMS`，且已绑定短信验证码场景，否则返回 `FORBIDDEN`。
2. **密码策略**：复用密码注册（register）的同一套 C2 策略 —— 最小长度取应用配置 `PasswordMinLength`（缺省走 `identityPasswordMinLen`），并按 `PasswordRequireAlnum` 校验字母数字组合。因此**重置不能设置比注册更弱的密码**。
3. **核身顺序**：先校验并消费短信验证码，再触碰账号数据 —— 未持有已送达验证码无法通过探测手机号是否已注册。
4. **首次设密码兼容**：若该用户此前只通过短信登录注册、尚未设置密码，重置会**创建首个密码绑定**而不是更新已有绑定（等价于补设密码）。
5. **吊销旧会话**：重置成功后调用 `RevokeUserRefreshSessions` 吊销该用户在当前 Client 下的全部 Refresh Token，随后签发新的 `UserTokens`。
6. **用户状态**：用户非 `ACTIVE` 时返回 `USER_FROZEN`。

### 2.3 错误码与 HTTP 状态

依据中台 `internal/service/errors.go` 的 `ErrorCode()` 与 `internal/api/admin_handlers.go` 的 `statusForError()`：

| HTTP | code | 场景 |
|---|---|---|
| 400 | `INVALID_INPUT` | 密码强度不足、手机号格式错误、必填项缺失 |
| 400 | `sms.verification.invalid_code` | 验证码错误 |
| 400 | `sms.verification.expired` | 验证码过期 |
| 400 | `sms.verification.locked` | 验证码尝试次数超限被锁定 |
| 403 | `USER_FROZEN` | 用户被冻结（`ErrUserFrozen` 包装 `ErrForbidden`） |
| 403 | `FORBIDDEN` | 密码登录/短信登录渠道未开通，或短信场景未绑定 |
| 404 | `NOT_FOUND` | 该手机号在当前 Client 下没有绑定用户 |
| 409 | `CONFLICT` | 密码绑定归属于另一用户（异常数据） |

### 2.4 SDK 实现要点

1. **复用既有模式**：`ResetPassword` 与 `RegisterPassword` / `LoginPassword` 结构完全一致，直接复用 `call[UserTokens]` 泛型辅助函数，无需新增请求基础设施。
2. **不缓存用户 Token**：与既有身份方法一致，`ResetPassword` 返回的 `UserTokens` 由产品线会话层管理，SDK 不缓存、不替换 Client Token。
3. **不自动重试**：验证码一次性消费，重试会得到 `sms.verification.invalid_code`；SDK 遵循"业务调用不自动重试"的既有原则。
4. **版本策略**：纯增量新增方法，不影响既有方法签名，按仓库 SemVer 规则提升 MINOR 发布为 v0.4.0。

## 三、其余模块核对结果（无需更新）

| 核对项 | 中台现状 | SDK 现状 | 结果 |
|---|---|---|---|
| Identity 9 路由 | password/register、password/login、**password/reset**、sms/send-code、sms/login、wechat qrcode 创建/状态/登录、token/refresh | `IdentityService` 8 → 9 方法一一对应 | ✅ 本次已补齐 |
| Images 11 路由 | 上传、直传会话创建/查询/reconcile、列表/详情、access-url、variants、删除、usage、call-records | `ImageService` 11 方法一一对应 | ✅ 一致 |
| SMS 4 路由 | verifications/send、verify、notifications/send、messages/{id} | `SMSService` 4 方法对应 | ✅ 一致 |
| WeChat MP 7 路由 | access-token 读取/刷新、qrcode/temp、users/{openid}、messages/custom、channels 列表、channels/{key}/menu | `WechatService` 7 方法对应 | ✅ 一致 |
| Files 7 路由 | upload-authorizations、upload-credentials、reconcile、get、list、access-urls、delete | `FilesService` 7 方法对应（2026-11-02 联调验收已确认） | ✅ 一致 |
| Token 失效逻辑 | UNAUTHORIZED/TOKEN_EXPIRED 处理，refresh 路径豁免 | `client.go` 已按 `/api/v1/identity/token/refresh` 豁免 | ✅ 一致 |
| 错误信封 | 嵌套 `{error:{code,message}}` + RFC 6749 扁平格式 | v0.2.0 已兼容两种格式 | ✅ 一致 |

### 3.1 两处需要特别说明的"看似不一致"

核对中有两处表面差异，实为设计使然，**无需改动 SDK**：

1. **SMS 请求体缺少 `app_id`**：中台服务层 `SendSMSVerificationRequest` 等结构体带有 `app_id` 字段，但中台 handler 统一用 `request.AppID = authctx.Principal(r.Context()).ClientID` 从 JWT 覆盖赋值（`internal/api/sms_handlers.go`）。客户端传入的 `app_id` 不被信任，因此 SDK 不暴露该字段是正确的。
2. **微信 Token 字段名**：中台 `ExportedWechatToken.Version` / `WechatTokenFailure.Version` / `.Code` 的 Go 字段名与 JSON tag 不同，但 json tag（`token_version` / `failed_token_version` / `wechat_errcode`）与 SDK `WechatAccessToken` / `WechatTokenFailure` 完全一致。

## 四、覆盖但未实现的接口（仅备案）

中台需求文档 `docs/api/password-reset-requirement.md` §3.2 规划了 `POST /api/v1/identity/password/change`（已登录用户用 **User Access Token** 修改密码）。该接口在中台 **尚未实现**（路由表中不存在），且需要 User Token 签发/校验链路改造，故 SDK 本次不封装。若中台后续落地，需届时新增独立方法并单独评估鉴权模型 —— 它使用终端用户 Token 而非 Client Token，与现有全部身份方法的双层令牌模型不同。

## 五、变更落地清单

| 文件 | 变更 |
|---|---|
| `identity.go` | 新增 `PasswordResetRequest` 类型与 `IdentityService.ResetPassword` 方法 |
| `client_test.go` | 新增 `TestIdentityResetPasswordContract`：断言路径、方法、请求体字段名（重点守护 `new_password`）与响应信封解码 |
| `docs/usage.md` | §2.2 补充重置密码用例、行为与错误码；§7 方法覆盖表补充 1 行；§8 与安装命令版本更新至 v0.4.0 |
| `README.md` | 接入版本 v0.2.0 → v0.4.0 |
| `CHANGELOG.md` | 新增 `## v0.4.0 - 2026-11-05` 条目 |

## 六、遗留风险提示（仅记录）

- **联调验收未覆盖**：`password/reset` 依赖真实短信验证码分发，无法在无人工注入的条件下自动化验收。SDK 侧本次只提供 httptest 契约测试；端到端验证需在测试环境由人工完成（与 files 模块配套 `acceptance/` 套件的策略不同）。
- **渠道开关依赖中台配置**：该接口在产品线接入前，需中台管理员为应用同时开通密码登录与短信登录渠道，并绑定短信验证码场景，否则调用即返回 `FORBIDDEN`。
- **身份归属待中台确认**：中台需求文档 §3.3 提出的"微信扫码用户设密码是否复用同一 `usr_xxx`"问题仍待中台管理员确认。该问题影响产品线"扫码用户补设密码"流程的可用性，但不改变本接口的 HTTP 契约。
