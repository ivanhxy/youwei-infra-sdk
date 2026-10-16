# Go SDK 使用文档

面向产品线 Go 后端开发者。SDK 统一处理鉴权、请求编码、响应解析和错误，覆盖身份、图片、短信、微信公众号 30 个业务接口。要求 Go 1.22+，只依赖标准库；产品项目无需引入中台后端或云厂商 SDK。

第一次接入按第 1 节跑通，再选择需要的模块。第 2～6 节的业务片段放在返回 `error` 的业务函数内，已持有 `client *infra.Client` 和请求的 `ctx context.Context`；`codeFromUser` 等变量来自本产品的请求参数。完整可运行程序见第 1 节。

| 需要完成的事情 | 阅读位置 |
| --- | --- |
| 准备凭据、安装、运行第一个调用 | 第 1 节 |
| 登录、注册、刷新用户凭证、扫码登录 | 第 2 节 |
| 上传、查询、访问、删除图片或终端直传 | 第 3 节 |
| 业务验证码、通知短信、公众号主动调用 | 第 4 节 |
| 接收公众号事件并验签 | 第 5 节 |
| 超时、错误码、重试与问题定位 | 第 6 节 |
| 查找全部方法、安装正式版本、接入验收 | 第 7～9 节 |

## 1. 从零接入

### 1.1 向中台管理员领取配置

每个产品应用使用自己的 OAuth Client，并区分开发、测试、生产配置。产品线后端需要拿到：

| 配置 | 来源与用途 |
| --- | --- |
| 中台服务地址 | 对应环境的 API 根地址，例如 `https://infra-api.example.com`；不包含 `/api/v1` 或 `/oauth/token` |
| `client_id`、`client_secret` | 管理员创建并安全交付的应用凭据；Secret 只放后端配置或密钥管理系统 |
| 能力授权 | 管理员开通需要的身份、图片、短信、公众号能力；有凭据不代表已获得所有权限 |
| `scene_code` | 图片、业务短信各自的场景编码，需已配置并授权给当前 Client；示例中的 `avatar`、`order_confirm` 等必须替换为真实值 |
| `channel_key` | 公众号渠道标识，需已授权；与业务产品的 Client ID 不同 |
| 回调 URL、回调 Secret | 仅接收公众号事件时需要，由双方配置并安全交付；不是 `client_secret` |

身份短信登录需要绑定登录验证码场景；密码注册还需同时启用密码、短信渠道。公众号扫码登录需开通身份扫码渠道并绑定可用公众号。微信取票另需单独的 Token 导出授权。SDK 不创建这些配置。

### 1.2 安装 SDK

Module 路径是 `github.com/ivanhxy/youwei-infra-sdk`，导入别名使用 `infra`。它是独立发布的公开 module，业务只依赖这个路径。

在产品项目根目录安装指定版本：

```bash
go get github.com/ivanhxy/youwei-infra-sdk@v0.1.0
```

已有 Go 项目继续使用自己的 `go.mod`；仅新建项目时先运行 `go mod init <你的产品模块路径>`。SDK 公开下载，无需中台仓库权限或私有 Git 凭据。

### 1.3 跑通第一个调用

以下程序查询图片列表，需要当前 Client 已开通图片能力。保存为产品项目的 `cmd/infra-check/main.go`：

```go
package main

import (
    "context"
    "log"
    "os"
    "time"

    infra "github.com/ivanhxy/youwei-infra-sdk"
)

func main() {
    client, err := infra.NewClient(infra.Config{
        BaseURL:      os.Getenv("INFRA_BASE_URL"),
        ClientID:     os.Getenv("INFRA_CLIENT_ID"),
        ClientSecret: os.Getenv("INFRA_CLIENT_SECRET"),
    })
    if err != nil {
        log.Fatal(err)
    }
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    result, err := client.Images.List(ctx, infra.ListImagesRequest{Page: 1, PageSize: 20})
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("图片总数=%d 当前页=%d request_id=%s", result.Data.Total, len(result.Data.Images), result.RequestID)
}
```

通过环境变量或产品现有配置系统注入对应值，再执行：

```bash
export INFRA_BASE_URL=https://infra-api.example.com
export INFRA_CLIENT_ID=your_client_id
# INFRA_CLIENT_SECRET 由本产品的安全配置注入，不提交到 Git。
go mod tidy
go run ./cmd/infra-check
```

成功会打印图片总数、当前页数量和 Request ID；空列表也是成功。出现 `invalid_client` 先核对环境和凭据；出现 `invalid_scope` 或 403 按第 6 节核对能力授权。未开通图片的 Client 可选择已授权模块作为首次调用。

以上程序在产品项目中运行，只需要对应环境的地址、凭据及授权。调用中台服务不要求产品同事在本机启动整套中台。

### 1.4 在业务服务中复用 Client

应用启动时每组凭据创建一次 Client，通过产品现有的依赖注入方式交给业务服务，跨 goroutine 复用。不要每个请求都重新创建 Client。业务调用使用上游请求的 `Context`，按业务设置超时；不要用请求结束后已取消的 Context 启动后台工作。

| `infra.Config` 字段 | 必填 | 行为 |
| --- | --- | --- |
| `BaseURL` | 是 | 最终 HTTP(S) 根地址，可带部署路径前缀；不能带用户名密码、查询参数或 fragment。SDK 不自动跟随重定向 |
| `ClientID` / `ClientSecret` | 是 | 中台应用凭据；不接受 User Token 代替 |
| `Scope` | 否 | 空格分隔；省略则申请已授权范围的合集，指定时必须是已授权范围的子集 |
| `HTTPClient` | 否 | 自定义超时、代理或 Transport；省略时每次 HTTP 请求超时 30 秒。自定义 Client 的 `Timeout` 原样生效，重定向仍被 SDK 禁止 |

例如只调用图片列表并将 HTTP 超时改为 10 秒（需导入 `net/http`、`time`）：

```go
client, err := infra.NewClient(infra.Config{
    BaseURL:      os.Getenv("INFRA_BASE_URL"),
    ClientID:     os.Getenv("INFRA_CLIENT_ID"),
    ClientSecret: os.Getenv("INFRA_CLIENT_SECRET"),
    Scope:        "image.read",
    HTTPClient:   &http.Client{Timeout: 10 * time.Second},
})
if err != nil {
    return err
}
_, err = client.Images.List(ctx, infra.ListImagesRequest{Page: 1, PageSize: 20})
if err != nil {
    return err
}
```

`NewClient` 只校验配置，不发网络请求；第一次业务调用才向 `/oauth/token` 换证。SDK 在内存中缓存 Client Token，在到期前 `min(5分钟, 有效期的10%)` 进入续取区间后，由下一次调用触发续取，没有后台定时任务。同一 Client 共享取证和缓存，不同进程各自管理缓存，无需产品再存一份 Client Token。

凭据轮换时用新配置创建 Client 并替换业务依赖。管理员新增权限后，旧缓存 Token 不会自动增加 scope；需要立即使用新权限时，按新配置重新创建 Client。403 不会被自动重试。

## 2. 身份登录

### 2.1 手机验证码登录

```go
// 登录/注册验证码与业务短信验证码使用不同接口。
_, err = client.Identity.SendSMSCode(ctx, "13800138000")
if err != nil {
    return err
}
login, err := client.Identity.LoginSMS(ctx, infra.SMSLoginRequest{
    Mobile: "13800138000", Code: codeFromUser,
})
if err != nil {
    return err
}
// 业务保存 login.Data.UserID、AccessToken 和 RefreshToken。
// RefreshToken 是轮转凭证，刷新成功后必须替换旧值。
refreshed, err := client.Identity.RefreshToken(ctx, login.Data.RefreshToken)
if err != nil {
    return err
}
```

发送和提交验证码通常是两次独立的前端操作，按本产品交互拆开上述调用。手机号字段是 `Mobile`，不要与业务短信的 `Phone` 混用。是否允许登录时自动注册由管理员配置，结果中的 `IsNewUser` 可供业务判断。

### 2.2 密码注册与登录

注册前同样调用 `Identity.SendSMSCode`，取得用户提交的验证码后：

```go
registered, err := client.Identity.RegisterPassword(ctx, infra.PasswordRegisterRequest{
    Mobile: mobileFromUser, Code: codeFromUser, Password: passwordFromUser,
})
if err != nil {
    return err
}
// registered.Data 为注册后签发的用户凭证，不打印或记录其中的 Token。
```

后续密码登录单独调用：

```go
login, err := client.Identity.LoginPassword(ctx, infra.PasswordLoginRequest{
    Mobile: mobileFromUser, Password: passwordFromUser,
})
if err != nil {
    return err
}
// login.Data.UserID 对应当前 Client 下的用户。
```

密码策略由中台当前应用配置决定；注册不可跳过短信核身。`ALREADY_EXISTS` 表示已注册，`INVALID_CREDENTIALS` 表示账号或密码错误，不能通过重复发送或自动重放登录来解决。

### 2.3 用户凭证的保存与刷新

| 数据 | 由谁管理 | 使用方式 |
| --- | --- | --- |
| `client_secret` | 产品线后端 | 创建 Client；不下发终端，不写日志 |
| Client Token | SDK 自动管理 | 调用中台业务接口；不替换为登录返回的用户 Token |
| `UserTokens.AccessToken` | 产品线会话层 | 结合 `UserID`、`ExpiresIn` 管理终端登录态，不作为 SDK 配置 |
| `UserTokens.RefreshToken` | 产品线会话层 | 安全保存；刷新成功后连同新的 Access Token 一起替换旧值 |

同一用户会话的刷新应串行处理，避免多个请求同时消费同一份 Refresh Token。刷新失败时不要继续使用已轮转的旧值；用户凭证失效需回到登录流程。用户登录成功不代表产品自己的业务权限已经校验，产品后端仍需管理会话和资源访问权限。

### 2.4 公众号扫码登录

先申请二维码，将 `QRURL` 展示给用户，并在产品会话中绑定 `LoginID`：

```go
qr, err := client.Identity.CreateWechatLoginQR(ctx, "mp1")
if err != nil {
    return err
}
// 保存 qr.Data.LoginID 与本产品发起登录的会话关系，展示 qr.Data.QRURL。
```

终端经由产品后端查询状态；以下片段表示一次查询，调用间隔和停止条件由本产品控制：

```go
status, err := client.Identity.GetWechatLoginStatus(ctx, loginID)
if err != nil {
    return err
}
if status.Data.Status == "confirmed" {
    login, err := client.Identity.LoginWechat(ctx, loginID)
    if err != nil {
        return err
    }
    // 使用 login.Data 建立本产品会话；这里不要再次换证。
}
```

状态为 `pending` 时继续等待；`expired`、`rejected` 时停止并重新发起；`consumed` 表示已被换证消费。`LoginWechat` 只能消费一次确认会话；不要通过公众号 Webhook 或 OpenID 直接签发登录会话，也不要用业务临时二维码代替身份登录二维码。

## 3. 图片

### 3.1 后端上传与获取访问地址

```go
file, err := os.Open("avatar.png")
if err != nil {
    return err
}
defer file.Close()
uploaded, err := client.Images.Upload(ctx, infra.UploadImageRequest{
    SceneCode: "avatar", Filename: "avatar.png", File: file,
    OperatorID: "user_1", BizRefType: "user", BizRefID: "user_1",
    IdempotencyKey: "avatar:user_1:change_1",
})
if err != nil {
    return err
}
imageID := uploaded.Data.Image.ImageID
access, err := client.Images.CreateAccessURL(ctx, imageID)
if err != nil {
    return err
}
```

上传按流发送，SDK 不关闭调用方传入的文件。数据库保存图片 ID；私有 URL 有时效，按需调用 `CreateAccessURL`。

`SceneCode`、`Filename`、`File` 是上传的核心输入；`OperatorID` 记录业务操作人，`BizRefType` / `BizRefID` 记录业务归属，这些字段不替代本产品的权限检查。`IdempotencyKey` 用于标识一次业务上传意图，重试同一意图复用原值，新的图片变更使用新值。上传策略、大小及格式限制由场景配置决定。

`CreateAccessURL` 的有效期由服务端策略决定；方法不接受自定义 TTL。不要将私有临时 URL 当作永久资源地址保存。

### 3.2 列表、详情、派生图和删除

列表保留分页和筛选：

```go
page, err := client.Images.List(ctx, infra.ListImagesRequest{
    SceneCode: "avatar", Status: "ACTIVE", AccessType: "private",
    Page: 1, PageSize: 20,
})
// page.Data.Images / Total / Page / PageSize
if err != nil {
    return err
}
```

`Page` 从 1 开始，`PageSize` 为 1～100；省略时服务端按第 1 页、每页 20 条处理。筛选字段可留空；状态值区分大小写，例如 `ACTIVE`，访问类型使用 `public` / `private`。SDK 每次返回一页，不会自动遍历所有图片。

```go
detail, err := client.Images.Get(ctx, imageID)
if err != nil {
    return err
}
variant, err := client.Images.CreateVariant(ctx, imageID, infra.CreateVariantRequest{
    VariantType: "thumbnail", Width: 200, Height: 200,
})
if err != nil {
    return err
}
// 业务确认删除后再调用；不要将删除放在普通查询链路中。
deleted, err := client.Images.Delete(ctx, imageID)
if err != nil {
    return err
}
```

派生图是否可用取决于场景策略和处理器配置。删除会改变资源状态；应先确认当前业务允许删除。`Images.Usage(ctx)` 返回当前 Client 的汇总，`Images.CallRecords(ctx)` 返回调用记录数组，当前方法不接受日期或分页参数。

### 3.3 终端直传

1. 产品后端调用 `CreateDirectUploadSession`。
2. 将返回的 `Upload` 临时指令交给终端，终端按指令向对象存储发送文件。
3. 产品后端调用 `GetDirectUploadSession` 查询结果，以 `completed` 且返回 Image 判定入库成功。云端 HTTP 上传成功本身不代表中台已完成入库。

```go
session, err := client.Images.CreateDirectUploadSession(ctx, infra.CreateDirectUploadRequest{
    SceneCode: "avatar", OriginalFilename: "avatar.png",
    FileSize: fileSize, ContentType: "image/png",
    BizRefType: "user", BizRefID: "user_1",
    IdempotencyKey: "avatar:user_1:change_2",
})
if err != nil {
    return err
}
// 将 session.Data.Upload 和临时有效期交给当前业务允许的终端。
```

后续独立的状态查询：

```go
status, err := client.Images.GetDirectUploadSession(ctx, uploadSessionID)
if err != nil {
    return err
}
if status.Data.Status == "completed" && status.Data.Image != nil {
    // 持久化 status.Data.Image.ImageID。
}
```

`pending` 表示等待完成，`expired` / `failed` 时停止并处理失败。`ReconcileDirectUploadSession` 仅用于回调丢失后的兜底，不是日常完成步骤；刚创建就重查可能得到 `RECONCILE_NOT_READY`。终端严格使用 `Upload` 返回的 URL、Method 和 Headers：`binary_put` 直接发送文件二进制；`multipart_form` 将全部 FormFields 加入表单，再用 FileFieldName 指定的字段追加文件，浏览器自行生成 multipart Content-Type。SDK 不代传云端文件，也不向云存储发送中台 Bearer。终端只获得临时指令，不获得 Client Token 或 Secret。

## 4. 业务短信与公众号

### 4.1 业务验证码

适用于订单确认等业务核身。登录验证码使用第 2 节的身份方法；不要跨模块消费验证码。先发送，用户填写验证码后再核验：

```go
verification, err := client.SMS.SendVerification(ctx, infra.SendVerificationRequest{
    SceneCode: "order_confirm", Phone: "13800138000", BusinessKey: "order_1",
})
if err != nil {
    return err
}
verified, err := client.SMS.VerifyCode(ctx, infra.VerifyCodeRequest{
    SceneCode: "order_confirm", Phone: "13800138000", BusinessKey: "order_1", Code: codeFromUser,
})
if err != nil {
    return err
}
```

发送与核验必须使用相同的 `SceneCode`、`Phone` 和 `BusinessKey`；`BusinessKey` 应绑定本次业务对象，由产品后端生成或校验。发送结果中的 `ExpiresIn` 是验证码有效期，`MessageID` 可用于查询；真正通过核验后才继续业务。核验可能消费验证码，不要把它当作可无限重复的只读检查。

### 4.2 通知短信与送达状态

```go
message, err := client.SMS.SendNotification(ctx, infra.SendNotificationRequest{
    SceneCode: "order_shipped", Phone: "13800138000",
    TemplateParams: map[string]string{"order_no": "order_1"},
    IdempotencyKey: "order_1:shipped",
})
if err != nil {
    return err
}
delivery, err := client.SMS.GetMessage(ctx, message.Data.MessageID)
if err != nil {
    return err
}
```

`TemplateParams` 的键必须与已配置模板的变量匹配。发送成功表示提交被接受，最终送达以 `GetMessage` 的状态为准。`IdempotencyKey` 绑定一次业务通知，同一业务重试复用该值，不同收件人或不同通知使用不同值；不能每次重试都随机生成新值。

### 4.3 公众号常用方法

```go
channels, err := client.Wechat.Channels(ctx)
if err != nil {
    return err
}
qr, err := client.Wechat.CreateTempQR(ctx, infra.CreateTempQRRequest{
    ChannelKey: "mp1", SceneStr: "campaign_spring", ExpireSeconds: 300,
})
if err != nil {
    return err
}
user, err := client.Wechat.GetUser(ctx, openID, "mp1")
if err != nil {
    return err
}
sent, err := client.Wechat.SendCustomMessage(ctx, infra.SendCustomMessageRequest{
    ChannelKey: "mp1", OpenID: openID, MsgType: "text", Content: "您好",
})
if err != nil {
    return err
}
menu, err := client.Wechat.GetMenu(ctx, "mp1")
if err != nil {
    return err
}
```

`Channels` 返回当前 Client 可用的渠道，调用时使用返回的 `ChannelKey`；示例 `mp1` 需替换。客服消息当前只支持文本，内容最多 2048 字节，并受微信自身的发送限制。业务二维码的 `SceneStr` 不能使用身份登录保留的 `scan_login:` / `qrscene_scan_login:` 前缀。

### 4.4 直接调用微信 API 时取票

使用上述中台代理方法时无需自己取微信 Token。只有产品确需直接调用微信 API，才使用以下方法，并先让管理员开通 Token 导出授权：

```go
token, err := client.Wechat.GetAccessToken(ctx, "mp1")
if err != nil {
    return err
}
// 用 token.Data.AccessToken 调用微信，并保留这次请求对应的 TokenVersion。
// 仅当微信明确返回 40014 / 42001，才执行下面的恢复调用。
recovered, err := client.Wechat.RecoverAccessToken(ctx, "mp1", infra.WechatTokenFailure{
    FailedTokenVersion: token.Data.TokenVersion, WechatErrCode: 42001,
})
if err != nil {
    return err
}
// recovered.Data 是恢复结果，不在日志或接口中暴露 AccessToken。
```

SDK 不维护第二份微信 Token 缓存，也不自动刷新并重发微信业务请求；恢复冷却通过 `APIError.RetryAfter` 返回。

## 5. 回调验签

在产品自己的后端注册回调入口，将 URL 和独立的回调 Secret 配置到中台。以下为验签与可靠接收的函数示例，需要导入 `context`、`io`、`net/http`、`time` 和 `infra`；`persist` 由产品实现：

```go
func wechatWebhook(
    secret, expectedClientID string,
    persist func(context.Context, *infra.WechatEvent) error,
) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            w.Header().Set("Allow", http.MethodPost)
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
            return
        }
        body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
        if err != nil {
            http.Error(w, "invalid body", http.StatusBadRequest)
            return
        }
        event, err := infra.ParseWechatEvent(
            secret, r.Header.Get("X-Infra-Timestamp"),
            r.Header.Get("X-Infra-Signature"), body, time.Now(),
        )
        if err != nil || event.InfraClientID != expectedClientID {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        if err := persist(r.Context(), event); err != nil {
            http.Error(w, "receive failed", http.StatusInternalServerError)
            return
        }
        w.WriteHeader(http.StatusNoContent)
    }
}
```

`persist` 应按 `EventID` 去重并可靠写入本产品的持久存储或队列，重复接收可返回成功，保存失败返回错误。不要先回复成功再只放到内存 goroutine 中处理。用户 OpenID 在 `event.Context.OpenID`，事件类型在 `event.Wechat.Event`；只需验签时调用 `VerifyWebhook`。

验签使用未经解析和重新编码的原始请求体，包含正负 300 秒时间窗和常量时间比较；产品服务器需保持时钟同步。时间窗内的重复事件仍须去重。当前中台事件分发为异步单次投递，没有自动重试或可靠补投保证；业务不能仅依赖该回调完成不可丢失的状态流转，扫码登录以身份状态查询和一次性换证为准。

当前服务端事件字段是 `event_id / delivered_at / channel_key / app_id / infra_client_id / wechat / context`。旧分册中的扁平 `event_type / client_id / payload` 示例不适用于当前事件分发器；SDK 的结构通过服务端信封序列化测试验证，不做字段猜测。

## 6. 错误与重试

所有业务方法返回 `*Response[T]`，其中 `Data` 为类型化结果，`RequestID` 用于排查。链路 ID 可用 `infra.WithRequestID(ctx, requestID)` 传入。

```go
ctx = infra.WithRequestID(ctx, requestID)
result, err := client.Images.List(ctx, infra.ListImagesRequest{Page: 1, PageSize: 20})
if err != nil {
    var apiErr *infra.APIError
    switch {
    case errors.As(err, &apiErr):
        log.Printf("infra status=%d code=%s request_id=%s retry_after=%s",
            apiErr.StatusCode, apiErr.Code, apiErr.RequestID, apiErr.RetryAfter)
    case errors.Is(err, context.DeadlineExceeded):
        log.Print("infra request timed out")
    case errors.Is(err, context.Canceled):
        log.Print("infra request canceled")
    default:
        log.Print("infra transport or response error")
    }
    return err
}
// 只有 err == nil 才使用 result.Data。
```

上例需要 `errors`、`log`。网络、取消、编码和响应协议异常返回普通 Go error；HTTP/API 错误返回 `*APIError`。`RetryAfter` 保留响应头原值，可能为空，不是已解析的时长。SDK 不记录请求体、Token 或 Secret；产品日志也不要直接输出登录结果、请求密码、验证码或微信凭证。

SDK 不自动重试或重放业务请求。Client Token 被 `401 UNAUTHORIZED` / `TOKEN_EXPIRED` 拒绝时会清除该缓存，下次调用重新取证。用户 `RefreshToken` 接口的 `UNAUTHORIZED` 同时用于已失效的用户刷新凭证，SDK 在该接口仅按明确的 `TOKEN_EXPIRED` 清除 Client 缓存；其他换证失败交给业务处理。登录密码错误、能力关闭、权限撤回不通过刷新绕过。上传、发短信和一次性登录换证失败后，由产品线结合错误码、幂等键及查询结果决定是否重试。

### 6.1 常见问题排查

| 现象或错误码 | 先检查什么 | 产品应如何处理 |
| --- | --- | --- |
| `invalid_client` | 服务环境、Client ID、Secret、Client 是否启用 | 修正凭据；不要循环重试 |
| `invalid_scope` | 是否开通任一能力；显式 `Scope` 是否超出授权 | 让管理员开通能力或缩小申请范围 |
| 403 / `FORBIDDEN`、`SCENE_UNAUTHORIZED`、`SERVICE_DISABLED` 等 | 能力开关、场景与渠道授权；是否复用了开通权限前的旧 Token | 修正授权；必要时重建 Client 取新 Token |
| `INVALID_INPUT` / `INVALID_REQUEST` | 当前方法参数、大小写、分页范围 | 修正入参；不要按网络故障重试 |
| `INVALID_CREDENTIALS` | 用户提交的账号密码 | 向用户反馈登录失败；与应用 Secret 错误区分 |
| 用户刷新返回 `UNAUTHORIZED` | 刷新凭证是否已过期、撤销或被另一个请求消费 | 重新登录；不能继续重用旧刷新凭证 |
| `sms.verification.invalid_code`、`sms.verification.expired`、`sms.verification.locked` | 验证码及发送、核验是否绑定同一业务对象 | 让用户修正或重新获取，不自动重发短信 |
| HTTP 429 | `RetryAfter` 与当前场景频控 | 等待冷却；没有响应头时遵循场景限制 |
| `NOT_FOUND` / `IMAGE_GONE` | 资源 ID、所属 Client、删除和过期状态 | 核对归属或清理产品端失效引用 |
| 重定向、HTML 响应、协议错误 | BaseURL、网关和最终 HTTPS 地址 | 配置最终 API 地址，排除登录页或代理错误 |
| 网络超时或 5xx | 请求是否可能已被执行 | 读查询可由业务有限重试；写操作先核对状态及幂等键 |

排查时提供 SDK 版本、中台环境、方法名、HTTP 状态、错误码和 Request ID，避免发送完整 Token、Secret 或原始用户资料。

### 6.2 重试边界

- 列表、详情、状态查询：网络失败后可按业务超时预算有限重试；已取消的 Context 不能复用。
- 图片上传、通知短信：复用同一次业务的幂等键；文件流重试前重新打开或复位。SDK 不替调用方重新读取文件。
- 用户凭证刷新、扫码换证、验证码核验：可能消费一次性凭证，超时不等于未执行；不要通用重试。
- 发送验证码、微信发消息：不能假设接口具备幂等能力，先明确业务结果再决定后续动作。

## 7. 方法覆盖表

| 模块 | SDK 方法 | 对外接口 |
| --- | --- | --- |
| 身份 | `Identity.SendSMSCode` | `POST /api/v1/identity/sms/send-code` |
| 身份 | `Identity.LoginSMS` | `POST /api/v1/identity/sms/login` |
| 身份 | `Identity.RegisterPassword` | `POST /api/v1/identity/password/register` |
| 身份 | `Identity.LoginPassword` | `POST /api/v1/identity/password/login` |
| 身份 | `Identity.RefreshToken` | `POST /api/v1/identity/token/refresh` |
| 身份 | `Identity.CreateWechatLoginQR` | `POST /api/v1/identity/wechat/login/qrcode` |
| 身份 | `Identity.GetWechatLoginStatus` | `GET /api/v1/identity/wechat/login/qrcode/{login_id}` |
| 身份 | `Identity.LoginWechat` | `POST /api/v1/identity/wechat/login/qrcode/{login_id}/login` |
| 图片 | `Images.Upload` / `Images.List` | `POST` / `GET /api/v1/images` |
| 图片 | `Images.Get` / `Images.Delete` | `GET` / `DELETE /api/v1/images/{image_id}` |
| 图片 | `Images.CreateDirectUploadSession` | `POST /api/v1/images/direct-upload-sessions` |
| 图片 | `Images.GetDirectUploadSession` | `GET /api/v1/images/direct-upload-sessions/{upload_session_id}` |
| 图片 | `Images.ReconcileDirectUploadSession` | `POST /api/v1/images/direct-upload-sessions/{upload_session_id}/reconcile` |
| 图片 | `Images.CreateAccessURL` | `POST /api/v1/images/{image_id}/access-url` |
| 图片 | `Images.CreateVariant` | `POST /api/v1/images/{image_id}/variants` |
| 图片 | `Images.Usage` / `Images.CallRecords` | `GET /api/v1/image-usage` / `GET /api/v1/image-call-records` |
| 短信 | `SMS.SendVerification` | `POST /api/v1/sms/verifications/send` |
| 短信 | `SMS.VerifyCode` | `POST /api/v1/sms/verifications/verify` |
| 短信 | `SMS.SendNotification` | `POST /api/v1/sms/notifications/send` |
| 短信 | `SMS.GetMessage` | `GET /api/v1/sms/messages/{message_id}` |
| 公众号 | `Wechat.Channels` | `GET /api/v1/wechat/mp/channels` |
| 公众号 | `Wechat.CreateTempQR` | `POST /api/v1/wechat/mp/qrcode/temp` |
| 公众号 | `Wechat.GetUser` | `GET /api/v1/wechat/mp/users/{openid}` |
| 公众号 | `Wechat.SendCustomMessage` | `POST /api/v1/wechat/mp/messages/custom` |
| 公众号 | `Wechat.GetMenu` | `GET /api/v1/wechat/mp/channels/{channel_key}/menu` |
| 公众号 | `Wechat.GetAccessToken` | `GET /api/v1/wechat/mp/channels/{channel_key}/access-token` |
| 公众号 | `Wechat.RecoverAccessToken` | `POST /api/v1/wechat/mp/channels/{channel_key}/access-token/refresh` |

第三方推送到中台的云存储、短信、微信 Portal 回调入口属于服务端入站协议，不作为产品线 SDK 调用方法。后台管理 API 不在 SDK 范围内。

## 8. 正式版本安装与升级

当前接入版本为 **`v0.1.0`**。SDK 采用独立的 SemVer 版本，不随根应用 `VERSION` 一起变更。仓库标签是 `v0.1.0`，产品侧安装使用 `@v0.1.0`。变更内容见 [SDK 变更记录](../CHANGELOG.md)。

### 8.1 公开模块访问

SDK 通过公开 Git 仓库的版本标签分发，由 Go 模块代理缓存，无需注册 pkg.go.dev 账号，也无需私有仓库权限。使用 Go 默认代理配置或团队现有的公共 Go 代理即可。

下载 SDK 不需要中台应用凭据；调用中台 API 时仍须由产品后端注入自己的地址与凭据，并取得对应能力授权。SDK 公开不改变服务端的鉴权规则。

如果已有 `GOPRIVATE`、`GONOPROXY` 或 `GONOSUMDB` 规则覆盖 SDK 模块路径，按团队配置移除对应匹配项，使该公开模块通过公共代理与校验服务安装；保留其他私有模块规则。

### 8.2 固定、升级与回退

在产品项目中执行：

```bash
go get github.com/ivanhxy/youwei-infra-sdk@v0.1.0
go mod tidy
go list -m github.com/ivanhxy/youwei-infra-sdk
go test ./...
```

将产品项目的 `go.mod`、`go.sum` 提交到版本库，固定经过验证的具体版本。升级时先阅读变更记录，将安装命令的版本号改为目标版本，在测试环境跑通实际使用的业务流程，再按产品发布流程上线；回退时固定回上一个已验证版本。不要用 `@latest` 作为自动上线策略。

版本规则：修复使用 PATCH，兼容功能使用 MINOR；`v0.x` 的破坏性变更必须提升 MINOR 并提供迁移说明。`v1.0.0` 起，破坏性变更提升 MAJOR；未来 `v2` 必须同时调整 module/import 路径为 `/v2`。已发布标签不可覆盖或移动，发现问题发布新版本。SDK 新增接口上线前，先确认目标中台环境已支持该接口。

## 9. 接入验收与维护

### 9.1 产品线接入完成的判断

1. 在本产品中完成依赖安装，按对应环境注入地址与凭据，启动时复用 Client。
2. 跑通至少一个已授权接口，能用 Request ID 定位失败。
3. 按实际使用的模块确认权限拒绝、超时和业务错误的处理；涉及用户刷新时确认新凭证替换旧值。
4. 涉及上传时保存图片 ID；涉及短信时区分提交与送达；涉及回调时完成原始报文验签、接收持久化和去重。
5. 固定经过验证的 SDK 版本，并确认同事与 CI 可以按同一版本安装和构建。

产品同事只需运行本产品自身的测试。SDK 维护者的版本规则、CI 和自动发布流程见 [SDK 版本与自动发布](releasing.md)。

SDK 单测使用本机 HTTP 替身，覆盖凭据隔离、并发缓存、错误处理、流式上传和 Webhook 验签。单测不代表真实短信、微信、云存储业务已验收；这些链路仍需结合目标环境的配置和授权联调。
