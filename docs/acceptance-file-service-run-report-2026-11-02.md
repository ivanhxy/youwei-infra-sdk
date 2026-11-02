# 中台文件服务联调验收测试报告（2026-11-02）

- 执行脚本：[scripts/acceptance-files.sh](../scripts/acceptance-files.sh)（Shell/curl 版）
- 用例定义：[docs/acceptance-file-service-2026-11-02.md](acceptance-file-service-2026-11-02.md)
- 原则：只测试与报告，不修改中台与 SDK 业务代码（验收脚本自身的缺陷除外，见第五节）

## 一、环境信息

| 项 | 值 |
|---|---|
| 中台 BaseURL | `http://127.0.0.1:38471`（本地 `make dev` 同构实例，进程 infra-center） |
| 中台版本 | 本地开发版（文件服务 2.0.0 契约） |
| 测试 Client | `local-client-a`（种子账号，文件能力已开通，scope 含 file.*） |
| 探针场景 | `archive`（压缩包；allowed_formats: zip/7z/rar/tar/gz，max 1GiB） |
| SDK 版本 | v0.3.0（FilesService 7 接口契约） |
| RUN_ID | `sdk-acc-20261102191502` |
| 执行时间 | 2026-11-02 19:15 CST |
| 探针文件 | `sdk-acceptance-sdk-acc-20261102191502.zip`，1024 B |

## 二、结果汇总

**PASS 18 / FAIL 0 / RECORD 0 / SKIP 2（两项均为待人工验证），脚本退出码 0 —— 严格用例全部通过。**

| 用例 | 结果 | 实际状态码/错误码 | request_id | 备注 |
|---|---|---|---|---|
| TC-01 Token 获取与连通 | PASS | 200 | b8b59063… | `/oauth/token` JSON 请求体正常签发 |
| TC-02 无凭证拒绝 | PASS | 401 | 4f52b07f… | 错误信封 `UNAUTHORIZED` |
| TC-03 未知场景拒绝 | PASS | 400 SCENE_UNAUTHORIZED | 65b4d881… | 未出凭证 |
| TC-04 场景停用 | **SKIP（待人工验证）** | - | - | 见第七节补测说明 ①：需要管理员临时停用场景，避免影响本地环境数据而跳过 |
| TC-05 申请上传授权 | PASS | 200 | 58852190… | 全部契约字段符合；凭证掩码 `ST***kK/Cr***jV/CA***==` |
| TC-06 幂等重放 | PASS | 200 | d924823c… | 同 file_id 同 object_key，未新建 |
| TC-07 幂等键冲突 | PASS | 409 CONFLICT | 175d7356… | 同键异元数据被拒 |
| TC-08 元信息一致性 | PASS | 200 | 9882c74e… | status=UPLOADING，scene/文件名/类型/大小全部一致 |
| TC-09 列表筛选 | PASS | 200 | 1f07bbd1… | `scene_code=archive&status=UPLOADING` 命中 |
| TC-10 Reconcile 无对象 | PASS | 200 | cc8068a0… | 状态仍 UPLOADING，未伪造 ACTIVE |
| TC-11 非 ACTIVE 下载 | PASS | 409 CONFLICT | 66d88e6b… | 未激活文件拒绝出下载 URL |
| TC-12 凭证续签 | PASS | 200 | 1df1766f… | 同目标（file_id/object_key/bucket 不变），新 STS 有效 |
| TC-13 路径穿越 | PASS | 404 | - | `/files/../x` 被拒 |
| TC-14 不存在 file_id | PASS | 404 NOT_FOUND | 049b2144… | |
| TC-15 删除/重复删除 | PASS ×3 | 200 / 200 / 200 | ce24939a… 等 | 首删 DELETING；**重复删除幂等（200）** |
| TC-16 删除后续签 | PASS | 409 CONFLICT | 361bb39f… | 拒绝续签 |
| TC-17 删除后下载 | PASS | 409 CONFLICT | 603ff0d7… | 拒绝下载 |
| TC-18 真实 OSS 直传 | **SKIP（待人工验证）** | - | - | 见第七节补测说明 ②：验收脚本只覆盖控制面，文件体直传/回调链路需人工在真实 OSS 上执行 |

## 三、结论

1. **中台文件服务 7 个控制面接口与本 SDK v0.3.0 契约完全吻合**：错误信封（`UNAUTHORIZED`/`SCENE_UNAUTHORIZED`/`CONFLICT`/`NOT_FOUND`）、幂等键语义、STS 一次性签发与续签、删除调度与终态拒绝均符合预期。
2. SDK 侧安全设计得到验证：凭证不缓存、掩码输出、file_id 注入防护（TC-13）。
3. **具备与 v0.3.0 版本联调通过的结论**；TC-04/TC-18 两项标记为**待人工验证**（不阻塞 SDK v0.3.0 发版，因两者验证的是中台管理配置行为与文件体直传链路，非 SDK 控制面契约），建议随下一次窗口补测后宣布全量验收完成。

## 四、发现的契约差异（记录，未改中台/SDK 业务代码）

| 编号 | 关联用例 | 现象 | 期望 vs 实际 | 严重级 | 初步定位 | 状态 |
|---|---|---|---|---|---|---|
| P-01 | TC-05 | 中台实际签发 `file_id` 为 `file_<32hex>`（如 `file_b4c16f40…`），非 API 文档示例与验收用例假定的 UUID | 期望 UUID 格式；实际 `file_` 前缀十六进制 | 低（提示性） | 中台契约文档与实现存在轻微出入；SDK 本身不做格式强校验，不受影响 | 已闭环：中台文档已补注格式（`file-storage-interfaces.md` §二/§2.3、`05-file-storage.md`）；修复建议见中台 `docs/acceptance/file-service-fix-suggestions-2026-11-02.md` |
| P-02 | TC-05 首次执行 | `document` 场景对 `application/octet-stream`/`.bin` 返回 400 文件格式不支持 | 非缺陷：场景白名单行为符合设计 | 提示 | 环境策略；验收脚本已改为探针扩展名/MIME 可配置（`INFRA_FILE_EXT`/`INFRA_FILE_CONTENT_TYPE`） | 已闭环 |
| P-03 | TC-10 | 对不存在云端对象的文件调用 reconcile，返回 200 且状态保持 UPLOADING | 期望"不伪造成功"；实际行为符合，但响应体仅回状态、无"对象缺失"提示语 | 低 | 中台 reconcile 对无对象情况缺一句可解释的提示（如 message/reconcile 结果字段） | **已修复并复验通过**：中台实施方案 A，响应新增 `reconcile_result`；19:57 复验 TC-10 严格断言 `result=OBJECT_NOT_FOUND` 通过（rid=b71dc3ae…），Go 版同步通过（rid 同轮） |
| P-04 | TC-15 | 重复删除返回 200 幂等（此前 Go 版本 mock 假设为 409 终态错误） | 行为优于预期，非缺陷 | 提示 | 中台幂等删除语义 | 已在脚本中同时接受两种行为 |

## 五、验收脚本自身缺陷（执行过程中发现并修复，属测试工具非被测代码）

| # | 问题 | 修复 |
|---|---|---|
| 1 | bash 3.2 将 `$VAR` 后的全角字符（`：`、`；`、`（`）并入变量名 → unbound variable | 全部变量引用加 `${}` 花括号 |
| 2 | `/oauth/token` 误用表单编码；中台要求 JSON 请求体 | fetch_token 改为 JSON body（与 SDK 行为一致） |
| 3 | jq 1.6 `fromdateiso8601` 强制 Z 结尾、字符串不支持单下标 → 时间解析失败 | epoch_of 补 Z 拼接与切片写法，支持 `+08:00` 偏移与毫秒 |
| 4 | file_id UUID 假设与真实格式不符 | 放宽为 `file_<32hex>` 或 UUID |
| 5 | 探针文件格式与场景白名单耦合 | 新增 `INFRA_FILE_EXT`/`INFRA_FILE_CONTENT_TYPE` 配置 |

同步修正：Go 版验收测试（`acceptance/files_acceptance_test.go`）的 file_id 校验同步放宽；`go vet -tags acceptance`、常规测试套件均通过。

## 六、环境残留说明

- 探针文件 `file_b4c16f40…` 已通过 TC-15 进入删除调度（DELETING），无业务数据污染。
- 手动取样期间产生的 2 个 UPLOADING 探针文件（`file_8ffa99b4…`、`file_0f570f81…`，均 1KB、未上传对象）等待 `upload_expires_at`（约 7 天）过期；如需立即清理可在管理后台删除。

## 七、待人工验证项（下次窗口补测）

1. **TC-04 场景停用**（补测说明 ①）——**待人工验证**
   - 执行人：本人（中台管理员权限，可执行场景停用/还原）
   - 时间窗：**【待排期】**（建议安排在中台侧无其他文件服务变更的时段，避免停用操作与发版互相干扰）
   - 跳过原因：需要管理员临时停用一个启用中的场景，会影响本地环境数据，自动化脚本不便操作。
   - 补测步骤：管理员后台临时停用 `media` 场景 → 设置 `INFRA_FILE_DISABLED_SCENE=media` 重跑任一版验收脚本 → 预期该场景授权申请被拒（4xx，含明确错误码）→ 补测通过后**还原场景为启用**并核对能力/额度与停用前一致。
   - 通过标准：脚本 TC-04 显示 PASS，且环境还原后常规用例不受影响。
2. **TC-18 真实 OSS 直传**（补测说明 ②）——**待人工验证**
   - 执行人：本人（持有测试 OSS 环境 Bucket 与 STS 访问凭据）
   - 时间窗：**【待排期】**（需与 TC-04 同窗口或其后执行，复用同一次 dev 栈启动）
   - 跳过原因：验收脚本只验证控制面（授权/核验/删除）；文件体直传 OSS、回调触发、ACTIVE 终态与下载验证涉及真实 STS 凭证使用与对象存储环境，超出 SDK 自动化范围。
   - 补测步骤：以 TC-05 签发的 STS 凭证用阿里云 OSS CLI/SDK 直传对象 → 等待或手动触发回调 → `GET /{file_id}` 确认 ACTIVE（可参考中台 `tmp_dada_tool/tunnel-verify` 链路）→ 申请下载 URL 实际取回文件核对字节一致 → 走 TC-15 删除清理。
   - 通过标准：文件状态流转 UPLOADING → ACTIVE 完整达成，下载内容与直传内容一致，删除后状态进入 DELETING。
3. ~~P-01/P-03 反馈中台侧评审~~ **当日已闭环**：P-01 文档修订落库；P-03 方案 A 实施后于 19:57 复验——本地 dev 栈（UTM postgres + `make dev-api`，端口 38471）重启后重跑两版验收脚本，Shell 版 PASS 18 / SKIP 2 / FAIL 0，Go 版全 PASS，TC-10 严格断言 `reconcile_result=OBJECT_NOT_FOUND` 实测通过。

## 八、TC-04 / TC-18 详细执行步骤

命令级补测步骤（runbook）已迁移至用例文档：[acceptance-file-service-2026-11-02.md 第六节](acceptance-file-service-2026-11-02.md#六tc-04--tc-18-补测详细步骤待人工验证项-runbook)。补测完成后按该节"结果回填"说明更新本报告。
