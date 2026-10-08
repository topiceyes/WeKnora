# 钉钉集成部署指南

本文面向 **部署运维人员** 与 **钉钉管理员**，介绍 WeKnora 钉钉集成（扫码登录 + 通讯录同步）的完整配置流程、工作机制与日常运维操作。

> 前置条件：已完成 WeKnora 标准版部署（Docker Compose 或 Helm），拥有一个可公网访问的 HTTPS 域名，且你拥有钉钉开放平台的企业管理员或应用开发者权限。

---

## 1. 功能概述

钉钉集成包含四个互相配合的能力：

| 能力 | 说明 |
| --- | --- |
| **钉钉扫码登录** | 员工用手机钉钉扫码即登录 WeKnora，无需记密码。登录入口与账号密码、OIDC 登录并存，互不影响。 |
| **通讯录每日同步** | 内置 cron 定时任务，默认每日凌晨 03:00 全量对账钉钉通讯录，刷新已绑定用户的昵称、手机号、部门、职位等资料快照。 |
| **离职自动禁用** | 员工在钉钉侧被删除（离职）后，下一次对账时自动将其 WeKnora 账号置为禁用（`is_active=false`），并立即撤销其全部登录令牌（踢下线）。 |
| **新入职自动绑定** | 新员工首次扫码登录，或在通讯录同步时，若其钉钉邮箱与已有 WeKnora 账号邮箱一致，自动建立绑定（账号合并），无需管理员手工操作。 |

四个公开登录端点（无需鉴权）：

- `GET /api/v1/auth/dingtalk/config`：查询钉钉登录是否启用（前端据此决定是否展示「钉钉登录」按钮），不含 AppSecret。
- `GET /api/v1/auth/dingtalk/url`：返回钉钉扫码授权地址（前端跳转用）。
- `GET /api/v1/auth/dingtalk/start`：直接 302 跳转到钉钉授权页（适合外部系统放链接一键登录）。
- `GET /api/v1/auth/dingtalk/callback`：钉钉扫码后的回调地址（在钉钉开放平台登记的就是它）。

---

## 2. 钉钉开放平台配置步骤

以下步骤在钉钉开放平台完成，约需 10 分钟。

### 2.1 创建企业内部应用

1. 浏览器打开 <https://open-dev.dingtalk.com/>，用企业管理员或有开发者权限的钉钉账号扫码登录。
2. 顶部导航进入 **应用开发** → 左侧选择 **企业内部应用** → 点击 **创建应用**。
3. 应用类型选择 **H5 微应用**，填写应用名称（如「WeKnora 知识库」）与描述，确认创建。

### 2.2 记录 AppKey / AppSecret

进入刚创建的应用，左侧 **应用信息** → **基础信息** 页面，记录：

- **AppKey** → 对应 `DINGTALK_AUTH_APP_KEY`
- **AppSecret** → 对应 `DINGTALK_AUTH_APP_SECRET`

AppSecret 等同于密码，只填在服务端 `.env` 中，不要写进前端代码或提交进仓库。

### 2.3 记录 CorpId

CorpId 是企业级标识，有两个查看入口：

- 开放平台首页右上角点击企业名称，在弹出的企业信息中查看；
- 或登录钉钉企业管理后台 <https://oa.dingtalk.com/>，在 **首页/企业信息** 中查看。

记录 **CorpId** → 对应 `DINGTALK_AUTH_CORP_ID`（多企业共部署时它是身份隔离键，单企业也必须填写）。

### 2.4 开通权限

应用内左侧 **权限管理**，搜索并勾选以下权限：

| 权限 | 用途 |
| --- | --- |
| 通讯录部门成员读权限（`Contact.Department.Read` / `Contact.User.Read`） | 通讯录每日同步：递归拉取部门列表与部门成员 |
| 个人通讯录读（`users/me`） | 扫码登录时读取登录用户的昵称、邮箱、手机号 |
| 按 unionId 获取用户 ID（`getbyunionid`，通常在通讯录权限组内） | 登录时将 unionId 换算为企业内部 userid |

### 2.5 配置扫码登录回调

1. 应用内找到 **登录与分享**（或 **统一身份认证**）入口，创建扫码登录授权。
2. 回调域名/回调地址填写：

   ```
   https://<你的域名>/api/v1/auth/dingtalk/callback
   ```

   **必须精确匹配**：协议（https）、域名、路径三者完全一致，末尾不要多带 `/`，也不要填成前端页面地址。该值同时写入 `.env` 的 `DINGTALK_AUTH_REDIRECT_URI`，两边必须相同。

### 2.6 服务器出口 IP 白名单（如有）

如果企业开启了应用的服务器出口 IP 限制，在应用 **安全设置** 中将 WeKnora 服务器的公网出口 IP 加入白名单，否则通讯录同步调用钉钉 OpenAPI 会被拒绝。

### 2.7 发布应用版本

权限、回调等改动 **必须发布新版本后才生效**：应用内 **版本管理与发布** → 创建版本 → 发布（可用范围选全体员工）。此后每次调整权限都要重新发布。

---

## 3. WeKnora .env 配置

所有配置项位于 `.env`（模板见 `.env.example` 的 **F4** 段），修改后重启 `weknora-app` 容器生效。

| 配置项 | 含义 | 示例 |
| --- | --- | --- |
| `DINGTALK_AUTH_ENABLE` | 钉钉扫码登录总开关。同时它也是通讯录同步与管理端 API 的注入前提——关闭后同步与管理端点均不启用。 | `true` |
| `DINGTALK_AUTH_APP_KEY` | 企业内部应用 AppKey（见 2.2）。 | `dingxxxxabcd` |
| `DINGTALK_AUTH_APP_SECRET` | 企业内部应用 AppSecret（见 2.2）。 | `xxxxxxxx` |
| `DINGTALK_AUTH_CORP_ID` | 企业 CorpId（见 2.3），作为身份绑定表的多企业隔离键。 | `ding9f50xxxx` |
| `DINGTALK_AUTH_REDIRECT_URI` | 扫码登录回调地址，**必须与钉钉后台登记的回调精确一致**（见 2.5）。 | `https://weknora.example.com/api/v1/auth/dingtalk/callback` |
| `DINGTALK_DEFAULT_TENANT_ID` | 钉钉用户默认进入的空间 ID（角色 viewer，首次登录建号时加入）。**留空则走平台默认租户策略**（与本地注册共用 `WEKNORA_AUTH_DEFAULT_TENANT_MODE`）。 | `123456` 或留空 |
| `DINGTALK_SYNC_ENABLE` | 通讯录同步开关，独立于登录开关。 | `true` |
| `DINGTALK_SYNC_CRON` | 同步调度表达式，兼容 5 段（分）与 6 段（秒）cron。默认 `"0 0 3 * * *"` 即**每日 03:00** 全量对账。 | `"0 0 3 * * *"` |

最小可用配置（登录 + 同步全开）：

```env
DINGTALK_AUTH_ENABLE=true
DINGTALK_AUTH_APP_KEY=dingxxxxabcd
DINGTALK_AUTH_APP_SECRET=你的AppSecret
DINGTALK_AUTH_CORP_ID=ding9f50xxxx
DINGTALK_AUTH_REDIRECT_URI=https://weknora.example.com/api/v1/auth/dingtalk/callback
DINGTALK_DEFAULT_TENANT_ID=
DINGTALK_SYNC_ENABLE=true
DINGTALK_SYNC_CRON="0 0 3 * * *"
```

验证配置：启动后访问 `GET /api/v1/auth/dingtalk/config`，应返回 `{"success": true, "enabled": true, ...}`；登录页应出现「钉钉登录」入口。

---

## 4. 工作机制说明

### 4.1 扫码登录流程

```
员工浏览器                WeKnora 后端                  钉钉开放平台
    │ 点击「钉钉登录」          │                            │
    │───────────────────────>│ GET /auth/dingtalk/url      │
    │                        │ （生成带 nonce 签名的 state） │
    │<── 授权地址 ────────────│                            │
    │ 跳转钉钉扫码页           │                            │
    │────────────────────────────────────────────────────>│
    │                        │   手机钉钉扫码、确认授权       │
    │<────────── 302 回调（authCode + state）───────────────│
    │───────────────────────>│ GET /auth/dingtalk/callback  │
    │                        │ 校验 state/nonce 防回放        │
    │                        │ authCode → 个人 userToken     │
    │                        │ ──────────> users/me 取资料   │
    │                        │ ──────────> unionId 换 userid │
    │                        │ 账号合并（见 4.2）→ 签发 JWT   │
    │<── 302 回前端登录页（携带登录结果）──────────────────────│
```

安全细节：state 由服务端签名并绑定一次性 nonce cookie，回调校验后立即作废，防止 CSRF 与授权码回放；AppSecret 只在服务端使用。

### 4.2 账号合并规则

钉钉登录拿到用户资料后，按以下顺序决定落到哪个本地账号：

1. **绑定表命中**：该钉钉身份（corpId + unionId/userid）已绑定过 → 直接使用绑定的本地账号；
2. **邮箱匹配**：未绑定，但钉钉邮箱与某个已有 WeKnora 账号邮箱一致 → 自动建立绑定（账号合并），保留原账号全部数据；
3. **无匹配**：自动注册新账号并建立绑定；若配置了 `DINGTALK_DEFAULT_TENANT_ID`，新账号加入该空间（角色 viewer），否则走平台默认租户策略。

钉钉侧未配置邮箱时，系统用 `<unionId>@dingtalk.local` 占位邮箱兜底，保证账号唯一。若合并后账号已被禁用（如离职禁用），登录会被拒绝并提示账号已禁用。

### 4.3 离职对账逻辑

每次同步（cron 或手动触发）执行两轮：

1. **第一遍（资料对账）**：拉取企业全员（递归部门 → 逐部门分页成员，按 userid 去重）。已绑定的更新资料快照；未绑定但邮箱匹配本地账号的补绑定；**同步不会为陌生人主动建号**（建号只发生在扫码登录时）。
2. **第二遍（离职对账）**：本地活跃绑定与远端全员求差集 —— 本地有绑定但钉钉通讯录里查不到的，判定为疑似离职 → 账号禁用（`is_active=false`）+ 撤销全部登录令牌（立即踢下线）。

### 4.4 四重安全保护

离职自动禁用是破坏性操作，系统内置四重保护，宁可漏禁不可误伤：

1. **仅处理钉钉绑定用户**：对账范围只含钉钉身份绑定表中的账号，本地注册账号、OIDC 账号永不被通讯录同步触碰；
2. **特权账号豁免**：系统管理员（system_admin）与跨租户超管（can_access_all_tenants）永不被同步禁用，防止把管理员自己锁在门外；
3. **半量保护**：若钉钉侧拉取到的人数低于本地活跃绑定数的 50%，判定远端数据异常（如应用权限被收紧、部门被裁撤），**整批跳过禁用**，并在本次运行记录中写入告警信息；
4. **失败即停**：拉取 token、部门列表、部门成员任何一步失败，本轮整体标记为 failed，**绝不做任何禁用**——网络抖动不会导致误禁用。

### 4.5 unionId 占位降级

扫码登录主链路是 authCode → userToken → users/me → 按 unionId 换企业内部 userid。若应用未开通 `getbyunionid` 权限，第 4 步会失败，此时**不阻断登录**：系统先用 `union:<unionId>` 占位记录建立绑定，用户正常登录使用。占位记录无法参与通讯录对账（对不上部门成员的 userid），离职对账时会自动跳过；待权限补齐、该用户下次登录或下一轮同步后，绑定会升级为真实 userid，自动纳入对账范围。

---

## 5. 管理操作

以下接口均挂在 `/api/v1/system/dingtalk/*`，要求 **JWT 会话 + 系统管理员（SystemAdmin）** 身份（API Key 默认拒绝）。带 Bearer Token 调用：

```bash
TOKEN=<系统管理员的 access_token>
BASE=https://weknora.example.com/api/v1
```

### 5.1 手动触发同步

```bash
curl -X POST "$BASE/system/dingtalk/sync" -H "Authorization: Bearer $TOKEN"
```

立即执行一次全量对账（补绑定 / 更新资料 / 离职禁用），返回本次运行记录（远端人数、新建绑定数、更新数、禁用数等）。已有同步进行中时返回 409，不会并发跑两轮。日常运维等不到凌晨 03:00 时（如刚开通了权限、刚处理了一批离职）用它立即生效。

### 5.2 查看同步历史

```bash
curl "$BASE/system/dingtalk/sync/runs?page=1&page_size=20" -H "Authorization: Bearer $TOKEN"
```

分页返回运行记录（按开始时间倒序），每条含触发方式（cron/manual）、状态（running/success/failed）、各项计数与错误信息。排障先看这里。

### 5.3 查看绑定关系

```bash
curl "$BASE/system/dingtalk/identities?keyword=张三" -H "Authorization: Bearer $TOKEN"
```

分页返回钉钉身份 ↔ 本地账号绑定关系，`keyword` 可按昵称 / 手机号 / 邮箱 / 用户名模糊筛选。用于确认某个员工是否已绑定、绑定到了哪个账号。

### 5.4 手动禁用 / 启用用户

```bash
# 禁用
curl -X PUT "$BASE/system/users/<用户ID>/status" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"is_active": false}'

# 恢复
curl -X PUT "$BASE/system/users/<用户ID>/status" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"is_active": true}'
```

用于误禁用恢复、临时停权等场景。被禁用的用户令牌立即失效，无法登录与调用任何接口。

以上操作在管理界面（系统管理 → 钉钉集成页）均有对应按钮，不想调 API 可直接在页面上操作。

---

## 6. 故障排查 FAQ

**Q1：扫码后跳转失败 / 提示回调地址不合法？**
最常见原因。逐项核对：`.env` 的 `DINGTALK_AUTH_REDIRECT_URI` 与钉钉后台「登录与分享」登记的回调是否**逐字符一致**（协议、域名、路径，注意 https 与末尾 `/`）；改动后是否重新发布了应用版本；确认浏览器实际访问的域名与配置域名一致（不要在调试时用 IP 或 localhost 访问）。

**Q2：登录正常但同步一个人都拉不到？**
检查通讯录权限（`Contact.Department.Read` / `Contact.User.Read`）是否已勾选**并发布新版本**；检查应用安全设置中的服务器出口 IP 白名单是否包含 WeKnora 出口 IP；调用 `GET /system/dingtalk/sync/runs` 看最近一次运行记录的错误信息（token 获取失败多为凭证/IP 问题，拉成员失败多为权限问题）。

**Q3：员工已离职，但账号没被禁用？**
先查 `GET /system/dingtalk/sync/runs` 最近一次运行：`status=failed` 说明拉取失败（按 Q2 排查）；`success` 但 `error_message` 含 "below 50%" 说明触发了半量保护（检查钉钉侧通讯录可见范围是否被收窄）；该用户若是系统管理员或跨租户超管则属保护性豁免，需先降权再禁用；若其绑定记录是 `union:` 占位（从未成功换过 userid），属降级行为，补齐 `getbyunionid` 权限后下一轮对账生效。

**Q4：用户被误禁用了，如何恢复？**
调用 `PUT /api/v1/system/users/<用户ID>/status`，请求体 `{"is_active": true}`（见 5.4），或在管理界面将该用户重新启用。恢复后用户重新扫码登录即可。

**Q5：钉钉登录按钮不显示？**
访问 `GET /api/v1/auth/dingtalk/config`：若返回 `enabled=false`，检查 `DINGTALK_AUTH_ENABLE=true` 是否生效（改 `.env` 后需重启容器）；`AppKey` / `RedirectURI` 为空时登录端点也会拒绝服务。

**Q6：cron 表达式怎么写？**
兼容 5 段（`分 时 日 月 周`）与 6 段（`秒 分 时 日 月 周`）。默认 `"0 0 3 * * *"`（6 段式）= 每日 03:00:00；改成 `"0 30 2 * * *"` 即每日 02:30。表达式非法时调度器不启动，启动日志有明确报错。

---

## 附：相关配置与代码位置

- 配置模板：`.env.example` F4 段（钉钉扫码登录 + 通讯录同步）
- 登录端点实现：`internal/handler/auth.go`、`internal/dingtalk/login_service.go`
- 同步与离职对账：`internal/dingtalk/sync_service.go`、`internal/dingtalk/scheduler.go`
- 管理端 API：`internal/handler/dingtalk_admin.go`（路由注册见 `internal/router/routes_auth_tenant.go`）
