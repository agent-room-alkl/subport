# 数据库初始化 SQL

Subport 运行在 **PostgreSQL** 上（未配置 `DATABASE_URL` / `PGHOST` 时回落到本地 SQLite）。
这些脚本是权威的建库/建表定义，与 `internal/store/postgres.go` 保持一致。

Go 进程启动时也会用 `CREATE TABLE IF NOT EXISTS` 兜底一遍，所以脚本主要用于：
在不启动应用的情况下预置一个全新数据库，或审阅 schema。

## 文件

| 文件 | 作用 | 连接的库 |
|---|---|---|
| `00_create_database.sql` | 创建 `subport` 数据库（不存在才建） | `postgres`（管理库） |
| `01_schema.sql` | 建全部表与索引（幂等，可重复执行） | `subport` |
| `02_seed.sql` | 默认模型路由 + 可用模型目录（幂等） | `subport` |

## 用法

```bash
# 1) 建库（连到管理库 postgres）
psql "host=<HOST> port=5432 user=<ADMIN> dbname=postgres sslmode=require" -f 00_create_database.sql

# 2) 建表 + 种子（连到 subport 库）
psql "host=<HOST> port=5432 user=<ADMIN> dbname=subport sslmode=require" -f 01_schema.sql
psql "host=<HOST> port=5432 user=<ADMIN> dbname=subport sslmode=require" -f 02_seed.sql
```

> Azure Database for PostgreSQL — Flexible Server：用管理员账号即可建库。若走 `DATABASE_URL`，
> 记得带 `?sslmode=require`。

首次启动应用还会自动创建 admin 账号（默认密码 `subport-admin`，用 `SUBPORT_ADMIN_PASSWORD` 覆盖）——
**上线前务必修改**。
