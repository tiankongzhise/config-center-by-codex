# 外部读取鉴权引导

本文面向项目创建者和配置读取方，说明创建项目后如何拿到 auth-limit 授权并读取配置中心的密文配置。

## 先区分两个账号体系

配置中心账号和 auth-limit 账号不是同一个账号体系：

- 配置中心账号：在 `/register` 创建，只用于登录配置中心 UI、创建项目、维护 `config` 和 `env`。
- auth-limit 账号：在 auth-limit 中注册和授权，用于换取 `access_token`，再调用配置中心对外读取接口。

因此，刚在配置中心注册的新账号不能直接换取 auth-limit 的 `access_token`。如果需要用 Bearer Token 读取配置，必须先拥有一个 auth-limit 账号，并由 auth-limit 管理员授予合适角色或放行策略。

## 方式一：在 UI 获取 access_token

1. 登录配置中心。
2. 打开项目详情页。
3. 在“外部读取鉴权”区域选择 `Bearer Token`。
4. 输入 auth-limit 用户名和密码，点击“获取 access_token”。
5. 页面会显示短期 `access_token`，并自动生成读取 `config` 和 `env` 的 curl 示例。

页面不会把 auth-limit 密码或 token 写入配置中心数据库。token 只在当前页面显示，刷新页面后需要重新获取。

## 方式二：直接调用 auth-limit 登录接口

```bash
curl -sS -X POST "https://auth-limit.baichengedu.com/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "<AUTH_LIMIT_USERNAME>",
    "password": "<AUTH_LIMIT_PASSWORD>"
  }'
```

成功后使用响应中的 `data.accessToken`：

```bash
curl -sS "https://config-center.example.com/api/public/projects/<PROJECT_CODE>/config" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

读取 `env` 时把路径末尾换成 `/env`。

## 方式三：M2M 签名

服务、脚本和 CI 建议使用调用方自己的 auth-limit APP：

1. 在 auth-limit 为调用方创建 APP。
2. 保存创建或重置时一次性返回的 `appSecret`。
3. 每次请求生成 `timestamp`，按 auth-limit 文档计算 `sign`。
4. 调用配置中心对外读取接口时携带以下请求头：

```http
appId: <APP_ID>
timestamp: <UNIX_SECONDS>
sign: <HMAC_SHA256_HEX>
```

`AUTH_LIMIT_APP_ID` 和 `AUTH_LIMIT_APP_SECRET` 是配置中心服务接入 auth-limit 时使用的服务侧凭据，不建议作为普通调用方共享密钥。

## 常见问题

| 问题 | 处理方式 |
| --- | --- |
| 用配置中心账号换 token 失败 | 确认该账号是否也存在于 auth-limit。配置中心本地账号不会自动同步到 auth-limit。 |
| UI 获取 token 返回 401 | 检查 auth-limit 用户名、密码和账号状态。 |
| 对外读取返回 `external caller credential is required` | 请求没有带 `Authorization: Bearer <ACCESS_TOKEN>`，也没有带 M2M 的 `appId/timestamp/sign`。 |
| 对外读取返回 `auth-limit bearer verification failed` | token 过期、被吊销，或不是 auth-limit 颁发的 token。 |
| 对外读取返回 `AUTH_LIMIT_SERVICE_ID is not configured` | 部署方还没有执行 `register-service`，或 `.env` 缺少 `AUTH_LIMIT_SERVICE_ID`。 |
