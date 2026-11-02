#!/usr/bin/env bash
# 中台文件服务联调验收脚本（Shell/curl 版）
#
# 对应用例文档：docs/acceptance-file-service-2026-11-02.md（TC-01..TC-18）
# 仅依赖 curl + jq；问题只记录不修复。TC-18（真实 OSS 直传）为人工项。
#
# 用法：
#   INFRA_BASE_URL=https://center.example.com \
#   INFRA_CLIENT_ID=... INFRA_CLIENT_SECRET=... INFRA_FILE_SCENE=archive \
#   ./scripts/acceptance-files.sh
#
# 可选：
#   INFRA_FILE_DISABLED_SCENE=...   补跑 TC-04（场景停用）
#   INFRA_TIMEOUT=15                单请求超时秒数
#   INFRA_REPORT=path.tsv           结果 TSV 输出路径（默认 ./acceptance-files-report-<时间戳>.tsv）
#
# 退出码：0 全部严格用例通过；1 存在 FAIL；2 环境或参数错误。
set -uo pipefail

BASE="${INFRA_BASE_URL:-}"
CLIENT_ID="${INFRA_CLIENT_ID:-}"
CLIENT_SECRET="${INFRA_CLIENT_SECRET:-}"
SCENE="${INFRA_FILE_SCENE:-}"
DISABLED_SCENE="${INFRA_FILE_DISABLED_SCENE:-}"
TIMEOUT="${INFRA_TIMEOUT:-15}"
REPORT="${INFRA_REPORT:-}"

if [ -z "$BASE" ] || [ -z "$CLIENT_ID" ] || [ -z "$CLIENT_SECRET" ] || [ -z "$SCENE" ]; then
  echo "缺少环境变量：INFRA_BASE_URL / INFRA_CLIENT_ID / INFRA_CLIENT_SECRET / INFRA_FILE_SCENE" >&2
  echo "用法见脚本头部注释。" >&2
  exit 2
fi
if ! command -v curl >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
  echo "需要 curl 与 jq" >&2
  exit 2
fi
case "$SCENE" in
  *[!A-Za-z0-9_-]*) echo "INFRA_FILE_SCENE 只允许字母/数字/下划线/连字符：$SCENE" >&2; exit 2 ;;
esac
case "$CLIENT_ID$CLIENT_SECRET" in
  *[\"\\]*) echo "INFRA_CLIENT_ID / INFRA_CLIENT_SECRET 不允许包含引号或反斜杠" >&2; exit 2 ;;
esac
case "$BASE" in
  http://*|https://*) : ;;
  *) echo "INFRA_BASE_URL 必须以 http(s):// 开头" >&2; exit 2 ;;
esac
BASE="${BASE%/}"

RUN_ID="sdk-acc-$(date +%Y%m%d%H%M%S)"
# 探针文件扩展名与 MIME 需匹配目标场景的 allowed_formats/allowed_content_types 白名单
FILE_EXT="${INFRA_FILE_EXT:-zip}"
FILE_CT="${INFRA_FILE_CONTENT_TYPE:-application/zip}"
TSV="${REPORT:-./acceptance-files-report-${RUN_ID}.tsv}"
PASS=0; FAIL=0; RECORD=0; SKIP=0

TOKEN=""; TOKEN_TYPE=""; TOKEN_EXPIRES_AT=0

note() { printf '%s\n' "$*"; }
mask() {
  local s="$1"
  if [ -z "$s" ]; then printf '***'; return; fi
  if [ "${#s}" -le 4 ]; then printf '***'; return; fi
  printf '%s***%s' "${s:0:2}" "${s: -2}"
}

# result <TC> <PASS|FAIL|RECORD|SKIP> <detail> <request_id>
result() {
  local tc="$1" verdict="$2" detail="$3" rid="$4"
  case "$verdict" in
    PASS)   PASS=$((PASS+1)) ;;
    FAIL)   FAIL=$((FAIL+1)) ;;
    RECORD) RECORD=$((RECORD+1)) ;;
    SKIP)   SKIP=$((SKIP+1)) ;;
  esac
  note "[$tc] $verdict $detail rid=${rid:-'-'}"
  printf '%s\t%s\t%s\t%s\n' "$tc" "$verdict" "$detail" "${rid:-'-'}" >> "$TSV"
}

# http <METHOD> <PATH> [JSON_BODY] [--no-auth] [--path-as-is]
# 结果存入：HTTP_STATUS / HTTP_BODY / HTTP_REQUEST_ID
HTTP_STATUS=""; HTTP_BODY=""; HTTP_REQUEST_ID=""
http() {
  local method="$1" path="$2" body="${3:-}" auth=1 asis=0
  shift $(( $# > 2 ? 3 : $# ))
  local arg
  for arg in "$@"; do
    [ "$arg" = "--no-auth" ] && auth=0
    [ "$arg" = "--path-as-is" ] && asis=1
  done
  local args=(-sS -m "$TIMEOUT" -w $'\n%{http_code}' -X "$method" "$BASE$path"
              -H 'Accept: application/json')
  [ "$auth" = 1 ] && [ -n "$TOKEN" ] && args+=(-H "Authorization: Bearer $TOKEN")
  [ "$body" != "" ] && args+=(-H 'Content-Type: application/json' --data "$body")
  [ "$asis" = 1 ] && args+=(--path-as-is)
  local raw status
  if ! raw="$(curl "${args[@]}" 2>&1)"; then
    HTTP_STATUS="000"; HTTP_BODY="$raw"; HTTP_REQUEST_ID=""
    return
  fi
  status="${raw##*$'\n'}"
  HTTP_BODY="${raw%$'\n'*}"
  HTTP_STATUS="$status"
  HTTP_REQUEST_ID="$(printf '%s' "$HTTP_BODY" | jq -r '.request_id // empty' 2>/dev/null)"
}

# err_code：从错误信封取 code（兼容嵌套与 RFC 6749 扁平格式）
err_code() { printf '%s' "$HTTP_BODY" | jq -r '.error.code // .error // empty' 2>/dev/null; }
err_msg()  { printf '%s' "$HTTP_BODY" | jq -r '.error.message // .error_description // empty' 2>/dev/null; }
data_field() { printf '%s' "$HTTP_BODY" | jq -r "$1 // empty" 2>/dev/null; }

# expect_error <TC> <允许的状态码列表，如 "400 403"> <允许的错误码正则> <说明>
expect_error() {
  local tc="$1" statuses="$2" code_re="$3" note_txt="$4"
  if [ "$HTTP_STATUS" = "000" ]; then
    result "$tc" FAIL "网络错误：$HTTP_BODY" ""; return
  fi
  local code; code="$(err_code)"
  local ok=0
  for s in $statuses; do
    [ "$HTTP_STATUS" = "$s" ] && ok=1
  done
  if [ "$ok" = 1 ] && printf '%s' "$code" | grep -Eq "^($code_re)$"; then
    result "$tc" PASS "HTTP $HTTP_STATUS $code" "$HTTP_REQUEST_ID"
  elif [ "$ok" = 1 ]; then
    result "$tc" RECORD "HTTP $HTTP_STATUS ${code}（状态符合，错误码在策略外，记录）msg=$(err_msg)" "$HTTP_REQUEST_ID"
  else
    result "$tc" FAIL "HTTP $HTTP_STATUS $code 期望 $statuses/$code_re msg=$(err_msg)" "$HTTP_REQUEST_ID"
  fi
}

# expect_data_nonempty <TC> <jq 表达式列表（data 下的字段）> <说明>
expect_data_nonempty() {
  local tc="$1" fields="$2" bad="" f v
  if [ "$HTTP_STATUS" != "200" ]; then
    result "$tc" FAIL "HTTP $HTTP_STATUS 期望 200 msg=$(err_msg)" "$HTTP_REQUEST_ID"; return 1
  fi
  for f in $fields; do
    v="$(data_field ".$f")"
    [ -z "$v" ] || [ "$v" = "null" ] && bad="$bad $f"
  done
  if [ -n "$bad" ]; then
    result "$tc" FAIL "data 缺少字段:$bad" "$HTTP_REQUEST_ID"; return 1
  fi
  return 0
}

fetch_token() {
  local now raw resp status tok ttype ttl
  now="$(date +%s)"
  if [ -n "$TOKEN" ] && [ "$now" -lt $((TOKEN_EXPIRES_AT - 60)) ]; then
    return 0
  fi
  if ! raw="$(curl -sS -m "$TIMEOUT" -w $'\n%{http_code}' -X POST "$BASE/oauth/token" \
      -H 'Content-Type: application/json' \
      --data "{\"grant_type\":\"client_credentials\",\"client_id\":\"$CLIENT_ID\",\"client_secret\":\"$CLIENT_SECRET\"}" 2>&1)"; then
    note "[TOKEN] FAIL 无法请求 /oauth/token：$raw"
    return 1
  fi
  status="${raw##*$'\n'}"; resp="${raw%$'\n'*}"
  if [ "$status" != "200" ]; then
    note "[TOKEN] FAIL HTTP ${status}：$resp"
    return 1
  fi
  tok="$(printf '%s' "$resp" | jq -r '.data.access_token // empty')"
  ttype="$(printf '%s' "$resp" | jq -r '.data.token_type // empty')"
  ttl="$(printf '%s' "$resp" | jq -r '.data.expires_in // 0')"
  if [ -z "$tok" ] || [ "$ttl" -le 0 ] 2>/dev/null; then
    note "[TOKEN] FAIL 无效 token 响应"
    return 1
  fi
  TOKEN="$tok"; TOKEN_TYPE="$ttype"; TOKEN_EXPIRES_AT=$((now + ttl))
  return 0
}

uuid_ok() { printf '%s' "$1" | grep -Eq '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'; }

# id_ok：中台 file_id 实际格式为 file_<32hex>；兼容 UUID 便于未来演进
id_ok() {
  printf '%s' "$1" | grep -Eq '^(file_[0-9a-fA-F]{32}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$'
}

# epoch_of：RFC3339/ISO8601 → Unix 秒。兼容带毫秒与 ±HH:MM 时区偏移；无法解析返回 0。
# jq 1.6 注意：fromdateiso8601 强制 Z 结尾；字符串不支持单下标，只能切片。
epoch_of() {
  printf '%s' "$1" | jq -Rr '
    if . == "" then "0"
    else
      (sub("\\.[0-9]+";"")) as $t |
      if ($t | test("[+-][0-9]{2}:[0-9]{2}$")) then
        (($t[0:19] + "Z") | fromdateiso8601) +
        (if $t[19:20] == "+" then -1 else 1 end) *
        (($t[20:22] | tonumber) * 3600 + ($t[23:25] | tonumber) * 60)
      else (if ($t | test("Z$")) then $t else ($t + "Z") end | fromdateiso8601) end
    end' 2>/dev/null
}

in_future() {
  local e
  e="$(epoch_of "$1")"
  [ -n "$e" ] && [ "$e" -gt "$(date +%s)" ]
}

printf '%s\n' "# 文件服务验收 RUN_ID=$RUN_ID BASE=$BASE SCENE=$SCENE $(date '+%F %T')" > "$TSV"

# ---------- TC-01 Client Token 与连通 ----------
if fetch_token; then
  http GET "/api/v1/files?page=1&page_size=1"
  if [ "$HTTP_STATUS" = "200" ] && [ -n "$HTTP_REQUEST_ID" ]; then
    result TC-01 PASS "token 获取并完成列表调用" "$HTTP_REQUEST_ID"
  else
    result TC-01 FAIL "HTTP $HTTP_STATUS body=$(printf '%s' "$HTTP_BODY" | head -c 200)" "$HTTP_REQUEST_ID"
  fi
else
  result TC-01 FAIL "无法获取 Client Token，中止" ""
  note "汇总：PASS=$PASS FAIL=$FAIL RECORD=$RECORD SKIP=$SKIP；报告：${TSV}"
  exit 1
fi

# ---------- TC-02 无凭证访问 ----------
http GET "/api/v1/files" "" --no-auth
if [ "$HTTP_STATUS" = "401" ]; then
  result TC-02 PASS "HTTP 401" "$HTTP_REQUEST_ID"
else
  result TC-02 FAIL "HTTP $HTTP_STATUS 期望 401" "$HTTP_REQUEST_ID"
fi

# ---------- TC-03 未知场景 ----------
http POST "/api/v1/files/upload-authorizations" \
  "{\"scene_code\":\"sdk-acc-not-exist-${RUN_ID}\",\"original_filename\":\"probe.bin\",\"file_size\":1,\"content_type\":\"application/octet-stream\"}"
expect_error TC-03 "400 403 404" "INVALID_INPUT|FORBIDDEN|SCENE_UNAUTHORIZED|NOT_FOUND" "未知场景不得出凭证"

# ---------- TC-04 场景停用（可选） ----------
if [ -n "$DISABLED_SCENE" ]; then
  http POST "/api/v1/files/upload-authorizations" \
    "{\"scene_code\":\"$DISABLED_SCENE\",\"original_filename\":\"probe.bin\",\"file_size\":1,\"content_type\":\"application/octet-stream\"}"
  expect_error TC-04 "400 403 409" "INVALID_INPUT|FORBIDDEN|SCENE_DISABLED|CONFLICT" "停用场景不得出凭证"
else
  result TC-04 SKIP "未提供 INFRA_FILE_DISABLED_SCENE" ""
fi

# ---------- TC-05 正常申请授权 ----------
AUTH_BODY="{\"scene_code\":\"$SCENE\",\"original_filename\":\"sdk-acceptance-${RUN_ID}.${FILE_EXT}\",\"file_size\":1024,\"content_type\":\"$FILE_CT\",\"biz_ref_type\":\"acceptance\",\"biz_ref_id\":\"$RUN_ID\",\"idempotency_key\":\"$RUN_ID\"}"
http POST "/api/v1/files/upload-authorizations" "$AUTH_BODY"
FILE_ID="$(data_field '.data.file_id')"
OBJECT_KEY="$(data_field '.data.object_key')"
BUCKET="$(data_field '.data.bucket')"
PROVIDER="$(data_field '.data.provider')"
CRED_ID="$(data_field '.data.credentials.access_key_id')"
CRED_SECRET="$(data_field '.data.credentials.access_key_secret')"
CRED_TOKEN="$(data_field '.data.credentials.security_token')"
CRED_EXPIRES="$(data_field '.data.credentials.expires_at')"
CB_URL="$(data_field '.data.callback.url')"
CB_BODY="$(data_field '.data.callback.body')"
CB_CT="$(data_field '.data.callback.contentType')"
MAX_SIZE="$(data_field '.data.max_file_size_bytes')"
PART_SIZE="$(data_field '.data.part_size_bytes')"
PARALLEL="$(data_field '.data.parallel')"
UPLOAD_EXPIRES="$(data_field '.data.upload_expires_at')"
if expect_data_nonempty TC-05 "data.file_id data.object_key data.region data.endpoint data.bucket data.credentials.access_key_id data.credentials.access_key_secret data.credentials.security_token data.credentials.expires_at data.callback.url data.callback.body data.callback.contentType data.max_file_size_bytes data.part_size_bytes data.parallel data.upload_expires_at"; then
  PROBLEMS=""
  id_ok "$FILE_ID" || PROBLEMS="$PROBLEMS file_id 格式非法（应为 file_<32hex> 或 UUID）"
  [ "$PROVIDER" = "aliyun_oss" ] || PROBLEMS="$PROBLEMS provider=$PROVIDER"
  case "$PART_SIZE" in ''|*[!0-9]*) PROBLEMS="$PROBLEMS part_size 非正整数";; 0) PROBLEMS="$PROBLEMS part_size=0";; esac
  case "$PARALLEL" in ''|*[!0-9]*) PROBLEMS="$PROBLEMS parallel 非正整数";; 0) PROBLEMS="$PROBLEMS parallel=0";; esac
  case "$MAX_SIZE" in ''|*[!0-9]*) PROBLEMS="$PROBLEMS max_file_size 非正整数";; 0) PROBLEMS="$PROBLEMS max_file_size=0";; esac
  in_future "$CRED_EXPIRES" || PROBLEMS="$PROBLEMS 凭证已过期"
  in_future "$UPLOAD_EXPIRES" || PROBLEMS="$PROBLEMS upload_expires_at 已过期"
  if [ -n "$PROBLEMS" ]; then
    result TC-05 FAIL "$PROBLEMS" "$HTTP_REQUEST_ID"
  else
    result TC-05 PASS "file_id=$FILE_ID cred=$(mask "$CRED_ID")/$(mask "$CRED_SECRET")/$(mask "$CRED_TOKEN")" "$HTTP_REQUEST_ID"
  fi
fi

if [ -n "$FILE_ID" ] && id_ok "$FILE_ID"; then
  # ---------- TC-06 幂等重放 ----------
  http POST "/api/v1/files/upload-authorizations" "$AUTH_BODY"
  REPLAY_ID="$(data_field '.data.file_id')"; REPLAY_KEY="$(data_field '.data.object_key')"
  if [ "$HTTP_STATUS" = "200" ] && [ "$REPLAY_ID" = "$FILE_ID" ] && [ "$REPLAY_KEY" = "$OBJECT_KEY" ]; then
    result TC-06 PASS "同 file_id=$REPLAY_ID" "$HTTP_REQUEST_ID"
  else
    result TC-06 FAIL "HTTP $HTTP_STATUS replay_id=$REPLAY_ID 原id=$FILE_ID" "$HTTP_REQUEST_ID"
  fi

  # ---------- TC-07 幂等键冲突 ----------
  CONFLICT_BODY="$(printf '%s' "$AUTH_BODY" | jq -c '.file_size = 2048')"
  http POST "/api/v1/files/upload-authorizations" "$CONFLICT_BODY"
  expect_error TC-07 "400 409" "INVALID_INPUT|CONFLICT" "同键异元数据不得新建"

  # ---------- TC-08 元信息一致性 ----------
  http GET "/api/v1/files/$FILE_ID"
  ST="$(data_field '.data.status')"; SC="$(data_field '.data.scene_code')"
  FN="$(data_field '.data.original_filename')"; CT="$(data_field '.data.content_type')"
  DS="$(data_field '.data.declared_size_bytes')"
  if [ "$HTTP_STATUS" = "200" ] && [ "$ST" = "UPLOADING" ] && [ "$SC" = "$SCENE" ] \
     && [ "$FN" = "sdk-acceptance-${RUN_ID}.${FILE_EXT}" ] && [ "$CT" = "$FILE_CT" ] && [ "$DS" = "1024" ]; then
    result TC-08 PASS "status=$ST scene=$SC size=$DS" "$HTTP_REQUEST_ID"
  else
    result TC-08 FAIL "HTTP $HTTP_STATUS status=$ST scene=$SC file=$FN size=$DS" "$HTTP_REQUEST_ID"
  fi

  # ---------- TC-09 列表筛选命中 ----------
  http GET "/api/v1/files?scene_code=$SCENE&status=UPLOADING&page=1&page_size=20"
  HIT="$(printf '%s' "$HTTP_BODY" | jq -r --arg id "$FILE_ID" '[.data.items[]? | select(.file_id == $id)] | length')"
  if [ "$HTTP_STATUS" = "200" ] && [ "${HIT:-0}" -ge 1 ]; then
    result TC-09 PASS "列表命中 file_id" "$HTTP_REQUEST_ID"
  else
    result TC-09 FAIL "HTTP $HTTP_STATUS 命中数=${HIT:-0}" "$HTTP_REQUEST_ID"
  fi

  # ---------- TC-10 Reconcile 无云端对象 ----------
  http POST "/api/v1/files/$FILE_ID/reconcile" '{}'
  if [ "$HTTP_STATUS" = "200" ]; then
    RST="$(data_field '.data.status')"
    RRST="$(data_field '.data.reconcile_result')"
    if [ "$RST" = "ACTIVE" ]; then
      result TC-10 FAIL "无对象却返回 ACTIVE（伪造成功）" "$HTTP_REQUEST_ID"
    elif [ "$RRST" = "OBJECT_NOT_FOUND" ]; then
      result TC-10 PASS "status=${RST} result=${RRST}（未伪造 ACTIVE）" "$HTTP_REQUEST_ID"
    else
      result TC-10 RECORD "status=${RST} reconcile_result 缺失或非 OBJECT_NOT_FOUND：'${RRST}'" "$HTTP_REQUEST_ID"
    fi
  else
    result TC-10 RECORD "HTTP ${HTTP_STATUS}（$(err_code)）— 可接受为明确失败说明 msg=$(err_msg)" "$HTTP_REQUEST_ID"
  fi

  # ---------- TC-11 非 ACTIVE 下载（记录型） ----------
  http POST "/api/v1/files/$FILE_ID/access-urls" '{}'
  if [ "$HTTP_STATUS" = "200" ]; then
    result TC-11 RECORD "非 ACTIVE 文件拿到了下载 URL（按策略应拒绝，记为问题）" "$HTTP_REQUEST_ID"
  else
    expect_error TC-11 "400 403 404 409" "INVALID_INPUT|FORBIDDEN|NOT_FOUND|CONFLICT|FILE_NOT_ACTIVE" "ACTIVE 前应拒绝"
  fi

  # ---------- TC-12 凭证续签同目标 ----------
  http POST "/api/v1/files/$FILE_ID/upload-credentials" '{}'
  RN_ID="$(data_field '.data.file_id')"; RN_KEY="$(data_field '.data.object_key')"; RN_BUCKET="$(data_field '.data.bucket')"
  RN_EXP="$(data_field '.data.credentials.expires_at')"
  RN_FUTURE=0
  [ -n "$RN_EXP" ] && in_future "$RN_EXP" && RN_FUTURE=1
  if [ "$HTTP_STATUS" = "200" ] && [ "$RN_ID" = "$FILE_ID" ] && [ "$RN_KEY" = "$OBJECT_KEY" ] && [ "$RN_BUCKET" = "$BUCKET" ] && [ "$RN_FUTURE" = 1 ]; then
    result TC-12 PASS "同目标续签，新凭证到期=$RN_EXP" "$HTTP_REQUEST_ID"
  else
    result TC-12 FAIL "HTTP $HTTP_STATUS target_changed=$([ "$RN_KEY" = "$OBJECT_KEY" ] && echo no || echo yes) cred_future=$RN_FUTURE" "$HTTP_REQUEST_ID"
  fi
else
  result TC-06 SKIP "TC-05 未产出 file_id" ""
  result TC-07 SKIP "-" ""; result TC-08 SKIP "-" ""; result TC-09 SKIP "-" ""
  result TC-10 SKIP "-" ""; result TC-11 SKIP "-" ""; result TC-12 SKIP "-" ""
fi

# ---------- TC-13 路径穿越（服务端防御） ----------
http GET "/api/v1/files/../x" "" --path-as-is
case "$HTTP_STATUS" in
  400|403|404) result TC-13 PASS "HTTP $HTTP_STATUS 拒绝路径穿越" "$HTTP_REQUEST_ID" ;;
  *) result TC-13 FAIL "HTTP $HTTP_STATUS 期望 4xx 拒绝" "$HTTP_REQUEST_ID" ;;
esac

# ---------- TC-14 不存在的 file_id ----------
http GET "/api/v1/files/00000000-0000-4000-8000-000000000000"
expect_error TC-14 "403 404" "NOT_FOUND|FORBIDDEN" "未知 id 应 404（跨 Client 403 可接受）"

# ---------- TC-15 删除/重复删除 ----------
if [ -n "$FILE_ID" ] && id_ok "$FILE_ID"; then
  http DELETE "/api/v1/files/$FILE_ID"
  if [ "$HTTP_STATUS" = "200" ]; then
    DST="$(data_field '.data.status')"
    case "$DST" in
      DELETING|DELETED) result TC-15 PASS "首次删除 status=$DST" "$HTTP_REQUEST_ID" ;;
      *) result TC-15 RECORD "首次删除返回 status=$DST" "$HTTP_REQUEST_ID" ;;
    esac
  else
    result TC-15 FAIL "首次删除 HTTP $HTTP_STATUS msg=$(err_msg)" "$HTTP_REQUEST_ID"
  fi
  http GET "/api/v1/files/$FILE_ID"
  if [ "$HTTP_STATUS" = "200" ]; then
    GST="$(data_field '.data.status')"
    case "$GST" in
      DELETING|DELETED) result TC-15 PASS "删除后 status=$GST" "$HTTP_REQUEST_ID" ;;
      *) result TC-15 RECORD "删除后 status=$GST" "$HTTP_REQUEST_ID" ;;
    esac
  else
    result TC-15 RECORD "删除后查询 HTTP ${HTTP_STATUS}（$(err_code)）" "$HTTP_REQUEST_ID"
  fi
  http DELETE "/api/v1/files/$FILE_ID"
  case "$HTTP_STATUS" in
    200) result TC-15 PASS "重复删除幂等" "$HTTP_REQUEST_ID" ;;
    403|404|409) result TC-15 RECORD "重复删除 HTTP ${HTTP_STATUS}（$(err_code)）— 终态明确错误可接受" "$HTTP_REQUEST_ID" ;;
    *) result TC-15 FAIL "重复删除 HTTP $HTTP_STATUS" "$HTTP_REQUEST_ID" ;;
  esac
else
  result TC-15 SKIP "无可用 file_id" ""
fi

# ---------- TC-16 删除后续签 ----------
if [ -n "$FILE_ID" ] && id_ok "$FILE_ID"; then
  http POST "/api/v1/files/$FILE_ID/upload-credentials" '{}'
  expect_error TC-16 "400 403 404 409" "INVALID_INPUT|FORBIDDEN|NOT_FOUND|CONFLICT" "删除后必须拒绝续签"
else
  result TC-16 SKIP "无可用 file_id" ""
fi

# ---------- TC-17 删除后下载 ----------
if [ -n "$FILE_ID" ] && id_ok "$FILE_ID"; then
  http POST "/api/v1/files/$FILE_ID/access-urls" '{}'
  expect_error TC-17 "400 403 404 409" "INVALID_INPUT|FORBIDDEN|NOT_FOUND|CONFLICT" "删除后必须拒绝下载"
else
  result TC-17 SKIP "无可用 file_id" ""
fi

# ---------- TC-18 人工项 ----------
result TC-18 SKIP "真实 OSS 直传/回调/ACTIVE/下载为人工验证项" ""

note "------------------------------------------------------------"
note "汇总：PASS=$PASS FAIL=$FAIL RECORD=$RECORD SKIP=$SKIP"
note "RUN_ID=$RUN_ID SCENE=$SCENE FILE_ID=${FILE_ID:-'-'}"
note "结果 TSV：${TSV}（回填 docs/acceptance-file-service-2026-11-02.md 第四节）"
note "RECORD 项为环境策略依赖行为，请人工评审；FAIL 项按问题报告模板登记。"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
