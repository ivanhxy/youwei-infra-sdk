# SDK 版本与自动发布

此仓库仅维护 Go SDK。公开 module 为 `github.com/ivanhxy/youwei-infra-sdk`，SDK 使用独立 SemVer，版本以 CHANGELOG 首个版本标题及 Git 标签为准；标签为 `v0.1.0`，与中台应用发布无关。许可证为 MIT。

## 发布新版本

1. 在功能分支修改 SDK、补充测试，并在 CHANGELOG 顶部增加新版本，更新 README 和使用指南的安装版本。
2. 通过 PR 合入 `main`。
3. CHANGELOG 变更合入 `main` 后，GitHub Actions 的 Release 工作流执行 `make release`，自动完成单测、发布脚本自检、竞态检测、vet、附注标签推送和公共 Go 代理安装验收，再创建 GitHub Release。无需人工打标签或上传包。

CI 在 PR 和 `main` 推送时检查 Go 1.22 与当前稳定版；Release 复用相同 CI 检查，全部通过后才发布。发布任务串行执行，仅使用 GitHub 内置的短期 `GITHUB_TOKEN` 写入本仓库标签与 Release；PR 检查只有读取权限，无需个人访问令牌。

修复提升 PATCH，兼容功能提升 MINOR；`v0.x` 破坏性变更提升 MINOR 并提供迁移说明。`v1` 起破坏性变更提升 MAJOR，`v2` 起需要在 module/import 路径增加 `/v2`，同步升级发布脚本；当前脚本只接受 `v0` / `v1` 稳定版本。已发布标签不可覆盖或移动。

## 失败与重跑

公共 Go 代理可能短时缓存尚未找到的版本。标签推送后若安装失败，不删除或移动标签；网络恢复或代理刷新后，在 Actions 中重跑 Release 或手动触发，在同一发布提交重跑。同一提交和同一版本可重复验收；SDK 需要修改时使用更高版本。

```bash
make release-check # 只检查，不推送标签
make release       # 检查、发布标签、公共代理安装验收
```

安装验收使用独立消费项目和全新 module cache，启用公共 checksum database，核对安装版本并编译。

## 日常验证

```bash
make test
go test -race ./...
go vet ./...
```

SDK 测试只访问本机 HTTP 替身，不发送真实短信、请求微信或上传云存储。真实业务权限与外部服务由接入产品另行联调。
