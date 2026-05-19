# auth-limit 接入设计

## 职责边界

auth-limit 是统一鉴权、限流和服务治理系统，但不保存配置中心本地用户。配置中心的注册、登录、密码哈希和会话都在配置中心数据库中完成。

auth-limit 仅用于：

- 配置中心服务注册。
- 外部读取配置时校验调用身份。
- 外部读取配置时执行限流。
- 可选 M2M 调用方接入。

## 服务注册

初始化时使用 `.env` 中的 auth-limit 管理员凭据登录，然后调用服务注册接口创建配置中心服务。注册成功后把以下值写回 `.env`：

| 变量 | 说明 |
| --- | --- |
| `AUTH_LIMIT_BASE_URL` | auth-limit 基础地址，默认 `https://auth-limit.baichengedu.com` |
| `AUTH_LIMIT_SERVICE_ID` | 配置中心在 auth-limit 中的服务 ID |
| `AUTH_LIMIT_APP_ID` | M2M APP ID，如注册流程创建 |
| `AUTH_LIMIT_APP_SECRET` | M2M APP Secret，只写入本地 `.env` |

## 外部读取鉴权

对外读取接口接受两类凭据：

- `Authorization: Bearer <token>`：转发给 auth-limit `/api/auth/verify` 校验。
- `appId/timestamp/sign`：转发给 auth-limit `/api/auth/m2m` 校验。

配置中心不解析或保存调用方身份，只使用 auth-limit 返回结果决定是否允许继续。

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
