# 文件服务 SDK 联调验收用例（2026-11-02）

- 对象：`youwei-infra-sdk` v0.3.0 `client.Files`（7 个控制面接口）↔ 中台 2.0.0 `/api/v1/files`
- 方式：真实中台环境联调，SDK 直连；不修改中台与 SDK 代码，问题仅记录
- 脚本：`acceptance/files_acceptance_test.go`（`go test -tags acceptance ./acceptance/ -v`）
- 执行人/环境/结果：见文末报告模板

## 一、环境与前置条件

| 项 | 要求 |
|---|---|
| 环境变量 | `INFRA_BASE_URL`、`INFRA_CLIENT_ID`、`INFRA_CLIENT_SECRET`、`INFRA_FILE_SCENE`（已对该 Client 开通文件能力且启用的场景编码，如 `archive`） |
| 应用授权 | OAuth Client 已开通 scope：`file.upload`、`file.read`、`file.download`、`file.delete` |
| 中台侧 | 文件存储已配置目标（阿里云 OSS 私有版本化桶）、上传授权角色检测通过、场景启用 |
| 幂等键 | 脚本按运行时间戳生成（`sdk-acc-<yyyymmddhhmmss>`），避免历史数据干扰 |
| 用例级开关 | `INFRA_FILE_VERBOSE=1` 打印每步结果摘要（默认开启） |

## 二、用例清单

标注说明：**[严格]** 结果不符即验收失败；**[记录]** 行为依赖环境策略，记录实际表现供评审，不直接判失败。

### TC-01 Client Token 获取与基础连通 **[严格]**
- 步骤：`Files.List(ListFilesRequest{Page:1, PageSize:1})`
- 预期：无错误；返回 `data` 含 `items/total/page/page_size` 结构，`request_id` 非空。
- 通过标准：SDK 自动完成 `POST /oauth/token` 并缓存凭证。

### TC-02 无凭证访问被拒绝 **[严格]**
- 步骤：用裸 `net/http` GET `{INFRA_BASE_URL}/api/v1/files`，不带 Authorization。
- 预期：HTTP 401，错误码 `UNAUTHORIZED`（或网关等价 401 信封）。

### TC-03 非法/未授权场景码申请授权 **[严格]**
- 步骤：`Files.AuthorizeUpload`，`scene_code="sdk-acc-not-exist-<ts>"`，合法文件元信息（小体积、`application/octet-stream`）。
- 预期：HTTP 4xx；错误码为 `INVALID_INPUT`（场景不存在）或 `FORBIDDEN`/`SCENE_UNAUTHORIZED`（未授权）系；不返回任何凭证字段。
- 记录：实际返回码与错误消息。

### TC-04 场景停用申请授权（如环境可配置）**[记录]**
- 步骤：管理员停用场景后重放 TC-03 正常请求（执行前由人工操作，脚本用 `INFRA_FILE_DISABLED_SCENE` 传入场景码，缺省跳过）。
- 预期：4xx，含可读中文提示；不出凭证。

### TC-05 正常申请上传授权 **[严格]**
- 步骤：`Files.AuthorizeUpload`：`scene_code=INFRA_FILE_SCENE`、`original_filename="sdk-acceptance-<ts>.bin"`、`file_size=1024`、`content_type="application/octet-stream"`、`biz_ref_type="acceptance"`、`biz_ref_id=<ts>`、幂等键 `<run-id>`。
- 预期 200 且 `data` 满足：
  - `file_id` 为 UUID；`provider="aliyun_oss"`
  - `region`、`endpoint`（https）、`bucket`、`object_key` 均非空，`object_key` 含场景/Client 前缀
  - `credentials.access_key_id / access_key_secret / security_token` 非空，`expires_at` 在未来（约 900s）
  - `callback.url`、`callback.body`、`callback.contentType` 非空
  - `max_file_size_bytes > 0`；`part_size_bytes > 0`（默认 8MiB）；`parallel > 0`（默认 3）
  - `upload_expires_at` 在未来
- 安全检查：响应不缓存（SDK 已保证）；凭证值不打印到日志（脚本仅打印长度/掩码）。

### TC-06 幂等重放（同键同元数据）**[严格]**
- 步骤：完全相同的请求体重放 TC-05。
- 预期：成功且返回同一 `file_id`、同一 `object_key`；不新建资源。

### TC-07 幂等键冲突（同键异元数据）**[严格]**
- 步骤：同幂等键，改 `file_size` 或 `original_filename` 重放。
- 预期：4xx（`CONFLICT`/`INVALID_INPUT` 系），不返回新 file_id。

### TC-08 状态查询与元信息一致性 **[严格]**
- 步骤：`Files.Get(file_id)`。
- 预期：`status="UPLOADING"`；`scene_code`、`original_filename`、`content_type`、`declared_size_bytes` 与申请一致；`biz_ref_*` 保留；`created_at` 非零。

### TC-09 列表分页与筛选 **[严格]**
- 步骤：`Files.List{SceneCode:场景, Status:"UPLOADING", Page:1, PageSize:20}`。
- 预期：`items` 包含 TC-05 的 `file_id`；`total ≥ 1`；`page/page_size` 回显。
- 附加 [记录]：`keyword=original_filename` 精确词过滤同样命中。

### TC-10 Reconcile 兜底核验（对象不存在）**[严格]**
- 步骤：未实际上传 OSS，直接 `Files.Reconcile(file_id)`。
- 预期：请求成功或返回可解释错误；**不伪造成功**：状态不得变为 `ACTIVE`（仍 `UPLOADING` 或明确失败原因）。

### TC-11 非 ACTIVE 申请下载 URL **[记录]**
- 步骤：`Files.CreateAccessURL(file_id)`（文件仍 UPLOADING）。
- 预期：按服务端策略应 4xx 拒绝；记录实际状态码/错误码（若环境允许对未完成文件出链接，记为问题）。

### TC-12 凭证续签（同一上传目标）**[严格]**
- 步骤：`Files.RenewUploadCredentials(file_id)`。
- 预期：200；`file_id`、`object_key`、`bucket` 与 TC-05 相同（不新建目标）；`credentials` 为新签发凭证（`expires_at` 在未来）；`callback` 仍可用。

### TC-13 非法 file_id 入参 **[严格]**
- 步骤：`Files.Get("../x")` 等。
- 预期：SDK 客户端侧直接报 `infra: invalid resource identifier`，无网络请求。

### TC-14 不存在的 file_id **[严格]**
- 步骤：`Files.Get(<随机 UUID>)`。
- 预期：HTTP 404，`NOT_FOUND` 系错误码。

### TC-15 删除与重复删除 **[严格]** / **[记录]**
- 步骤：`Files.Delete(file_id)` → `Files.Get(file_id)` → 再次 `Files.Delete(file_id)`。
- 预期：首次删除成功；Get 返回 `DELETING`/`DELETED` 或 404；重复删除幂等（成功或明确 404/终态提示均算通过，记录实际）。

### TC-16 删除后续签被拒绝 **[严格]**
- 步骤：对已删除文件 `Files.RenewUploadCredentials(file_id)`。
- 预期：4xx 拒绝，不出新凭证。

### TC-17 删除后下载被拒绝 **[严格]**
- 步骤：对已删除文件 `Files.CreateAccessURL(file_id)`。
- 预期：4xx 拒绝；已签发 URL 在原有效期内可能仍可用（属预期，记录即可）。

### TC-18 真实 OSS 直传（人工项，脚本不覆盖）**[记录]**
- 步骤：按中台 `docs/api/05-file-storage.md` 第 4 节，用 TC-05 凭证以官方 `ali-oss` 上传小文件 → 触发回调 → `Files.Get` 确认 `ACTIVE` → `Files.CreateAccessURL` 下载验证。
- 原因：SDK 仅封装控制面；OSS 分片/回调链路需官方 SDK 与真实外网，人工执行并回填结果。

## 三、执行方式

### 3.1 Shell 版（scripts/acceptance-files.sh，仅依赖 curl + jq）

```bash
cd /Users/iease/GoProject/youwei-infra-sdk
INFRA_BASE_URL=... INFRA_CLIENT_ID=... INFRA_CLIENT_SECRET=... INFRA_FILE_SCENE=archive \
  ./scripts/acceptance-files.sh
# 可选：INFRA_FILE_DISABLED_SCENE=<停用场景> 补跑 TC-04；INFRA_REPORT=<路径> 指定 TSV 输出
```

- 逐用例输出 `[TC-xx] PASS/FAIL/RECORD/SKIP <摘要> rid=<request_id>`，结束时打印汇总并生成结果 TSV（默认 `./acceptance-files-report-<RUN_ID>.tsv`）。
- 退出码：0 全部严格用例通过；1 存在 FAIL；2 环境或参数错误。
- 安全细节：场景码仅允许 `[A-Za-z0-9_-]`；STS 凭证仅掩码输出；Token 在到期前 60 秒自动续取。
- 兼容 macOS bash 3.2；时间解析（毫秒/时区偏移）由 jq 完成。

### 3.2 Go 版（acceptance/files_acceptance_test.go，直接使用 SDK）

```bash
cd /Users/iease/GoProject/youwei-infra-sdk
INFRA_BASE_URL=... INFRA_CLIENT_ID=... INFRA_CLIENT_SECRET=... INFRA_FILE_SCENE=archive \
  go test -tags acceptance -run TestFileServiceAcceptance -v ./acceptance/
```

- 用例顺序固定（幂等链路有依赖）；脚本在无环境变量时自动 Skip，不影响 CI。
- 输出逐用例 PASS/FAIL 与关键返回摘要；结束时打印运行 ID、耗时与结果汇总，直接回填到下方报告。

两版覆盖同一套用例（TC-01..TC-18）：Shell 版适合运维/无 Go 环境；Go 版同时验证 SDK 本身的端到端行为，建议联调优先。

## 四、测试报告模板

| 用例 | 结果（PASS/FAIL/SKIP） | 实际状态码/错误码 | request_id | 备注 |
|---|---|---|---|---|
| TC-01 | | | | |
| TC-02 | | | | |
| ... | | | | |
| TC-18 | | | | |

- 环境：BaseURL（脱敏）、中台 VERSION、SDK 版本、执行时间、执行人。
- 结论：通过率、阻塞项、是否具备发版/上线条件。

## 五、问题报告模板

| 编号 | 关联用例 | 问题现象 | 期望 vs 实际 | request_id | 严重级 | 初步定位（中台/SDK/环境） | 状态 |
|---|---|---|---|---|---|---|---|
| P-01 | TC-xx | | | | | | 待确认 |

> 依据约定：验收中发现的问题仅记录与报告，不直接修改中台或 SDK 代码；修复需另行评审。

## 六、TC-04 / TC-18 补测详细步骤（待人工验证项 runbook）

> 前置（两项共用）：dev 栈已启动（`cd /Users/iease/GoProject/youwei-infra-center && nohup make dev-api > /tmp/infra-dev-api.log 2>&1 &`，等 `nc -z 127.0.0.1 38471` 成功）；以下 admin 凭据仅限本地种子环境。补测结果回填测试报告模板（第四节），并同步更新 run report 的第二节表格与第七节状态。

### 6.1 TC-04 场景停用（预计 10 分钟）

```bash
# ① admin 登录取 token
ADMIN_TOKEN=$(curl -sS -X POST http://127.0.0.1:38471/api/v1/admin/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.data.access_token')

# ② 查看并保存 media 场景当前配置（还原基线，务必留存原文）
curl -sS -H "Authorization: Bearer $ADMIN_TOKEN" \
  'http://127.0.0.1:38471/api/v1/admin/file-scenes?page=0&page_size=101' \
  | jq '.data.items[] | select(.scene_code=="media")' | tee /tmp/media-scene-baseline.json

# ③ 停用（PUT 为全量字段：从 ② 的输出复制，仅把 enabled 改为 false）
curl -sS -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d "$(jq '.enabled=false' /tmp/media-scene-baseline.json)" \
  http://127.0.0.1:38471/api/v1/admin/file-scenes/media

# ④ 补跑 TC-04（其余用例走 archive 场景不受影响）
cd /Users/iease/GoProject/youwei-infra-sdk
INFRA_BASE_URL=http://127.0.0.1:38471 INFRA_CLIENT_ID=local-client-a \
INFRA_CLIENT_SECRET=ocs_local_seed_secret_do_not_use_in_prod \
INFRA_FILE_SCENE=archive INFRA_FILE_DISABLED_SCENE=media \
  ./scripts/acceptance-files.sh   # 预期 [TC-04] PASS（场景授权被拒，4xx + 明确错误码）

# ⑤ 还原（把 enabled 改回 true，字段同基线）
curl -sS -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d "$(jq '.enabled=true' /tmp/media-scene-baseline.json)" \
  http://127.0.0.1:38471/api/v1/admin/file-scenes/media

# ⑥ 复核：场景已还原 + 常规用例不受影响
curl -sS -H "Authorization: Bearer $ADMIN_TOKEN" \
  'http://127.0.0.1:38471/api/v1/admin/file-scenes?page=0&page_size=101' \
  | jq '.data.items[] | select(.scene_code=="media") | .enabled'   # 预期 true
```

结果回填：将第 ④ 步 TC-04 行（含 request_id）写入测试报告模板，并在 run report 第二节将 TC-04 改为 PASS。

### 6.2 TC-18 真实 OSS 直传（预计 20 分钟）

推荐用中台现成工具 `tunnel-verify`（Go + 官方 OSS SDK，一条命令走完整链路）：

```bash
# ① 确认本机可出公网访问 OSS（oss-cn-beijing.aliyuncs.com:443）
nc -z -w 3 oss-cn-beijing.aliyuncs.com 443 && echo "OSS reachable"

# ② 运行完整链路：admin 授权（document 场景）→ STS 直传（PutObject 带 OSS 回调配置）
#    → 回调触发 → 轮询状态至 ACTIVE → admin 删除清理
cd /Users/iease/GoProject/youwei-infra-center && go run ./tmp_dada_tool/tunnel-verify
# 预期输出依次：
#   callback url: http://127.0.0.1:38471/api/v1/files/callback...
#   UPLOAD + CALLBACK OK
#   status: UPLOADING → ... → status: ACTIVE
#   cleanup: 200
```

可选深验（对应通过标准中"下载内容与直传内容一致"）：把上面流程改为自定义内容并在 ACTIVE 后取下载 URL 比对——

```bash
# ③a 用 client token 走 SDK 侧授权（拿到完整 STS 凭证，勿外泄）
TOKEN=$(curl -sS -X POST http://127.0.0.1:38471/oauth/token -H 'Content-Type: application/json' \
  --data '{"grant_type":"client_credentials","client_id":"local-client-a","client_secret":"ocs_local_seed_secret_do_not_use_in_prod"}' \
  | jq -r '.data.access_token')
TS=$(date +%s)
curl -sS -X POST http://127.0.0.1:38471/api/v1/files/upload-authorizations \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"scene_code\":\"document\",\"original_filename\":\"tc18-$TS.txt\",\"file_size\":7,\"content_type\":\"text/plain\",\"idempotency_key\":\"tc18-$TS\"}" \
  | tee /tmp/tc18-auth.json | jq '{file_id: .data.file_id, object_key: .data.object_key}'
# 按中台 API 文档的回调模拟方式触发回调（或等待 OSS 回调）后确认 ACTIVE，再：
# access-urls 无请求体（TTL 由中台场景/目标配置决定，响应含 url 与 expires_at）
FILE_ID=$(jq -r '.data.file_id' /tmp/tc18-auth.json)
URL=$(curl -sS -X POST http://127.0.0.1:38471/api/v1/files/$FILE_ID/access-urls \
  -H "Authorization: Bearer $TOKEN" | jq -r '.data.url')
curl -sS "$URL"   # 与直传内容逐字节比对（diff 或 md5）；响应带 response-content-disposition=attachment

# ③b 清理
curl -sS -X DELETE http://127.0.0.1:38471/api/v1/files/$FILE_ID -H "Authorization: Bearer $TOKEN"
```

结果回填：记录 file_id、各阶段状态流转（UPLOADING → ACTIVE）、下载比对结果与清理响应，写入测试报告模板 TC-18 行；如回调未自动触发，检查中台回调 URL 是否公网可达（本地环境通常需按中台文档手动模拟回调），并注明实际采用方式。
