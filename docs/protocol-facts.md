# 协议事实与证据等级

这份文档收「我们对上游（Qoder）到底知道什么、凭什么知道」，因为代码注释记不住
证据强弱：两个看起来一样的结论，一个来自官网文档、一个来自兄弟实现的猜测，改起来
后果完全不同。协议事实散落在几十个文件里时，下一个人几乎必然会按最方便的那条改。

## 证据等级

| 级 | 含义 | 能怎么用 |
|---|---|---|
| **A** | 官方客户端产物 / 真机抓包，逐字段对账过 | 可以直接依赖，改动必须给出新的 A 级证据 |
| **B** | 报文语义或官方文档明确写出 | 可以依赖，但要在用例里写清推导链 |
| **C** | 兄弟实现（qoder2api*、dsh-providers-hub 等）的结论 | 只作线索，落地前尽量升到 A/B |

已有的 A 级外部参考：
- 官方文档 `docs.qoder.com`（模型档位与积分倍率表、限免活动说明）
- `iJetLi/dsh-providers-hub` 的 `docs/agent-notes/providers-qoder.md`
  （2026-10-20 真机对账 CN/Intl 两站 `catalog-v6` 与官方客户端产物）

## 模型目录（`/algo/api/v2/model/list`）

| 事实 | 级 | 守卫 |
|---|---|---|
| `price_factor` 是**当前生效价**；`is_free` + `original_price_factor` 显式标出限免与原价 | A | `TestParseCatalogExplicitFree` |
| 限免判据 = `is_free` 为真 **或** `price_factor == 0`；「上游没给 price_factor」不等于限免 | A/B | `TestParseCatalogLimitedFree` |
| `promotion` 块是**限时促销/错峰**，不是限免；`before_promotion_price_factor × discount_factor` **不是价格**，官方客户端从不显示 | A | `TestPromotionPricing` |
| `thinking_config.enabled.efforts` 有对象 / 数组 / 字符串三种形态 | A | `TestThinkingEfforts` |
| 档位白名单 `none/low/medium/high/xhigh/max`，**没有 `minimal`** | A | `TestThinkingEfforts` |
| `default_effort` 在顶层；`supports_disabled` / `supportsDisabled` / `thinking_config.disabled` 三条都认 | A | `TestThinkingEfforts` |
| `available_reasoning_effort` 是**一个 thinking_config 对象**，不是档位表；只在条目自身没档位时兜底 | A | `TestThinkingEfforts` |
| `is_reasoning=false` **不代表**不能思考：CN `dfmodel` 跑 `reasoning_effort=low/max` 会真的思考，`none` 才是 0 字符 | A | `TestReasoningFlagIgnoresIsReasoning` |
| 目录条数受「身份 + 版本」闸门控制：`cli/5@1.1.49/1.1.69` 14 条、`cli/5@1.1.70` 15 条、`app/10@1.1.69` 15 条 | A | 面板「模型」页标题旁显示当前身份，见下 |
| 目录**按账号**下发（同一 provider 下免费号可能只给 2 条） | A | `TestModelsAvailability`（池内按地区统计可用号） |

### 身份与版本

- COSY 版本同时参与**签名**与**目录闸门**，所以只有一个正确的改法：
  `config.json → upstream.client_version`（`SetIdentity` 的第二参）。
  **不要**改 `cosy.GatewayVersion` 常量。
- 实际生效值在面板「模型」页标题旁（`身份 <client_type>/<cosy_version>`）与
  `/panel/api/models` 的 `identity` 字段里，模型少一条时先看它。
- `client_type` 目前固定 10（桌面 App 形态）；改成 5（CLI）会连带影响目录与
  风控行为，属于未验证区间。

## 计价与额度

| 事实 | 级 | 守卫 |
|---|---|---|
| 额度/套餐类业务码：`110`、`112` 及同族 `113~119`、`122` | A/B | `TestClassifyBillingCodes` |
| 结构字段/文案判断：`pricingUrl`、`insufficient`、`no_quota`、`subscription`、`upgrade` | B | `TestClassifyBillingCodes` |
| **码值表与文案判断缺一即复发**：上游换码值时只认文案的实现会静默当普通 4xx（不换号、不冷却） | A | 同上（两条腿各有用例） |
| `10605` 是排队，不是没额度；`502/504` 是网关超时，不罚号 | A | `TestClassifyBillingCodes` |
| 国际版不是「无限免费」：Free plan 走美元计量（实测 `basic_usage_limit=1`） | A | 面板额度口径 `credit_unit` |

## 选号与冷却

| 事实 | 级 | 守卫 |
|---|---|---|
| 「余额未查到」（`credits=0`）与「无到期信息」（`expire=0`）都**不是**不可用，只影响临期加成 | C→B | `TestPickWithoutCreditsOrExpiry` |
| 「不可用」的唯一判据是 `healthy()`（禁用 / 冷却） | B | `TestPickExcludesDisabledAndCooling` |
| 冷却标记只是**一次 429/额度的快照**，服务端常提前放行；所以诊断（零网络）与重测（真请求）必须分开 | A | `TestOverviewDiagnosis`、`TestAccountProbeClearsCooldown` |
| 重测必须按 uid 取凭据（不自动选号），且停用/冷却中的号也要能测 | A | `TestAccountProbeClearsCooldown` |

## 维护约定

1. 新增一条协议事实 → 在对应表格加一行，并写下守卫用例名（没有守卫就等于没记录）。
2. 改已有结论 → 必须附新的 A 级证据（抓包字段、官方文档链接或客户端产物），
   并在提交信息里写清「旧结论为什么错」。
3. 只写「观测到了什么」与「据此怎么判」，不写推测的意图。
4. 兄弟实现能省探索时间，但它们的结论只是 **C 级**线索。
