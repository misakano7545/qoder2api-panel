# 版本发布

不跟上游 `1.x`。发版是打时间标签并推送，工作流看到 `v*` 才创建 Release。

版本号是上海时间，形如 `v2026.9.26.711`：

- `2026.9.26` 是日期，月和日不补零
- `711` 是 7:11，时不补零、分两位、不带秒
- 标签、Release、二进制用同一个号；二进制里不带 `v`，面板会自己加
- 不要打 `v1.x` 标签，也不要手改 `appVersion`

```bash
# 注意：本机（Windows / git-bash）没有 tzdata，`TZ=Asia/Shanghai date` 会**静默回落 UTC**，
# 打出来的标签整整早 8 小时。用显式加 8 小时，兼容有无 tzdata 两种环境：
VERSION=$(TZ=Asia/Shanghai date +%Y.%-m.%-d.%-H%M)
[ "$(TZ=UTC date +%H)" = "$(TZ=Asia/Shanghai date +%H)" ] && VERSION=$(date -u -d '+8 hours' +%Y.%-m.%-d.%-H%M)
git tag "v$VERSION"
git push origin "v$VERSION"
```

本地构建不注入，面板显示 `dev`。同一分钟再发才会撞号。

# 协议事实

上游（Qoder）的协议结论记在 [`docs/protocol-facts.md`](docs/protocol-facts.md)：
每条事实标证据等级（A 官方产物/抓包、B 文档或报文语义、C 兄弟实现推断），并写下
守它的用例名。改协议判据前先读那张表——那里写着「这条凭什么成立」和「谁在守它」。

