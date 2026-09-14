# 部署（Azure Container Apps）

Subport 打包成单个容器：多阶段 `Dockerfile` 先用 Node 构建 Vue 前端，再用 Go
编译静态二进制，最后合到一个精简镜像里，由 Go 进程同时提供 API 与静态前端。

## 线上环境（australiaeast）

| 资源 | 名称 |
|---|---|
| 资源组 | `rg-subport` |
| 容器应用 | `subport` |
| 运行环境 | 复用 `rg-kiwi-demo/kiwi-env`（Container Apps managed env） |
| 镜像仓库 | `subportacr84125`（ACR，Basic） |
| 数据库 | `kiwi-pg` 服务器上的 `subport_prod` 库 |

应用地址：<https://subport.redbeach-8d18c6c7.australiaeast.azurecontainerapps.io>

配置以 Container App **secret** 注入，不写进镜像、不进 git：
`DATABASE_URL`、`SUBPORT_ADMIN_PASSWORD`、`SUBPORT_ANTIGRAVITY_CLIENT_ID/SECRET`、
`SUBPORT_INVITE_CODE`。数据库 schema 与种子由应用首启动时自动创建（见 `deploy/sql/`）。

## 重新构建并发布新版本

```bash
ACR=subportacr84125
# 云端构建（无需本地 Docker），从仓库根目录执行
az acr build --registry $ACR --image subport:latest .
# 让容器应用拉取新镜像（滚动一个新修订版）
az containerapp update -g rg-subport -n subport --image $ACR.azurecr.io/subport:latest
```

> Windows + Git Bash 调 az 时，遇到资源 ID 被路径转换污染，前面加
> `MSYS_NO_PATHCONV=1`。

## 首次从零搭建（简要）

```bash
az group create -n rg-subport -l australiaeast
az postgres flexible-server db create -g rg-kiwi-demo --server-name kiwi-pg -n subport_prod
az postgres flexible-server firewall-rule create -g rg-kiwi-demo -s kiwi-pg \
  -n AllowAzureServices --start-ip-address 0.0.0.0 --end-ip-address 0.0.0.0
az acr create -g rg-subport -n <acr> --sku Basic --admin-enabled true
az acr build --registry <acr> --image subport:latest .
# 复用现有 kiwi-env，或 az containerapp env create -g rg-subport -n subport-env
az containerapp create -g rg-subport -n subport \
  --environment <env-resource-id> \
  --image <acr>.azurecr.io/subport:latest \
  --registry-server <acr>.azurecr.io --registry-username <acr> --registry-password <pw> \
  --target-port 8080 --ingress external --min-replicas 1 --max-replicas 2 \
  --secrets database-url=... admin-password=... antigravity-id=... antigravity-secret=... \
  --env-vars DATABASE_URL=secretref:database-url \
             SUBPORT_ADMIN_PASSWORD=secretref:admin-password \
             SUBPORT_ANTIGRAVITY_CLIENT_ID=secretref:antigravity-id \
             SUBPORT_ANTIGRAVITY_CLIENT_SECRET=secretref:antigravity-secret \
             SUBPORT_INVITE_CODE=subport-invite
```

`min-replicas 1`：网关有后台 token 刷新与账号巡检，不能缩容到 0。
