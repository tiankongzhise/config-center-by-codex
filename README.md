# config-center-by-codex

配置中心是一个 Go 单体应用，提供本地用户注册登录、项目隔离、RSA 加密配置存储、Web UI、管理 API 和对外配置读取 API。配置中心用户信息保存在自己的 PostgreSQL 数据库中；auth-limit 只用于外部读取配置时的鉴权、限流和服务治理。

## 功能

- 本地用户注册、登录、登出和会话 cookie。
- 每个用户只能管理自己创建的项目。
- 每个项目维护独立 RSA 公钥。
- `config` 和 `env` 写入前使用项目 RSA 公钥加密，数据库不保存明文。
- 外部读取接口返回密文，由调用方使用自己的私钥解密。
- 对外读取接口接入 auth-limit Bearer/M2M 鉴权和限流。
- 提供 PostgreSQL 专用数据库和专用用户初始化命令。

## 准备配置

复制示例配置：

```bash
cp .env.example .env
```

填写 `.env`：

```env
CONFIG_CENTER_ADDR=:8080
CONFIG_CENTER_BASE_URL=https://config-service.baichengedu.com

PG_HOST=127.0.0.1
PG_PORT=5432
PG_ADMIN=postgres
PG_ADMIN_SECRET=你的PostgreSQL管理员密码

AUTH_LIMIT_BASE_URL=https://auth-limit.baichengedu.com
AUTH_LIMIT_ADMIN=授权服务管理员账号
AUTH_LIMIT_ADMIN_SECRET=授权服务管理员密码
AUTH_LIMIT_OPERATOR_USERNAME=config_center_operator
AUTH_LIMIT_OPERATOR_PASSWORD=
AUTH_LIMIT_SERVICE_CODE=config-service-baichengedu
AUTH_LIMIT_SERVICE_NAME=配置中心生产服务
AUTH_LIMIT_APP_NAME=config-service-baichengedu
```

`CONFIG_CENTER_BASE_URL` 是注册到 auth-limit 的服务地址，必须是 auth-limit 能访问到的真实公网 HTTPS 地址。不要把 `localhost`、`127.0.0.1`、局域网 IP 或普通 HTTP 地址注册到远端 auth-limit。

`AUTH_LIMIT_ADMIN` 只用于一次性引导：创建配置中心专用 auth-limit 用户、创建角色并分配 `app:manage`、`service:manage`、`limit:manage`、`statistics:read` 权限。服务注册和 APP 创建会使用 `AUTH_LIMIT_OPERATOR_USERNAME` 登录后的 Token，不再直接使用 admin Token 执行业务接入操作。

`.env` 已被 `.gitignore` 忽略，不要提交生产密钥。

## Windows 本地联调

Windows 本地开发时，远端 auth-limit 无法访问你的 `localhost`。要完成真实联调，需要先给本地服务建立公网 HTTPS 隧道，再把隧道地址写入 `CONFIG_CENTER_BASE_URL`。

可选方式：

- Cloudflare Tunnel：`cloudflared tunnel --url http://127.0.0.1:8080`
- ngrok：`ngrok http 8080`
- frp：把本机 8080 映射到具备 HTTPS 的公网域名

拿到类似 `https://xxxx.trycloudflare.com` 或自己的 HTTPS 域名后：

```env
CONFIG_CENTER_ADDR=:8080
CONFIG_CENTER_BASE_URL=https://xxxx.trycloudflare.com
```

然后再执行：

```bash
go run ./cmd/config-center register-service --env .env
```

`register-service` 默认会拒绝本地地址，避免把远端 auth-limit 永远访问不到的地址注册进去。只有完全离线的本地实验才可以设置 `ALLOW_LOCAL_SERVICE_URL=true`，但这种模式不能算 auth-limit 真实联调。

## 初始化

创建配置中心专用数据库和专用用户：

```bash
go run ./cmd/config-center init-db --env .env
```

命令会在 `.env` 中写入：

```env
CONFIG_CENTER_DB_NAME=config_center
CONFIG_CENTER_DB_USER=config_center
CONFIG_CENTER_DB_PASSWORD=自动生成或已有密码
```

执行数据库迁移：

```bash
go run ./cmd/config-center migrate --env .env
```

注册到 auth-limit：

```bash
go run ./cmd/config-center register-service --env .env
```

命令会把 `AUTH_LIMIT_SERVICE_ID`、`AUTH_LIMIT_APP_ID`、`AUTH_LIMIT_APP_SECRET` 写回 `.env`。

如果使用绝对路径运行二进制，建议也使用绝对 `.env` 路径：

```bash
/www/wwwroot/config-service.baichengedu.com/config-center init-db --env /www/wwwroot/config-service.baichengedu.com/.env
```

## 宝塔面板部署构建

在 Windows 开发机执行：

```powershell
.\go_build.ps1
```

如果部署目录、监听端口或运行用户不同，可以显式指定：

```powershell
.\go_build.ps1 -DeployDir "/www/wwwroot/config-service.baichengedu.com" -ListenAddr ":8080" -ServiceUser "www"
```

构建产物位于：

```text
dist/config-center-linux-amd64/
```

目录内容：

- `config-center`：Linux amd64 静态 Go 二进制，无 CGO 外部依赖。
- `.env.example`：生产部署环境变量模板，默认使用 `https://config-service.baichengedu.com`。
- `config-center.service`：systemd 服务示例。
- `BUILD_INFO.txt`：构建信息和 SHA256。

上传到宝塔建议路径：

```text
/www/wwwroot/config-service.baichengedu.com
```

部署后把 `.env.example` 复制为 `.env`，填写 PostgreSQL 管理员密码和 auth-limit 管理员凭据，然后在服务器执行：

```bash
chmod +x ./config-center
./config-center init-db --env /www/wwwroot/config-service.baichengedu.com/.env
./config-center migrate --env /www/wwwroot/config-service.baichengedu.com/.env
./config-center register-service --env /www/wwwroot/config-service.baichengedu.com/.env
./config-center serve --env /www/wwwroot/config-service.baichengedu.com/.env --addr :8080 --migrate
```

宝塔反向代理或站点配置需把 `https://config-service.baichengedu.com` 转发到本机 `127.0.0.1:8080`。

## 启动

```bash
go run ./cmd/config-center serve --env .env --addr :8080
```

也可以启动前自动迁移：

```bash
go run ./cmd/config-center serve --env .env --addr :8080 --migrate
```

浏览器访问：

```text
http://localhost:8080
```

## 使用流程

1. 打开 `/register` 注册配置中心本地账号。
2. 登录后进入项目列表。
3. 新建项目，填写项目名称、编码和 RSA 公钥。
4. 在项目详情页保存 `config` 和 `env` 明文。
5. 服务端立即加密保存，只在数据库中保留密文和内容哈希。
6. 外部调用方通过 auth-limit 鉴权后读取密文配置。

## 对外读取接口

读取 config：

```bash
curl -sS "https://config-center.example.com/api/public/projects/<PROJECT_CODE>/config" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

读取 env：

```bash
curl -sS "https://config-center.example.com/api/public/projects/<PROJECT_CODE>/env" \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

M2M 调用方也可以按 `docs/api-usage-guide.md` 中的签名规则传递：

```http
appId: <APP_ID>
timestamp: <UNIX_SECONDS>
sign: <SIGN>
```

鉴权和限流都通过后，响应示例：

```json
{
  "project": {
    "code": "demo-service",
    "name": "Demo Service"
  },
  "config": {
    "projectId": "...",
    "kind": "config",
    "ciphertext": "...",
    "contentHash": "...",
    "updatedAt": "2026-05-19T12:00:00Z"
  }
}
```

## 常用命令

```bash
go test ./...
go run ./cmd/config-center init-db --env .env
go run ./cmd/config-center migrate --env .env
go run ./cmd/config-center register-service --env .env
go run ./cmd/config-center serve --env .env --addr :8080 --migrate
```

## 文档

- `docs/architecture.md`：架构和安全边界。
- `docs/database.md`：数据库和迁移设计。
- `docs/auth-limit-integration.md`：auth-limit 接入说明。
- `docs/development.md`：开发和提交流程。
- `docs/api-usage-guide.md`：auth-limit API 使用说明。

原始需求已归档到 `原始需求.md`。
