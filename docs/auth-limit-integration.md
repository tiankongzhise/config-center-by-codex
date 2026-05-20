# auth-limit 接入设计

## 职责边界

auth-limit 是统一鉴权、限流和服务治理系统，但不保存配置中心本地用户。配置中心的注册、登录、密码哈希和会话都在配置中心数据库中完成。

auth-limit 仅用于：

- 配置中心服务注册。
- 外部读取配置时校验调用身份。
- 外部读取配置时执行限流。
- 可选 M2M 调用方接入。

## 服务注册

首次注册时使用 `.env` 中的 auth-limit 管理员凭据引导一个配置中心专用 operator 用户，给它分配 `app:manage`、`service:manage`、`limit:manage`、`statistics:read` 权限，然后使用 operator 登录后的 Token 创建配置中心服务和 M2M APP。注册成功后把以下值写回 `.env`：

| 变量 | 说明 |
| --- | --- |
| `AUTH_LIMIT_BASE_URL` | auth-limit 基础地址，默认 `https://auth-limit.baichengedu.com` |
| `AUTH_LIMIT_SERVICE_ID` | 配置中心在 auth-limit 中的服务 ID |
| `AUTH_LIMIT_APP_ID` | M2M APP ID，如注册流程创建 |
| `AUTH_LIMIT_APP_SECRET` | M2M APP Secret，只写入本地 `.env` |

`register-service` 是幂等命令：

- `AUTH_LIMIT_SERVICE_ID`、`AUTH_LIMIT_APP_ID`、`AUTH_LIMIT_APP_SECRET` 三项都存在时，命令直接跳过，不重复创建服务或 APP。
- 三项不完整时，命令会优先使用 `AUTH_LIMIT_OPERATOR_USERNAME` 和 `AUTH_LIMIT_OPERATOR_PASSWORD` 登录后补齐注册。
- `AUTH_LIMIT_OPERATOR_PASSWORD` 为空时，才需要 `AUTH_LIMIT_ADMIN` 和 `AUTH_LIMIT_ADMIN_SECRET` 执行首次 operator 引导。
- 注册完成或检测到已注册后，命令会清空 `.env` 中的 `AUTH_LIMIT_ADMIN`、`AUTH_LIMIT_ADMIN_SECRET` 以及兼容旧命名的 `AUTH_SERVICE_ADMIN*`。

`AUTH_LIMIT_APP_SECRET` 是创建 APP 或重置 secret 时返回的一次性密钥，无法从 auth-limit 查询恢复。如果 APP 已经存在但 `.env` 缺少 secret，需要在 auth-limit 中重置 APP secret 后写回 `.env`。

## 外部读取鉴权

对外读取接口接受两类凭据：

- `Authorization: Bearer <token>`：转发给 auth-limit `/api/auth/verify` 校验。
- `appId/timestamp/sign`：转发给 auth-limit `/api/auth/m2m` 校验。

配置中心不解析或保存调用方身份，只使用 auth-limit 返回结果决定是否允许继续。

配置中心本地用户和 auth-limit 用户是分离的。用户在配置中心 UI 注册账号、创建项目后，只获得项目管理权限；这个账号不会自动成为 auth-limit 用户，也不能直接换取 auth-limit `access_token`。

项目创建者可以在项目详情页使用“外部读取鉴权”面板获取调用示例：

- Bearer Token：输入已存在的 auth-limit 用户名和密码，配置中心后端代理调用 auth-limit `/api/auth/login`，只把短期 `access_token` 返回到当前页面。
- M2M 签名：调用方使用自己的 auth-limit APP 凭据生成 `appId/timestamp/sign`，配置中心只负责把这些请求头转发给 auth-limit 校验。

如果调用方还没有 auth-limit 账号或 APP，需要先由 auth-limit 管理员创建并授权。`AUTH_LIMIT_APP_ID` 和 `AUTH_LIMIT_APP_SECRET` 是配置中心服务接入 auth-limit 时的服务侧凭据，不应该作为普通调用方共享密钥。

## 限流校验

鉴权通过后，配置中心调用 auth-limit `/oidc/limit/verify`：

```json
{
  "serviceId": "<AUTH_LIMIT_SERVICE_ID>",
  "path": "/api/public/projects/<code>/config",
  "method": "GET",
  "ip": "<client-ip>",
  "userId": "<auth-limit-user-id>",
  "appId": "<app-id>"
}
```

限流通过时返回配置密文；限流失败时透传 `429`、`Retry-After`、`X-RateLimit-Remaining` 和 `X-RateLimit-Reset` 等关键信息。

## 降级策略

默认采用失败关闭：auth-limit 不可用、鉴权失败或限流校验失败时，对外读取接口拒绝请求。这样可以避免配置在身份不明时被读取。
