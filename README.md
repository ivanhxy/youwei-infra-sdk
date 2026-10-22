# Youwei Infra Center Go SDK

面向产品线后端的 Go SDK，接入版本 **v0.2.0**。Go 1.26+，仅使用标准库；无需引入中台后端、数据库驱动或云厂商 SDK。

完整说明见 [Go SDK 使用文档](docs/usage.md)，从领取凭据、安装到业务调用和问题排查按顺序接入：

- [首次接入与完整运行程序](docs/usage.md#1-从零接入)
- [身份登录](docs/usage.md#2-身份登录)、[图片](docs/usage.md#3-图片)、[短信与公众号](docs/usage.md#4-业务短信与公众号)
- [回调验签](docs/usage.md#5-回调验签)、[错误与重试](docs/usage.md#6-错误与重试)
- [正式版本安装与升级](docs/usage.md#8-正式版本安装与升级)

在产品项目中安装：

```bash
go get github.com/ivanhxy/youwei-infra-sdk@v0.2.0
```

SDK 公开下载，无需私有仓库权限；中台凭据只在运行时注入产品后端。版本遵循 SemVer，使用独立标签 `v0.2.0`，详情见 [变更记录](CHANGELOG.md)。

下面是已有 `ctx` 的业务函数内的调用片段；需要导入 `os`，并使用查询结果：

```go
import infra "github.com/ivanhxy/youwei-infra-sdk"

client, err := infra.NewClient(infra.Config{
    BaseURL:      os.Getenv("INFRA_BASE_URL"),
    ClientID:     os.Getenv("INFRA_CLIENT_ID"),
    ClientSecret: os.Getenv("INFRA_CLIENT_SECRET"),
})
if err != nil {
    return err
}
result, err := client.Images.List(ctx, infra.ListImagesRequest{Page: 1, PageSize: 20})
```

完整可运行程序及环境变量配置见 [首次接入](docs/usage.md#1-从零接入)。

客户端可并发复用，自动缓存并在过期前续取 Client Token。用户登录和刷新返回的 User Token 交给业务保存，不会替换 Client Token。

SDK 不自动重试业务请求。Client Token 失效会清除缓存，下次调用重新取证；用户刷新凭证的失效不会混入 Client 缓存。发送短信、上传、登录换证等操作是否重试由业务决定，详见接入指南的错误处理约定。

验证：`make test`、`go test -race ./...`、`go vet ./...`。支持 Go 1.26+，仅依赖标准库。

修改 SDK 并更新 CHANGELOG 后通过 PR 合入 `main`，GitHub Actions 自动测试、发布版本标签和 Release，并验证公共 Go 代理安装，详见 [SDK 版本与自动发布](docs/releasing.md)。

许可证：[MIT](LICENSE)。
