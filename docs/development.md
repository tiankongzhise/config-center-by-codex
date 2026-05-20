# 开发指南

## 本地准备

1. 安装 Go 1.26 或兼容版本。
2. 准备 PostgreSQL。
3. 在仓库根目录创建 `.env`，写入 PostgreSQL 管理员凭据和 auth-limit 管理员凭据。

`.env` 不提交到 Git。当前仓库的 `.gitignore` 已忽略 `.env`。

## 常用命令

```bash
go test ./...
go run ./cmd/config-center --help
go run ./cmd/config-center init-db
go run ./cmd/config-center migrate
go run ./cmd/config-center register-service
go run ./cmd/config-center serve --migrate
```

`serve` 命令只建议用于本地开发或临时调试。生产环境在宝塔面板中配置 Go 项目常驻进程，不要在 SSH 里以前台方式手动运行 `serve`。

## 提交流程

- 所有开发在开发分支完成，不直接提交到 `main`。
- 每实现一个功能点提交一次 commit。
- commit message 使用中文，说明本次实现内容和影响范围。
- 可以 push 开发分支，但不发起 PR。

## 测试要求

- 用户注册、登录、登出必须有测试覆盖。
- 项目隔离必须覆盖“用户不能访问他人项目”。
- 配置加密必须覆盖“数据库不保存明文”。
- auth-limit 客户端至少覆盖响应解析和错误处理。

## 编码约定

- 后端优先使用标准库，确有必要再引入依赖。
- HTTP 返回 JSON 时统一包含清晰错误信息。
- UI 页面不展示配置密文以外的 Secret。
- 日志中不要输出 `.env` 的任何 Secret。
