# 鉴权限流服务 API 使用说明

本文面向开发者、网关接入方和自动化脚本维护者，说明如何通过 API 使用本系统完成统一鉴权、限流、服务注册与发现、OIDC/OAuth2 接入、M2M 签名鉴权，以及 APISIX 对接。

生产环境基础地址：

```text
https://auth-limit.baichengedu.com
```

文档中的账号、Token、Secret、服务 ID 均为占位符。请勿把生产可用凭据写入脚本仓库、APISIX 配置仓库或文档。

## 1. 基础约定

### 1.1 响应格式

普通业务接口统一返回：

```json
{
  "code": 0,
  "message": "ok",
  "data": {},
  "requestId": "req_xxx"
}
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `code` | `0` 表示成功，`1` 表示失败 |
| `message` | 成功时通常为 `ok`，失败时为错误说明 |
| `data` | 业务数据，失败时可能不存在 |
| `requestId` | 请求 ID，排查日志时使用 |

OIDC/OAuth2 标准端点返回 OAuth 兼容格式，例如：

```json
{
  "error": "invalid_client"
}
```

限流校验成功时直接返回限流结果，不包一层 `code/message/data`：

```json
{
  "allowed": true,
  "remaining": 99,
  "resetAt": 1779173008
}
```

### 1.2 认证头

除公开端点外，管理接口需要携带用户 Access Token：

```http
Authorization: Bearer <ACCESS_TOKEN>
```

当 Access Token 接近过期，服务会在部分鉴权相关响应中返回新的 Access Token：

```http
X-Access-Token: <NEW_ACCESS_TOKEN>
```

客户端收到该响应头后应替换本地保存的 Access Token。

### 1.3 常见状态码

| 状态码 | 含义 |
| --- | --- |
| `200` | 请求成功 |
| `400` | 参数格式错误或必填字段缺失 |
| `401` | 缺少 Token、Token 无效、M2M 签名失败或 OIDC 客户端无效 |
| `403` | 权限不足、黑名单命中或访问不属于自己的资源 |
| `404` | 资源不存在 |
| `409` | 唯一字段冲突，例如用户名、角色编码、启用限流规则重复 |
| `423` | 登录失败次数过多，账号临时锁定 |
| `429` | 限流校验未通过 |
| `503` | PostgreSQL、Redis 等依赖暂不可用 |

### 1.4 权限点

系统启动时会初始化以下权限点。拥有 `admin` 角色的超级管理员自动具备全部权限。

| 权限点 | 用途 |
| --- | --- |
| `user:manage` | 用户管理 |
| `app:manage` | APP 管理和 M2M 凭据管理 |
| `role:manage` | 角色、权限和角色分配 |
| `oidc:manage` | OIDC Client 管理 |
| `service:manage` | 服务注册、审核、发现配置 |
| `limit:manage` | 限流规则管理 |
| `blacklist:manage` | 黑名单、白名单管理 |
| `log:read` | 日志查询 |
| `health:read` | 健康检测日志查询 |
| `statistics:read` | 限流统计查询 |

### 1.5 Redis 降级与敏感凭据存储

Redis 短时不可用时，用户登录会跳过密码缓存直接读取 PostgreSQL；Access Token 校验、Refresh Token 刷新和登出会以 PostgreSQL 中的 `auth_tokens` 元数据为兜底。限流仍会切换到本地令牌桶降级。

APP Secret 只在创建和重置时返回明文，数据库中保存 AES-GCM 密文；服务端在 M2M 验签时解密后参与 HMAC 计算。历史明文 APP Secret 可继续兼容，重置后会改为加密密文。OIDC Client Secret 使用 bcrypt 哈希存储。

## 2. 鉴权流程

### 2.1 登录

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "<USERNAME>",
    "password": "<PASSWORD>"
  }'
```

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "tokenType": "Bearer",
    "accessToken": "<ACCESS_TOKEN>",
    "accessTokenExpiresAt": "2026-05-19T08:30:00Z",
    "refreshToken": "<REFRESH_TOKEN>",
    "refreshTokenExpiresAt": "2026-05-20T08:00:00Z"
  }
}
```

用户名规则为 3-20 位字母、数字或下划线。密码规则为 8-20 位，并且包含大写字母、小写字母、数字和特殊字符。

### 2.2 校验 Access Token

```bash
curl -i -sS "https://auth-limit.baichengedu.com/api/auth/verify" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "userId": "<USER_ID>",
    "roles": ["admin"],
    "permissions": ["user:manage", "app:manage"],
    "expiresAt": "2026-05-19T08:30:00Z"
  }
}
```

如果响应头里出现 `X-Access-Token`，客户端应保存新的 Access Token，并在后续请求里使用它。

### 2.3 刷新 Token

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/auth/refresh" \
  -H "Content-Type: application/json" \
  -d '{
    "refreshToken": "<REFRESH_TOKEN>"
  }'
```

系统默认启用 Refresh Token 轮换。刷新成功后，旧 Refresh Token 会失效，请保存响应里的新 `refreshToken`。

### 2.4 登出并吊销 Token

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/auth/logout" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "refreshToken": "<REFRESH_TOKEN>"
  }'
```

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "loggedOut": true
  }
}
```

### 2.5 管理员解锁账号

连续登录失败达到阈值后，账号会临时锁定。拥有 `user:manage` 权限或 `admin` 角色的管理员可以手动清理锁定状态和失败计数：

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/users/<USER_ID>/unlock" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>"
```

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "unlocked": true
  }
}
```

## 3. 用户、角色与 RBAC

### 3.1 注册用户

用户注册接口是公开接口。注册后的用户默认启用，但没有管理权限。

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/users/register" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "demo_user",
    "password": "<PASSWORD>",
    "displayName": "演示用户"
  }'
```

### 3.2 查询用户

```bash
curl -sS "https://auth-limit.baichengedu.com/api/users" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

普通用户只能看到自己；拥有 `user:manage` 或 `admin` 角色的用户可以管理其他用户。

### 3.3 创建角色

先查询权限列表：

```bash
curl -sS "https://auth-limit.baichengedu.com/api/permissions" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>"
```

创建角色：

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/roles" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "code": "service_operator",
    "name": "服务接入负责人",
    "description": "可管理 APP、服务、限流和统计",
    "permissionIds": ["<APP_MANAGE_PERMISSION_ID>", "<SERVICE_MANAGE_PERMISSION_ID>", "<LIMIT_MANAGE_PERMISSION_ID>", "<STATISTICS_READ_PERMISSION_ID>"]
  }'
```

角色编码需要全局唯一。系统内置 `admin` 角色不可删除。

### 3.4 给用户分配角色

```bash
curl -sS -X PUT "https://auth-limit.baichengedu.com/api/users/<USER_ID>/roles" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "roleIds": ["<ROLE_ID>"]
  }'
```

角色变更后，用户需要重新登录或刷新 Token，新的 Access Token 才会携带最新权限。

### 3.5 给 APP 分配角色

```bash
curl -sS -X PUT "https://auth-limit.baichengedu.com/api/apps/<APP_RECORD_ID>/roles" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "roleIds": ["<ROLE_ID>"]
  }'
```

APP 角色用于机器身份权限治理。当前 M2M 鉴权会校验 APP 身份和签名，业务侧如需使用 APP 权限，应结合返回的 `appId` 与角色配置自行扩展授权策略。

## 4. APP 与 M2M 签名鉴权

### 4.1 创建 APP

需要 `app:manage` 权限。

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/apps" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "order-service"
  }'
```

成功响应会返回一次性 `appSecret`：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": "<APP_RECORD_ID>",
    "appId": "<APP_ID>",
    "name": "order-service",
    "status": "enabled",
    "appSecret": "<APP_SECRET>"
  }
}
```

`appSecret` 只在创建和重置时返回一次，请立即写入安全的密钥管理系统。服务端数据库保存的是加密密文，不会通过列表、详情、日志或管理后台再次返回 Secret 明文。

### 4.2 重置 APP Secret

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/apps/<APP_RECORD_ID>/reset-secret" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

重置后旧 Secret 立即失效，调用方必须更新签名密钥。

### 4.3 M2M 签名算法

请求头：

| Header | 说明 |
| --- | --- |
| `appId` | 创建 APP 时返回的 `appId` |
| `timestamp` | Unix 秒级时间戳，默认允许 30 秒偏差 |
| `sign` | HMAC-SHA256 签名，十六进制小写字符串 |

签名步骤：

1. 收集 URL query 和 JSON body 中的一层参数。
2. 只参与字符串、数字、布尔值；数组和对象不会参与签名。
3. 按参数名升序排列。
4. 拼接 canonical string：

```text
<APP_SECRET>&<TIMESTAMP>&key1=value1&key2=value2
```

5. 使用 `APP_SECRET` 作为 HMAC key，对 canonical string 做 HMAC-SHA256。
6. 将摘要转为 hex 字符串，放入 `sign` 请求头。

伪代码：

```text
canonical = appSecret + "&" + timestamp + "&" + sorted("key=value")
sign = hex(hmac_sha256(key=appSecret, message=canonical))
```

Node.js 示例：

```js
const crypto = require("crypto");

function sign(appSecret, timestamp, params) {
  const pairs = Object.keys(params)
    .sort()
    .map((key) => `${key}=${params[key]}`);
  const canonical = [appSecret, timestamp, ...pairs].join("&");
  return crypto.createHmac("sha256", appSecret).update(canonical).digest("hex");
}

const timestamp = Math.floor(Date.now() / 1000).toString();
const params = { scenario: "gateway-check", nonce: "request-001" };
console.log(sign("<APP_SECRET>", timestamp, params));
```

### 4.4 调用 M2M 鉴权

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/auth/m2m?path=/api/orders" \
  -H "Content-Type: application/json" \
  -H "appId: <APP_ID>" \
  -H "timestamp: <UNIX_SECONDS>" \
  -H "sign: <SIGN>" \
  -d '{
    "method": "GET",
    "nonce": "request-001"
  }'
```

成功响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "allowed": true,
    "appId": "<APP_ID>",
    "appName": "order-service"
  }
}
```

完全相同的 `appId + timestamp + sign` 在短时间内重复提交会触发重放保护，返回 401。

## 5. OIDC/OAuth2

### 5.1 创建 OIDC Client

需要 `oidc:manage` 权限。

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/oidc-clients" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "apisix-gateway",
    "redirectUri": "https://gateway.example.com/callback"
  }'
```

成功响应返回一次性 `clientSecret`：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": "<OIDC_CLIENT_RECORD_ID>",
    "clientId": "<CLIENT_ID>",
    "name": "apisix-gateway",
    "redirectUri": "https://gateway.example.com/callback",
    "status": "enabled",
    "clientSecret": "<CLIENT_SECRET>"
  }
}
```

### 5.2 OIDC Discovery 和 JWKS

```bash
curl -sS "https://auth-limit.baichengedu.com/.well-known/openid-configuration"
curl -sS "https://auth-limit.baichengedu.com/oauth2/jwks"
```

Discovery 会返回：

```json
{
  "issuer": "https://auth-limit.baichengedu.com",
  "authorization_endpoint": "https://auth-limit.baichengedu.com/oauth2/authorize",
  "token_endpoint": "https://auth-limit.baichengedu.com/oauth2/token",
  "userinfo_endpoint": "https://auth-limit.baichengedu.com/oauth2/userinfo",
  "jwks_uri": "https://auth-limit.baichengedu.com/oauth2/jwks",
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code", "refresh_token"]
}
```

### 5.3 Authorization Code 流程

先让用户登录本系统，拿到用户 Access Token。然后请求授权端点：

```bash
curl -i -sS "https://auth-limit.baichengedu.com/oauth2/authorize?response_type=code&client_id=<CLIENT_ID>&redirect_uri=https%3A%2F%2Fgateway.example.com%2Fcallback&scope=openid%20profile%20email&state=<STATE>" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

成功时返回 302，跳转到：

```text
https://gateway.example.com/callback?code=<AUTHORIZATION_CODE>&state=<STATE>
```

使用授权码换 Token：

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/oauth2/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=authorization_code" \
  -d "code=<AUTHORIZATION_CODE>" \
  -d "redirect_uri=https://gateway.example.com/callback" \
  -d "client_id=<CLIENT_ID>" \
  -d "client_secret=<CLIENT_SECRET>"
```

也可以使用 HTTP Basic 传递 Client 凭据：

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/oauth2/token" \
  -u "<CLIENT_ID>:<CLIENT_SECRET>" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=authorization_code" \
  -d "code=<AUTHORIZATION_CODE>" \
  -d "redirect_uri=https://gateway.example.com/callback"
```

成功响应：

```json
{
  "token_type": "Bearer",
  "access_token": "<ACCESS_TOKEN>",
  "expires_in": 1800,
  "refresh_token": "<REFRESH_TOKEN>",
  "refresh_token_expires_in": 86400
}
```

### 5.4 Refresh Token 流程

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/oauth2/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=refresh_token" \
  -d "refresh_token=<REFRESH_TOKEN>"
```

### 5.5 UserInfo

```bash
curl -sS "https://auth-limit.baichengedu.com/oauth2/userinfo" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

成功响应：

```json
{
  "sub": "<USER_ID>",
  "roles": ["service_operator"],
  "permissions": ["app:manage", "service:manage"]
}
```

## 6. 服务治理与服务发现

### 6.1 注册服务

需要 `service:manage` 权限。

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/services" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "订单服务",
    "code": "order-service",
    "baseUrl": "https://orders.example.com",
    "healthPath": "/health",
    "healthCheckInterval": 30
  }'
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `name` | 服务名称 |
| `code` | 服务编码，全局唯一 |
| `baseUrl` | 服务基础地址，不要带末尾 `/` |
| `healthPath` | 健康检查路径，空值时使用 `/health` |
| `healthCheckInterval` | 健康检测间隔，当前配置允许 10-300 秒 |

普通用户注册的服务初始为 `pending`，需要管理员审核。管理员注册服务会自动审核。

### 6.2 审核服务

只有管理员可以审核服务。

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/services/<SERVICE_ID>/approve" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>"
```

### 6.3 查询服务列表

```bash
curl -sS "https://auth-limit.baichengedu.com/api/services?name=订单&health=healthy" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

非管理员只能看到自己注册的服务。

### 6.4 服务发现

```bash
curl -sS "https://auth-limit.baichengedu.com/api/services/discover?name=订单&health=healthy" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

服务发现只返回满足以下条件的服务：

| 条件 | 值 |
| --- | --- |
| `approved` | `true` |
| `status` | `approved` |
| `healthStatus` | `healthy` |

服务刚审核完成时，健康状态可能仍是 `unknown`。等下一轮健康检测成功后，服务才会进入发现列表。

## 7. 限流

### 7.1 创建限流规则

需要 `limit:manage` 权限。

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/limit-rules" \
  -H "Authorization: Bearer <ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "serviceId": "<SERVICE_ID>",
    "dimension": "ip",
    "granularity": "second",
    "capacity": 100,
    "ratePerSecond": 10,
    "blacklistHits": 0,
    "blockSeconds": 0,
    "enabled": true
  }'
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `dimension` | 限流维度：`ip`、`user_id`、`app_id`、`path` |
| `granularity` | 统计粒度：`second`、`minute`、`hour`、`day` |
| `capacity` | 令牌桶容量，代表可承受突发请求数 |
| `ratePerSecond` | 每秒补充令牌数 |
| `blacklistHits` | 连续拦截达到该次数后临时拉黑；`0` 表示不触发 |
| `blockSeconds` | 临时拉黑秒数；`0` 表示不触发 |
| `enabled` | 是否启用 |

同一服务、同一维度、同一粒度只能存在一条启用规则；重复创建会返回 409。

如果服务没有匹配的启用规则，系统会按全局默认配置限流。当前示例配置默认启用 `ip/user_id/app_id` 维度，容量 `100`，每秒 `10` 个令牌。

### 7.2 调用限流校验

APISIX 或业务网关在转发请求前调用：

```bash
curl -i -sS -X POST "https://auth-limit.baichengedu.com/oidc/limit/verify" \
  -H "Content-Type: application/json" \
  -d '{
    "serviceId": "<SERVICE_ID>",
    "path": "/api/orders",
    "method": "GET",
    "ip": "203.0.113.10",
    "userId": "<USER_ID>",
    "appId": "<APP_ID>"
  }'
```

请求字段：

| 字段 | 是否必填 | 说明 |
| --- | --- | --- |
| `serviceId` | 是 | 服务记录 ID |
| `path` | 否 | 请求路径，用于 `path` 维度限流 |
| `method` | 否 | 请求方法，当前记录用途 |
| `ip` | 否 | 客户端 IP |
| `userId` | 否 | 用户 ID |
| `appId` | 否 | APP ID |

成功响应头：

```http
X-RateLimit-Remaining: 99
X-RateLimit-Reset: 1779173008
```

被限流时返回 429，并附带：

```http
Retry-After: 3
```

## 8. 黑白名单

### 8.1 新增黑名单

接口需要 `blacklist:manage` 权限，当前新增和删除操作要求管理员身份。

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/blacklists" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "serviceId": "<SERVICE_ID>",
    "type": "ip",
    "key": "203.0.113.10",
    "reason": "异常请求过多",
    "permanent": false,
    "expiresAt": "2026-05-20T08:00:00Z"
  }'
```

黑名单支持 `ip`、`user`、`app`、`token`。永久黑名单设置 `permanent=true`。

### 8.2 新增白名单

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/whitelists" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "serviceId": "<SERVICE_ID>",
    "type": "app",
    "key": "<APP_ID>",
    "reason": "核心内部系统",
    "expiresAt": "2026-06-01T00:00:00Z"
  }'
```

白名单支持 `ip`、`user`、`app`。白名单优先级高于黑名单，命中白名单时跳过限流。

### 8.3 查询和删除名单

```bash
curl -sS "https://auth-limit.baichengedu.com/api/blacklists?serviceId=<SERVICE_ID>" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>"

curl -sS -X DELETE "https://auth-limit.baichengedu.com/api/blacklists/<BLACKLIST_ID>" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>"

curl -sS "https://auth-limit.baichengedu.com/api/whitelists?serviceId=<SERVICE_ID>" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>"

curl -sS -X DELETE "https://auth-limit.baichengedu.com/api/whitelists/<WHITELIST_ID>" \
  -H "Authorization: Bearer <ADMIN_ACCESS_TOKEN>"
```

删除黑名单或白名单时，系统会同步清理对应 Redis 缓存；删除成功后新的限流校验不会继续命中已删除名单。

## 9. APISIX 对接

本系统可作为 APISIX 的统一身份、OIDC、限流和服务发现控制面。以下配置是可复制模板，实际字段请结合 APISIX 版本参考官方文档：

- [APISIX openid-connect 插件](https://apisix.apache.org/docs/apisix/plugins/openid-connect/)
- [APISIX forward-auth 插件](https://apisix.apache.org/zh/docs/apisix/next/plugins/forward-auth/)
- [APISIX Admin API](https://apisix.apache.org/docs/apisix/admin-api/)

### 9.1 用 openid-connect 保护业务路由

适用于浏览器访问或希望 APISIX 直接处理 OIDC 流程的场景。先在本系统创建 OIDC Client，拿到 `clientId` 和 `clientSecret`。

```bash
curl -sS -X PUT "http://127.0.0.1:9180/apisix/admin/routes/order-web" \
  -H "X-API-KEY: <APISIX_ADMIN_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "uri": "/orders/*",
    "plugins": {
      "openid-connect": {
        "client_id": "<CLIENT_ID>",
        "client_secret": "<CLIENT_SECRET>",
        "discovery": "https://auth-limit.baichengedu.com/.well-known/openid-configuration",
        "scope": "openid profile email",
        "bearer_only": false,
        "redirect_uri": "https://gateway.example.com/orders/callback",
        "ssl_verify": true
      }
    },
    "upstream": {
      "type": "roundrobin",
      "nodes": {
        "orders.example.com:443": 1
      },
      "scheme": "https"
    }
  }'
```

如果 APISIX 仅作为 API 网关校验调用方传入的 Bearer Token，可把插件配置为 `bearer_only=true`，由调用方自行先完成登录或 OIDC 换 Token。

### 9.2 用 forward-auth 调用 Token 校验

适用于网关在转发前把 `Authorization` 交给本系统校验的场景。

```bash
curl -sS -X PUT "http://127.0.0.1:9180/apisix/admin/routes/order-api-auth" \
  -H "X-API-KEY: <APISIX_ADMIN_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "uri": "/api/orders/*",
    "plugins": {
      "forward-auth": {
        "uri": "https://auth-limit.baichengedu.com/api/auth/verify",
        "request_method": "GET",
        "request_headers": ["Authorization"],
        "upstream_headers": ["X-Access-Token"],
        "client_headers": ["X-Access-Token"]
      }
    },
    "upstream": {
      "type": "roundrobin",
      "nodes": {
        "orders.example.com:443": 1
      },
      "scheme": "https"
    }
  }'
```

建议把 `X-Access-Token` 透传给客户端，以便客户端保存自动续签后的新 Access Token。若后端业务也需要识别用户，可在 APISIX 中解析 JWT 或由业务服务再次调用 `/api/auth/verify` 获取 `userId/roles/permissions`。

### 9.3 转发前调用限流校验

APISIX 官方插件没有直接内置本系统的限流协议。推荐用 serverless/Lua、自定义插件或外部插件在转发前调用：

```http
POST https://auth-limit.baichengedu.com/oidc/limit/verify
Content-Type: application/json

{
  "serviceId": "<SERVICE_ID>",
  "path": "<REQUEST_PATH>",
  "method": "<REQUEST_METHOD>",
  "ip": "<CLIENT_IP>",
  "userId": "<USER_ID>",
  "appId": "<APP_ID>"
}
```

伪代码：

```lua
local http = require("resty.http")
local cjson = require("cjson.safe")

local body = cjson.encode({
  serviceId = "<SERVICE_ID>",
  path = ngx.var.uri,
  method = ngx.req.get_method(),
  ip = ngx.var.remote_addr,
  userId = ngx.var.http_x_user_id,
  appId = ngx.var.http_x_app_id
})

local httpc = http.new()
local res, err = httpc:request_uri("https://auth-limit.baichengedu.com/oidc/limit/verify", {
  method = "POST",
  body = body,
  headers = { ["Content-Type"] = "application/json" },
  ssl_verify = true
})

if not res then
  return 500, { message = err }
end

if res.status == 429 or res.status == 403 then
  ngx.status = res.status
  ngx.header["Retry-After"] = res.headers["Retry-After"]
  ngx.say(res.body)
  return ngx.exit(res.status)
end

if res.status >= 400 then
  return ngx.exit(res.status)
end
```

生产建议：

| 建议 | 说明 |
| --- | --- |
| 明确失败策略 | Redis 或鉴权服务异常时，可按业务风险选择失败关闭或失败放行 |
| 透传限流头 | 将 `X-RateLimit-Remaining`、`X-RateLimit-Reset`、`Retry-After` 透传给客户端 |
| 服务 ID 固定配置 | 每条 APISIX route 固定一个 `serviceId`，避免运行时查错 |
| 记录 requestId | 网关日志记录本系统响应里的 `requestId`，方便排查 |

### 9.4 用服务发现同步 APISIX upstream

本系统的服务发现接口返回已审核且健康的服务。可以用定时任务同步 APISIX upstream。

查询服务发现：

```bash
curl -sS "https://auth-limit.baichengedu.com/api/services/discover?health=healthy" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

同步思路：

1. 定时调用 `/api/services/discover` 获取健康服务列表。
2. 按服务 `code` 映射 APISIX `upstream_id` 或 `route_id`。
3. 将每个服务的 `baseUrl` 解析为 APISIX upstream `nodes`。
4. 调用 APISIX Admin API 更新对应 upstream 或 route。
5. 如果服务从发现列表消失，不要立刻删除，可设置连续 N 次消失后再下线，避免健康检测抖动。

APISIX Admin API 示例：

```bash
curl -sS -X PUT "http://127.0.0.1:9180/apisix/admin/upstreams/order-service" \
  -H "X-API-KEY: <APISIX_ADMIN_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "roundrobin",
    "scheme": "https",
    "nodes": {
      "orders.example.com:443": 1
    }
  }'
```

## 10. 运维查询

### 10.1 健康检查和指标

```bash
curl -sS "https://auth-limit.baichengedu.com/health"
curl -sS "https://auth-limit.baichengedu.com/metrics"
```

`/metrics` 返回 Prometheus 指标，可接入现有监控系统。

### 10.2 日志查询

需要 `log:read` 权限。

```bash
curl -sS "https://auth-limit.baichengedu.com/api/logs?type=auth&result=failure&page=1&pageSize=20" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

支持日志类型：

| type | 说明 |
| --- | --- |
| `operation` | 管理操作日志 |
| `auth` | 登录、M2M 等鉴权日志 |
| `limit` | 限流日志 |
| `health` | 健康检测日志 |

时间筛选使用 RFC3339：

```text
startAt=2026-05-19T00:00:00Z&endAt=2026-05-20T00:00:00Z
```

### 10.3 健康检测日志

需要 `health:read` 权限。

```bash
curl -sS "https://auth-limit.baichengedu.com/api/health-checks?serviceId=<SERVICE_ID>&page=1&pageSize=20" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

### 10.4 限流统计

需要 `statistics:read` 权限。

```bash
curl -sS "https://auth-limit.baichengedu.com/api/limit-statistics?serviceId=<SERVICE_ID>&dimension=ip&page=1&pageSize=20" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

响应字段：

| 字段 | 说明 |
| --- | --- |
| `bucketTime` | 统计桶时间 |
| `serviceId` | 服务 ID |
| `dimension` | 限流维度 |
| `totalCount` | 总请求数 |
| `blockedCount` | 被拦截请求数 |

## 11. 常见排查

| 现象 | 排查建议 |
| --- | --- |
| 登录返回 401 | 检查用户名密码、用户状态是否启用、是否被临时锁定 |
| 登录返回 423 | 账号已被失败登录临时锁定，可等待锁定到期或由管理员调用 `/api/users/{id}/unlock` |
| 管理接口返回 401 | 检查 `Authorization: Bearer <ACCESS_TOKEN>` 是否存在，Token 是否过期或被登出吊销 |
| 管理接口返回 403 | 检查用户角色是否包含对应权限点；角色变更后需重新登录或刷新 Token |
| 创建服务后发现列表为空 | 服务需要审核通过，并且健康检测成功后 `healthStatus=healthy` 才会出现在发现列表 |
| M2M 返回 401 | 检查 `appId`、`timestamp`、`sign`；确认参数排序和签名字符串完全一致；不要重复使用同一签名 |
| 限流校验返回 403 | 命中了黑名单；如果同时配置白名单，确认主体类型和 key 是否一致 |
| 限流统计暂未出现 | 等待短时间后重试，统计写入和查询存在轻微延迟 |
| OIDC token 返回 `invalid_client` | 检查 `clientId/clientSecret/redirectUri` 是否与 OIDC Client 记录完全一致 |
| APISIX 配置不生效 | 检查 APISIX 插件版本字段、Admin API Key、route 匹配优先级和 upstream 是否可达 |
