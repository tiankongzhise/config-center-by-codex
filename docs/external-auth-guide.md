# 外部读取鉴权引导

本文面向项目创建者和配置读取方，说明如何通过配置中心获取鉴权 token，并读取配置中心的密文配置。

## 职责边界

调用方不需要自行申请 auth-limit 账号，也不需要理解 auth-limit 的 M2M 签名细节。配置中心负责签发读取配置所需的 `access_token` 和 `refresh_token`，并在服务端完成与 auth-limit 的限流联调。

配置中心 token 绑定到配置中心用户：

- A 用户生成或刷新 token，不会导致 B 用户的 token 失效。
- 同一用户可以生成多组 token，刷新其中一组只会轮换这组 token。
- token 只能读取所属配置中心用户创建的项目配置。

## 方式一：在 UI 获取 token

1. 登录配置中心。
2. 打开项目详情页。
3. 在“配置读取 Token”区域点击“生成鉴权 token”。
4. 页面会显示 `access_token` 和 `refresh_token`，并自动生成读取 `config` 和 `env` 的 curl 示例。
5. 需要续期时，点击“刷新 refresh_token”。刷新成功后旧 `refresh_token` 失效，请保存新的返回值。

页面只展示本次生成或刷新的 token。刷新页面后如需查看 token，需要重新生成，或粘贴已保存的 `refresh_token` 进行刷新。

## 方式二：直接调用 API 获取 token

```bash
curl -sS -X POST "https://config-center.example.com/api/auth/tokens" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "<CONFIG_CENTER_USERNAME>",
    "password": "<CONFIG_CENTER_PASSWORD>"
  }'
```

成功响应：

```json
{
  "tokenType": "Bearer",
  "accessToken": "<ACCESS_TOKEN>",
  "accessTokenExpiresAt": "2026-05-20T08:30:00Z",
  "refreshToken": "<REFRESH_TOKEN>",
  "refreshTokenExpiresAt": "2026-05-27T08:00:00Z"
}
```

刷新 token：

```bash
curl -sS -X POST "https://config-center.example.com/api/auth/tokens/refresh" \
  -H "Content-Type: application/json" \
  -d '{
    "refreshToken": "<REFRESH_TOKEN>"
  }'
```

刷新成功后会返回新的 `access_token` 和 `refresh_token`。旧 `refresh_token` 只能使用一次，已刷新过的 token 不能再次刷新。

## 读取配置

读取 `config`：

```bash
curl -sS "https://config-center.example.com/api/public/projects/<PROJECT_CODE>/config" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

读取 `env`：

```bash
curl -sS "https://config-center.example.com/api/public/projects/<PROJECT_CODE>/env" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

读取成功后返回密文配置。调用方使用项目对应的 RSA 私钥解密。

## 常见问题

| 问题 | 处理方式 |
| --- | --- |
| 获取 token 返回 401 | 检查配置中心用户名和密码。这里使用配置中心账号，不是 auth-limit 账号。 |
| 对外读取返回 `Authorization: Bearer <ACCESS_TOKEN> is required` | 请求没有携带配置中心签发的 Bearer Token。 |
| 对外读取返回 `invalid access token` | `access_token` 过期、被刷新吊销，或不是配置中心签发的 token。 |
| 对外读取返回 `resource not found` | 当前 token 所属用户没有这个项目，或项目编码/配置类型不正确。 |
| 对外读取返回 `AUTH_LIMIT_SERVICE_ID is not configured` | 部署方还没有执行 `register-service`，或 `.env` 缺少 `AUTH_LIMIT_SERVICE_ID`。 |
