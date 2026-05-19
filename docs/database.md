# 数据库设计

## 数据库与账号

配置中心使用独立 PostgreSQL 数据库和专用数据库用户。管理员凭据只用于初始化：

- 创建配置中心数据库。
- 创建配置中心专用用户。
- 授权专用用户访问配置中心数据库。

应用启动、迁移和日常访问都使用专用用户。专用用户不应拥有管理其他数据库的权限。

## 环境变量

| 变量 | 说明 |
| --- | --- |
| `PG_HOST` | PostgreSQL 地址 |
| `PG_PORT` | PostgreSQL 端口 |
| `PG_ADMIN` | PostgreSQL 管理员账号，仅初始化使用 |
| `PG_ADMIN_SECRET` | PostgreSQL 管理员密码，仅初始化使用 |
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

迁移由应用内置 SQL 执行，可重复运行。启动服务前运行 `config-center migrate`，本地开发也可以使用 `config-center serve --migrate` 自动迁移。

## 数据保护

- 不建立保存配置明文的字段。
- 错误日志只记录配置长度、项目 ID、请求 ID 等元数据。
- `.env` 不纳入 Git，生产 Secret 只保存在部署环境。
