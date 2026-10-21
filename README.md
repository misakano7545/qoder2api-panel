<p align="center">
  <h1>Qoder2API Panel</h1>
</p>

<p align="center">
  <b>把 Qoder 编码助手通道包成 OpenAI / Anthropic 兼容 API 的多账号网关 · 附 Web 管理面板</b><br>
  双区域 · COSY 签名转发 · 账号池轮转 · 熔断与冷却 · 会话粘性 · 每日签到 · 单二进制 · 零第三方依赖
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22-00ADD8?logo=go&logoColor=white&style=flat-square">
  <img alt="API" src="https://img.shields.io/badge/API-OpenAI%20%7C%20Anthropic-412991?style=flat-square">
  <img alt="Deploy" src="https://img.shields.io/badge/Deploy-Single_Binary%20%7C%20Docker-2496ED?style=flat-square">
  <img alt="License" src="https://img.shields.io/badge/License-MIT-green?style=flat-square">
</p>

---

> 架构与交互以 [WorkBuddy2API Panel](https://github.com/linguo2625469/workbuddy2api-panel) 为基线；
> 协议事实取自 `bincooo/qoder2api`、`shuishuipingan/qoder2api-hub`、`nullstacks/qoder2api`、`swysgh/qoder2api` 等公开实现。

> [!IMPORTANT]
> **本仓的架构基线 [linguo2625469/workbuddy2api-panel](https://github.com/linguo2625469/workbuddy2api-panel) 已于 2026-10-21 停止维护**；Qoder 协议事实取自公开实现并用本仓测试固定，此后由本仓独立演进，**无官方支持与后续保障**。
>
> **维护方式披露**：本仓的 issue / PR 处置、代码审查、测试与修复实现由 **Ekko Studio + Hermes Agent**（AI 编码 agent）辅助完成，合并与发布决策由维护者本人作出。作者是学生，因家庭经济困难，周末需兼职，业余还有摄影、公交等自己的兴趣——**本项目兴趣驱动，更新随缘，不代表作者有全部精力投入维护**，也可能出现较长时间没有提交，请勿当作有 SLA 的商业项目。
>
> 风险自评：**上游随时可能收紧导致通道不可用**；因使用非官方客户端导致的账号风控 / 封禁，由使用者自行承担。

> [!WARNING]
> **郑重提醒**——这段话针对两类人：**现实中与作者同校的使用者**，以及**拿本项目搭建对外中转站、做批量小号 / 售卖额度等恶意滥用的人**。
>
> 作者无法追踪、也不会去追踪任何人的行为，不对任何人行使监控。但必须把话说清楚：上述用法**严重违背上游项目作者与维护者的初衷**，也是消耗本仓作者维护动力的最大来源——本项目免费、开源、由一名学生用课余时间维护，不是给任何人拿去转卖的资源。
>
> 如果哪一天这个项目**确确实实让作者心力憔悴、不再有动力维护，本仓也会停止维护**，且不另行通知。上游的今天，可能就是本仓的明天，请自重。
>
> 同样的底线也适用于**同校使用者**：若有人出于一己私利，或以作者无法理解、也不知情的行为，把本项目的情况反映到**学校信息中心**、作者所在的**二级学院**，乃至**学院院长与校方其他上级领导**，藉此换取奖学金等个人好处，**本仓也会同时停止维护**。这不是赌气——接受核查、说明与后续处理所占用的时间与心力，已经超出本仓能承受的维护成本。

## 项目简介

把 [Qoder](https://qoder.com)（国际版 `qoder.com` / 国内版 `qoder.com.cn`）的编码助手通道
包装成 **OpenAI 兼容网关**，自带一个 Web 管理面板：多账号池、熔断冷却、会话粘性、
COSY 签名转发、每日签到领 Credits、额度/套餐查询、模型目录、用量台账与请求日志。

单二进制 + 内嵌页面 + 一份 JSON 配置，**纯 Go 标准库，零第三方依赖**。

## 核心能力

- **三种入站协议**：`POST /v1/chat/completions`（OpenAI chat，流式/非流式）、
  `POST /v1/responses`（Codex）、`POST /v1/messages` 与 `POST /messages`（Anthropic，支持 `x-api-key`）；
  三者都先归一成 chat 请求，再走**同一条链路**（挑号、冷却、会话粘性、提示词改写）
- **`GET /v1/models`**：模型目录（两区并集；上游 `model/list` 是 GET 也必须带 `{}` body + 签名的
  形状，本仓照实现，10 分钟缓存，拉不到回落到静态兜底）
- **模型页直说价格与能力**：限免模型把原价划掉（上游 `is_free` + `original_price_factor`），
  促销窗口与折扣系数照原值带出；思考档位按官方白名单归一（对象/数组/字符串三种下发形态都认），
  并标出默认档位与「可关」；池里没有该地区可用号时逐条标「无可用号」
- **双区域**：国际版走 `api3.qoder.sh`（`jt-` 令牌分流到 `api2`），国内版走 `gateway.qoder.com.cn`；
  账号地区决定模型可见性与出口
- **多账号池**：积分加权挑选（闲置补偿 + 快过期优先）、在途限额、熔断、降权、计划冷却；
  401 连续 3 次才禁用（一次抖动不杀号）
- **错误分类**（HTTP 级与流内信封共用一张表）：额度/套餐 → 计划冷却；限流 → 软冷却退避；
  登录态失效 → 会话失效计数；**排队（10605）不罚号**，换号重试即可
- **账号接入两条路**：设备授权（PKCE S256，面板给链接、浏览器授权、服务端自己轮询收号）、
  PAT / refreshToken 直登；都落到 `auths/qoder-<uid>.json`（0600，原子写回）
- **签到与额度**：`sash` 活动列表 → 领取 `CLAIM_BENEFIT`（官方幂等，已领即「今日已领取」）；
  额度按 `userQuota + addOnQuota + orgResourcePackage` 摊成「积分构成」
- **COSY 签名转发**：AES-128-CBC 身份包 + RSA(PKCS#1 v1.5) 密钥 + MD5 签名，请求体走上游
  自定义编码（`Encode=1`）；设备指纹 `cosy-machineid` 按账号稳定保存，可一键轮换
- **账号诊断与重测**：每个号直说「为什么接不了单」（禁用 / 冷却剩余时间 / 额度用尽 /
  登录态将过期 / 熔断连败），零网络推断；「重测」真实发一次 `max_tokens=1` 的对话，
  通过才解除冷却标记，「重置标记」只清显示不发请求
- **Token 保活 / 定时签到 / 周期余额**：排程时点可配，refresh token 轮换后原子落盘
- **可观测**：请求记录（含客户端 IP/UA，可关）、用量台账（时间片 × 账号 × 模型）、
  Prometheus 指标、运行日志环

## 快速开始

```bash
go build ./...                                          # 全量构建
CGO_ENABLED=0 go build -o q2api ./cmd/server            # 静态二进制
./q2api -config ./config.json                           # 缺配置时先写一份默认配置再起
```

- 默认监听 `:7864`，面板 `http://127.0.0.1:7864/panel/`
- 首次运行会生成 `config.json`，其中 `api_key` 是随机 `sk-…`（面板「配置」页可改）
- 账号目录不存在会自动创建；没有账号是正常启动态

Docker：

```bash
cp config.example.json config.json
docker compose up -d          # 镜像内二进制 /app/q2api，数据在 ./auths 与 ./data
```

## 接入客户端

1. 面板 → **账号** → **添加账号**
   - 浏览器登录：选地区（国际版 / 国内版）→ 「打开」在新标签页完成 Qoder 授权 → 面板每 3 秒自动收号
   - refreshToken / PAT：另一页签直接粘（`pt-` 开头按 PAT 兑换，其余按刷新令牌续期）
2. **模型**页确认目录已拉到官方清单（拉不到会显示静态兜底并留日志）
3. 客户端指向 `http://<host>:7866/v1`，`Authorization: Bearer <api_key>`：

```bash
curl http://127.0.0.1:7866/v1/chat/completions \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"model":"Qwen3.8-Max","stream":true,
       "messages":[{"role":"user","content":"用 Go 写个快排"}]}'
```

模型名接受官方显示名、小写写法与上游 key（`Qwen3.8-Max` / `qwen3.8-max` / `qmodel_38max`）；
未知模型直接 400，不做猜测。

地区用前缀指定：`cn:Auto`（国内版）/ `global:Auto`（国际版）；不带前缀按目录归属走，
某个地区没有可用账号时会落到另一个地区。

## 端点

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/chat/completions` | OpenAI chat（流式 / 非流式） |
| POST | `/v1/responses` | Codex Responses |
| POST | `/v1/messages`、`/messages` | Anthropic Messages |
| POST | `/v1/messages/count_tokens`、`/messages/count_tokens` | 上下文 token 估算（字符/4，本地算，不占额度） |
| GET | `/v1/models` | 模型目录 |
| GET | `/status` | 账号池状态（需 key） |
| GET | `/healthz` | 存活探针（不需要 key） |
| GET | `/panel/` | 管理面板 |

面板 API 在 `/panel/api/*`（overview / models / packages / usage / request_logs / config /
login/* / accounts/* / checkin / balance / keepalive）。空 `api_key` 时只信本机回环，
写操作另外校验同源。

## 配置

见 `config.example.json`。要点：

| 键 | 说明 |
|---|---|
| `listen` / `api_key` | 监听地址；空 = 不鉴权（仅本机） |
| `auth_dir` / `state_file` | 账号目录与池状态；用量台账与请求归档默认跟 `state_file` 同目录 |
| `default_model` | 请求缺 `model` 或写 `auto` 时用，默认 `Auto` |
| `cooldown.plan_credit` | 额度不足/套餐不支持的硬冷却**上限**（默认 12h）。实际冷到北京时间次日 0 点即额度重置点，本值只做上限截断 |
| `upstream.gateway_host` / `gateway_host_cn` | 网关基址，留空用内置（`api3.qoder.sh` / `gateway.qoder.com.cn`） |
| `upstream.timeout_seconds` / `header_timeout_seconds` / `idle_timeout_seconds` | 短请求 / 首字节 / 流内空闲 |
| `pool.*` | 熔断、降权、在途、闲置补偿、快过期窗口 |
| `session_sticky.*` | 同一会话粘同一账号（命中上游缓存） |
| `prompt.mode` / `file` | `passthrough` / `custom` / `append` 系统提示词改写 |
| `schedule.*` / `logging.*` | 签到/保活时点、余额周期、请求记录与归档 |

环境变量覆盖：`Q2A_<段>_<键>`，如 `Q2A_LISTEN=0.0.0.0:7864`、`Q2A_API_KEY=sk-…`。

## 发版

版本号是上海时间 `v2026.10.19.0930`（日期不补零，时分不补零/两位）；标签、Release 与二进制
版本串同一个号。本地构建显示 `dev`，CI 用 `-X main.appVersion=<tag>` 注入；推 `v*` 标签才会
创建 Release 并推送镜像。

## 安全与合规

- **非官方、未获授权**：本文与上游服务商无任何关系，仅用于**本人授权账号**在本机 / 私有环境下的协议互操作。
- **凭证**：`auths/*.json` 以 0600 明文保存，目录不得进入备份、同步盘或共享范围；仓库不含任何账号、token 或密钥。
- **鉴权**：`api_key`（或 `api_keys`）为空时 `/v1/*` 与 `/panel/api/*` 不鉴权，仅适合 `127.0.0.1` 或可信内网；公网部署必须设密钥并前置 HTTPS 反代与访问限流。
- **合规边界**：不得用于搭建对外中转站、共享他人账号、批量注册小号或售卖额度；作者不承担使用后果。

## 已知限制

- **国内版签到**需要设备风控身份（UMID 那一套）。本仓未移植：国内号会拿到空活动列表并
  按「今日无活动」跳过，不会伪装成功。
- **积分单价不内置**：Qoder 没有稳定可引用的积分价目，面板「积分≈」显示 `—`，不做估算
  （同名模型多，错配单价会静默给出错误金额）。
- **凭证明文落盘**（`auths/*.json`，0600）：宿主机目录本身也要收紧。
- **同一批账号不要同时挂到多个网关**（如 agent2api 与本仓）：两边各自续期 refresh token
  会互相顶掉。
- 上游接口随时可能变（COSY 版本号、信封结构、模型名）；出问题先比对 `internal/upstream/`
  的协议常量与参考实现。

## 开发

```bash
go vet ./...
go test ./...        # COSY 已知向量、凭证解析、SSE 信封、端到端假上游（流式/非流式/工具调用/签到）
```

没有真账号时联调全链路：`python3 scripts/qoder_stub.py 7899` 起假上游，另起一个实例把
`upstream.gateway_host`/`gateway_host_cn` 指向 `http://127.0.0.1:7899/`、放一份假凭证进
`auths/qoder-<uid>.json`，即可验证签名→组包→SSE 转换（桩会拒绝没有 COSY 头的请求）。

## 血统与致谢

- 架构与交互基线：[`linguo2625469/workbuddy2api-panel`](https://github.com/linguo2625469/workbuddy2api-panel)（2026-10-21 停止维护）。
- 协议事实参考：`bincooo/qoder2api`、`shuishuipingan/qoder2api-hub`、`nullstacks/qoder2api`、`swysgh/qoder2api`（只读参考，未直接合并其代码）。
- 感谢上述项目的作者与维护者，把 COSY 签名与信封结构摊开成可验证的公开实现。

## License

MIT，见 [LICENSE](LICENSE)。
