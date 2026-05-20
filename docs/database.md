# 数据库设计

## 数据库与账号

配置中心使用独立 PostgreSQL 数据库和专用数据库用户。管理员凭据只用于首次初始化：

- 创建配置中心数据库。
- 创建配置中心专用用户。
- 授权专用用户访问配置中心数据库。

应用启动、迁移和日常访问都使用专用用户。专用用户不应拥有管理其他数据库的权限。

`init-db` 是幂等命令。它会先使用 `CONFIG_CENTER_DB_*` 检查专用账号是否已经可连接；如果可连接，就直接成功返回，不再要求 `PG_ADMIN` 和 `PG_ADMIN_SECRET`。只有专用账号不存在或不可用时，才需要 PostgreSQL 管理员凭据来创建数据库和用户。

首次创建成功后，`init-db` 会把 `.env` 中的 `PG_ADMIN` 和 `PG_ADMIN_SECRET` 清空。生产运行环境不应长期保存 PostgreSQL 超级账号信息。

## 环境变量

| 变量 | 说明 |
| --- | --- |
| `PG_HOST` | PostgreSQL 地址 |
| `PG_PORT` | PostgreSQL 端口 |
| `PG_ADMIN` | PostgreSQL 管理员账号，仅首次引导使用，成功后会被清空 |
| `PG_ADMIN_SECRET` | PostgreSQL 管理员密码，仅首次引导使用，成功后会被清空 |
| `CONFIG_CENTER_DB_NAME` | 配置中心数据库名 |
| `CONFIG_CENTER_DB_USER` | 配置中心专用数据库用户 |
| `CONFIG_CENTER_DB_PASSWORD` | 配置中心专用数据库密码 |

## 表结构

### users

保存配置中心本地用户。

| 字段 | 说明 |
| --- | --- |
| `id` | UUID 主键 |
| `username` | 用户名，全局唯一 |
| `password_hash` | bcrypt 密码哈希 |
| `display_name` | 显示名 |
| `created_at` | 创建时间 |
| `updated_at` | 更新时间 |

### sessions

保存本地登录会话。

| 字段 | 说明 |
| --- | --- |
| `id` | UUID 主键 |
| `user_id` | 用户 ID |
| `token_hash` | 会话 Token 哈希 |
| `expires_at` | 过期时间 |
| `created_at` | 创建时间 |
| `revoked_at` | 登出或吊销时间 |

### api_tokens

保存配置读取 API 的 access token 和 refresh token 哈希。

| 字段 | 说明 |
| --- | --- |
| `id` | UUID 主键 |
| `user_id` | token 所属配置中心用户 ID |
| `access_token_hash` | `access_token` 哈希，唯一 |
| `refresh_token_hash` | `refresh_token` 哈希，唯一 |
| `access_token_expires_at` | access token 过期时间 |
| `refresh_token_expires_at` | refresh token 过期时间 |
| `created_at` | 创建时间 |
| `revoked_at` | 刷新、吊销或失效时间 |

刷新 token 时只吊销当前 `refresh_token` 对应的这一行，再为同一用户创建新记录。同一用户其它 token 和其它用户 token 不会被批量吊销。

### projects

保存用户创建的项目。

| 字段 | 说明 |
| --- | --- |
| `id` | UUID 主键 |
| `owner_id` | 创建用户 ID |
| `name` | 项目名称 |
| `code` | 项目编码，全局唯一，用于对外读取 |
| `description` | 项目描述 |
| `rsa_public_key` | 项目方提供的 RSA 公钥 |
| `created_at` | 创建时间 |
| `updated_at` | 更新时间 |

### project_configs

保存项目配置密文。

| 字段 | 说明 |
| --- | --- |
| `project_id` | 项目 ID |
| `kind` | `config` 或 `env` |
| `ciphertext` | RSA 加密后的密文，Base64 编码 |
| `content_hash` | 明文 SHA-256 摘要，用于判断是否变化，不用于还原明文 |
| `updated_at` | 更新时间 |

## 迁移策略

迁移由应用内置 SQL 执行，可重复运行。生产环境先运行 `config-center migrate` 完成一次性迁移，再由宝塔面板托管 `serve --migrate` 作为常驻进程；本地开发可以直接使用 `config-center serve --migrate` 临时启动。

`migrate` 和 `serve --migrate` 都只使用专用数据库账号，不依赖 `PG_ADMIN`。初始化完成后删除或清空管理员账号不会影响启动。

## 数据保护

- 不建立保存配置明文的字段。
- 错误日志只记录配置长度、项目 ID、请求 ID 等元数据。
- `.env` 不纳入 Git，生产 Secret 只保存在部署环境。
