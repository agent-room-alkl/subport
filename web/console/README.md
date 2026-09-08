# Subport 用户端控制台

客户用的界面。管理端在 `../admin/`，两端共用 `../shared/app.css`，看起来是同一个产品。

零依赖、零构建步骤，和管理端一样。

## 页面

| 页面 | 作用 |
|---|---|
| 总览 | 剩余额度、已用、启用中的密钥数，以及「三步开始」 |
| 密钥 | 新建 / 停用 / 启用 / 删除；**密钥只在创建时显示一次** |
| 调用记录 | 时间、模型、token、花费、尝试次数、结果 |
| 充值 | 兑换码与充值（占位，功能未开放） |
| 接入指引 | 可一键复制的 curl 和 Python 示例 |

## 两个设计上的取舍

**1. 密钥只显示一次，并且说清楚。**
创建后弹出的绿框写明「离开本页无法再次查看，丢了只能删掉重建」。后端只存哈希，
所以这不是界面偷懒，是它真的取不回来——与其让用户以为能找回，不如当场讲明白。

**2. 接入地址从 `window.location.origin` 读，绝不写死。**
一段指向错误主机的 curl 比没有示例更糟：用户会照着跑，失败，然后怀疑是自己的问题。
页面上显示的地址永远是他此刻访问的这个地址。

## 调用记录里的「中途断流」

网关的规则是：**一旦开始向客户端输出就不再重试**，因为重试会让用户收到重复内容。
代价是输出中途断掉不会自动重来。这类记录在本页标为「中途断流」，
并在顶部提示按已生成 token 计费、可据此申诉。

把这件事摆在用户眼前，而不是藏进日志——用户看到「断了还扣钱」时，
至少知道这是已知的、有说法的，不是系统在乱扣。

## 跑起来

后端会一并托管：

```bash
go run ./cmd/subport
```

打开 <http://127.0.0.1:8080/console/index.html>。

后端未连接时自动进入演示模式，顶部有橙色横幅标注数据不是真实的。

## 接口

```
POST /api/auth/register   {username, password, invite_code}
POST /api/auth/login      {username, password}
POST /api/auth/logout
GET  /api/console/me
GET  /api/console/quota   -> {quota_total, quota_used, remaining}
GET  /api/console/keys
POST /api/console/keys    {name}          -> {key, secret}  secret 仅此一次
PATCH  /api/console/keys/{id}  {enabled}
DELETE /api/console/keys/{id}
GET  /api/console/usage
```

所有 `/api/console/*` 都由后端按已登录用户过滤，**前端从不发送 user id**。
