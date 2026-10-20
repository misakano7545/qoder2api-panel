package upstream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ModelInfo 是模型目录的一条。
type ModelInfo struct {
	// ID 是给客户端用的名字：官方 display_name 去掉空格（客户端唯一需要填的值）。
	ID string `json:"id"`
	// Name 是官方 display_name（面板显示用，可能带空格）。
	Name string `json:"name"`
	// UpstreamKey 是发往上游的 model_config.key（如 qmodel_38max）。
	UpstreamKey   string   `json:"upstream_key"`
	Reasoning     bool     `json:"reasoning"`
	Efforts       []string `json:"efforts,omitempty"`
	ContextWindow int64    `json:"context_window"`
	MaxOutput     int64    `json:"max_output"`
	VL            bool     `json:"vl"`
	Enabled       bool     `json:"enabled"`
	Credits       string   `json:"credits,omitempty"`
	// Promo* 是官方 promotion 块（限时促销，与「限免」是两件事）。口径照官方
	// 客户端 yo0()：**当前价恒取 price_factor**，划线原价取 original_price_factor
	// （促销进行中优先 before_promotion_price_factor）。不做 before × discount
	// 的乘法——那个积是「谷时推算价」，官方客户端从不显示它，我们算出来就是在
	// 报一个上游没说的数。
	PromoWindow   string `json:"promo_window,omitempty"`
	PromoActive   bool   `json:"promo_active,omitempty"`
	PromoDiscount string `json:"promo_discount,omitempty"`
	// OriginalFactor 是划线原价（折扣前/促销前的倍率），与 Credits（当前价）成对
	// 出现。限免时上游给 price_factor=0 + is_free=1 + original_price_factor=0.1，
	// 面板据此把 0.00× 后面那个 0.10× 划掉——原价是上游下发的，不要自己推。
	OriginalFactor string `json:"original_factor,omitempty"`
	// Free 是「限免」：显式 is_free 为真，或 price_factor 被压到 0（两种都见过，
	// 官方口径就是把价倍率从 0.1× 降到 0.0×）。is_free 优先，缺它才反推。
	Free bool `json:"free,omitempty"`
	// DefaultEffort 是官方声明的默认思考档位（顶层 default_effort）。
	DefaultEffort string `json:"default_effort,omitempty"`
	// SupportsDisabled 表示可以显式关掉思考（thinking_config.disabled /
	// supports_disabled / supportsDisabled 任一出现）。它与 Reasoning 独立：
	// is_reasoning=false 只说明「不是推理模型」，不代表不能调档位。
	SupportsDisabled bool   `json:"supports_disabled,omitempty"`
	Realm            string `json:"realm,omitempty"`
	Source           string `json:"source,omitempty"`
}

// fallbackRow 是静态兜底目录的一行（官方目录拉不到时仍能出模型列表）。
type fallbackRow struct {
	id        string
	key       string
	reasoning bool
	efforts   []string
	vl        bool
	enabled   bool
	factor    string
}

var fallbackGlobal = []fallbackRow{
	{"Qwen3.8-Flash", "qfmodel", true, []string{"low", "medium", "xhigh"}, true, true, "0.1"},
	{"Qwen3.8-Max", "qmodel_38max", true, []string{"low", "medium", "xhigh"}, true, true, "0.5"},
	{"Auto", "auto", true, nil, true, false, "1"},
	{"Ultimate", "ultimate", true, nil, true, false, "1.6"},
	{"Performance", "performance", true, nil, true, false, "1.1"},
	{"Efficient", "efficient", false, nil, true, false, "0.3"},
	{"Sonus", "smodel", true, nil, true, false, "3.2"},
	{"Cantus", "cmodel", true, nil, true, false, "3.2"},
	{"Qwen3.7-Max", "qmodel_latest", true, nil, true, false, "0.5"},
	{"Qwen3.7-Plus", "qmodel", false, nil, true, false, "0.1"},
	{"Kimi-K3", "kmodel_latest", false, nil, true, false, "0.8"},
	{"Kimi-K2.8-Preview", "kmodel", false, nil, true, false, "0.3"},
	{"GLM-5.3", "gmodel", true, nil, true, false, "0.6"},
	{"GLM-5.3-Flash", "gfmodel", true, nil, true, false, "0.1"},
	{"DeepSeek-V4-Pro", "dmodel", true, nil, true, false, "0.8"},
	{"DeepSeek-Flash", "dfmodel", true, nil, true, false, "0.2"},
	{"MiniMax-M3", "mmodel", false, nil, true, false, "0.2"},
}

var fallbackCN = []fallbackRow{
	{"Qwen3.8-Flash", "qfmodel", true, []string{"low", "medium", "xhigh"}, true, true, "0.1"},
	{"Qwen3.8-Max", "qmodel_38max", true, []string{"low", "medium", "xhigh"}, true, true, "0.5"},
	{"Auto", "auto", true, nil, true, false, "1"},
	{"Qwen3.7-Max", "qmodel_latest", true, nil, true, false, "0.5"},
	{"Qwen3.7-Plus", "qmodel", true, nil, false, false, "0.1"},
	{"DeepSeek-V4-Pro", "dmodel", true, nil, false, false, "0.8"},
	{"DeepSeek-Flash", "dfmodel", false, nil, false, false, "0.2"},
	{"GLM-5.3", "gmodel", true, nil, true, false, "0.6"},
	{"GLM-5.3-Flash", "gfmodel", true, nil, true, false, "0.1"},
	{"Kimi-K3", "kmodel_latest", false, nil, true, false, "0.8"},
	{"Kimi-K2.8-Preview", "kmodel", true, nil, true, false, "0.3"},
	{"MiniMax-M3", "mmodel", false, nil, false, false, "0.2"},
}

func fallbackModels(realm string) []ModelInfo {
	rows := fallbackGlobal
	if ifCN(realm) {
		rows = fallbackCN
	}
	out := make([]ModelInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, makeModel(row.id, row.key, row.reasoning, row.efforts, row.vl, row.enabled, row.factor, realm))
	}
	return out
}

func makeModel(id, key string, reasoning bool, efforts []string, vl, enabled bool, factor, realm string) ModelInfo {
	if efforts == nil {
		efforts = []string{}
	}
	credits := ""
	if strings.TrimSpace(factor) != "" {
		credits = "x" + strings.TrimSpace(factor) + " credits"
	}
	return ModelInfo{
		ID:            id,
		Name:          id,
		UpstreamKey:   key,
		Reasoning:     reasoning,
		Efforts:       normalizeEfforts(efforts),
		ContextWindow: DefaultContextWindow,
		MaxOutput:     MaxOutputTokens,
		VL:            vl,
		Enabled:       enabled,
		Credits:       credits,
		Free:          factorIsZero(factor),
		Realm:         normalRealm(realm),
		Source:        "system",
	}
}

// catalogTTL 是官方目录的缓存时长（与上游客户端同一口径：10 分钟）。
const catalogTTL = 10 * time.Minute

// RefreshModels 拉一次官方目录并写进缓存；force=false 且未过 TTL 时直接返回条数。
func (c *Client) RefreshModels(a *authState, force bool) (int, error) {
	realm := a.Realm()
	c.catMu.RLock()
	cached := c.catalog[realm]
	fetchedAt := c.catAt[realm]
	c.catMu.RUnlock()
	if !force && len(cached) > 0 && time.Since(fetchedAt) < catalogTTL {
		return len(cached), nil
	}

	url := ModelListURL(c.gatewayBase(realm))
	// GET 也必须带与签名一致的 {} body，否则上游 403。
	encoded := encodeBody([]byte("{}"))
	headers, err := c.buildAuthHeaders(encoded, url, a)
	if err != nil {
		return 0, err
	}
	headers["Content-Type"] = "application/json"
	headers["X-Model-Source"] = "system"

	req, err := http.NewRequest(http.MethodGet, url, strings.NewReader(string(encoded)))
	if err != nil {
		return 0, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := c.do(req)
	if err != nil {
		return 0, err
	}
	data, err := payloadOf(resp, "模型目录请求")
	if err != nil {
		return 0, err
	}
	models := parseCatalog(data, realm)
	if len(models) == 0 {
		return 0, &Error{Kind: ErrServer, Status: 502, Msg: "上游返回的模型目录为空"}
	}
	c.catMu.Lock()
	c.catalog[realm] = models
	c.catAt[realm] = time.Now()
	c.catMu.Unlock()
	return len(models), nil
}

// FetchModels 返回某账号地区的模型目录（缓存优先，缓存空时用静态兜底）。
// 实时刷新走 RefreshModels：面板「刷新模型」按钮与定时任务用它。
func (c *Client) FetchModels(a *authState) ([]ModelInfo, error) {
	realm := a.Realm()
	c.catMu.RLock()
	models := c.catalog[realm]
	c.catMu.RUnlock()
	if len(models) > 0 {
		return models, nil
	}
	if _, err := c.RefreshModels(a, false); err != nil {
		return fallbackModels(realm), err
	}
	c.catMu.RLock()
	models = c.catalog[realm]
	c.catMu.RUnlock()
	if len(models) == 0 {
		return fallbackModels(realm), nil
	}
	return models, nil
}

// Catalog 返回两地区目录的并集（面板模型页用；每区内部按 id 去重）。
func (c *Client) Catalog() []ModelInfo {
	c.catMu.RLock()
	defer c.catMu.RUnlock()
	out := []ModelInfo{}
	for _, realm := range []string{RegionGlobal, RegionCN} {
		source := c.catalog[realm]
		if len(source) == 0 {
			source = fallbackModels(realm)
		}
		seen := map[string]bool{}
		for _, model := range source {
			key := strings.ToLower(model.ID)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			model.Realm = realm
			out = append(out, model)
		}
	}
	return out
}

// ResolveModel 把客户端模型名解析成上游条目：先账号地区，再另一地区，最后静态兜底。
func (c *Client) ResolveModel(modelID, realm string) (ModelInfo, bool) {
	wanted := normalizeModelName(modelID)
	if wanted == "" {
		return ModelInfo{}, false
	}
	realm = normalRealm(realm)
	other := RegionCN
	if realm == RegionCN {
		other = RegionGlobal
	}
	c.catMu.RLock()
	catGlobal, catCN := c.catalog[RegionGlobal], c.catalog[RegionCN]
	c.catMu.RUnlock()

	for _, candidate := range []struct {
		realm  string
		models []ModelInfo
	}{
		{realm, c.catalogOf(realm, catGlobal, catCN)},
		{other, c.catalogOf(other, catGlobal, catCN)},
		{realm, fallbackModels(realm)},
		{other, fallbackModels(other)},
	} {
		if model, ok := findModel(candidate.models, wanted); ok {
			model.Realm = candidate.realm
			return model, true
		}
	}
	return ModelInfo{}, false
}

func (c *Client) catalogOf(realm string, catGlobal, catCN []ModelInfo) []ModelInfo {
	models := catGlobal
	if ifCN(realm) {
		models = catCN
	}
	if len(models) == 0 {
		return fallbackModels(realm)
	}
	return models
}

// normalizeModelName 归一模型名：小写、去掉空格/下划线/连字符（客户端常把
// "Qwen3.8-Max" 写成 "qwen3.8-max"、"Qwen 3.8 Max" 或 "qmodel_38max"）。
func normalizeModelName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch r {
		case ' ', '\t', '_', '-':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func findModel(models []ModelInfo, wanted string) (ModelInfo, bool) {
	for _, pick := range []func(ModelInfo) string{
		func(m ModelInfo) string { return m.ID },
		func(m ModelInfo) string { return m.Name },
		func(m ModelInfo) string { return m.UpstreamKey },
	} {
		for _, model := range models {
			if normalizeModelName(pick(model)) == wanted {
				return model, true
			}
		}
	}
	return ModelInfo{}, false
}

// parseCatalog 解析官方目录：{"chat":[{"key","display_name","is_reasoning","is_vl",
// "enable","max_input_tokens","price_factor","is_free","original_price_factor",
// "thinking_config":{...},"available_reasoning_effort":{...},"promotion":{...},"source"}...]}。
func parseCatalog(payload map[string]any, realm string) []ModelInfo {
	if message := envelopeError(payload); message != "" {
		return nil
	}
	chat, ok := payload["chat"].([]any)
	if !ok {
		return nil
	}
	out := []ModelInfo{}
	seen := map[string]bool{}
	for _, raw := range chat {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := strings.TrimSpace(text(item, "key"))
		display := strings.TrimSpace(text(item, "display_name"))
		if key == "" || display == "" {
			continue
		}
		id := strings.Join(strings.Fields(display), "")
		if id == "" || seen[strings.ToLower(id)] {
			continue
		}
		seen[strings.ToLower(id)] = true
		factor := trimFactor(item["price_factor"])
		efforts, supportsDisabled, defaultEffort := thinkingOf(item)
		// 能力口径（真题机实测）：is_reasoning=false 但声明了档位的模型（CN 的
		// dfmodel）跑 reasoning_effort=low/max 会真的思考，只有 none 才是 0 字符。
		// 所以「能思考」是三个信号的或：is_reasoning / thinking_config / 有档位。
		// 不要把 is_reasoning 单独当判据，也不要为了看起来对把它改写成 true。
		reasoning := truthy(item["is_reasoning"]) || item["thinking_config"] != nil || len(efforts) > 0
		model := makeModel(id, key, reasoning, efforts, truthy(item["is_vl"]), truthy(item["enable"]), factor, realm)
		model.Name = display
		model.DefaultEffort = defaultEffort
		model.SupportsDisabled = supportsDisabled
		// 限免：显式 is_free 优先；没有这个字段时才拿 price_factor==0 反推。
		model.Free = truthy(item["is_free"]) || factorIsZero(factor)
		model.OriginalFactor = originalFactorOf(item)
		applyPromotion(&model, item["promotion"])
		model.Source = orDefault(text(item, "source"), "system")
		if context := int64Of(item["max_input_tokens"]); context > 0 {
			model.ContextWindow = context
		}
		out = append(out, model)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// originalFactorOf 取划线原价，口径照官方客户端 yo0()：
// 促销进行中 → before_promotion_price_factor ?? original_price_factor；
// 否则 → original_price_factor。两者都没有就留空（不猜原价）。
func originalFactorOf(item map[string]any) string {
	original := trimFactor(item["original_price_factor"])
	promo, _ := item["promotion"].(map[string]any)
	if promo == nil || !truthy(promo["active"]) {
		return original
	}
	if before := trimFactor(promo["before_promotion_price_factor"]); before != "" {
		return before
	}
	return original
}

// applyPromotion 只记录促销元信息（时段 / 是否进行中 / 折扣系数）。
//
// 特别注意**不要**把 before × discount 当价格用：官方客户端当前价恒取
// price_factor，那个积只是「谷时推算价」。早先版本在这里算 0.5×0.4=0.2 并显示
// 「谷 x0.2」，等于向上游没这么说的小时数报了一个价。
func applyPromotion(m *ModelInfo, raw any) {
	promo, ok := raw.(map[string]any)
	if !ok {
		return
	}
	if discount := trimFactor(promo["discount_factor"]); discount != "" {
		m.PromoDiscount = discount
	}
	m.PromoActive = truthy(promo["active"])
	start, end := text(promo, "window_start"), text(promo, "window_end")
	if start != "" && end != "" {
		m.PromoWindow = start + "-" + end
	}
}

// envelopeError 取上游信封里的业务错误文案（200 或缺失视为正常）。
func envelopeError(payload map[string]any) string {
	value := numberOrNil(payload["statusCodeValue"])
	if value == nil {
		value = numberOrNil(payload["code"])
	}
	if value == nil || int64(*value) == 200 {
		return ""
	}
	message := text(payload, "message", "body")
	return fmt.Sprintf("上游返回业务错误 %d: %s", int64(*value), truncate(message, 200))
}

// effortOrder 是官方认的思考档位（既是白名单也是展示顺序）。
// 注意**没有** minimal：客户端产物里的合法值只有这六个，多出来的键要丢掉——
// 否则面板会出现一个用户选了、上游却不认的档位。
var effortOrder = []string{"none", "low", "medium", "high", "xhigh", "max"}

// thinkingOf 解析思考能力：档位、能否关掉思考、默认档位。
//
// 官方 thinking_config.enabled.efforts 有**三种形态**（都实测过）：对象
// {"low":{},...}、数组 ["low",...]、逗号/空格分隔的字符串。只认对象形态的话，
// 上游一旦切到另外两种，面板就会静默变成「没有档位」——不报错、也没日志。
//
// available_reasoning_effort 本身是**一个 thinking_config 对象**（不是档位表），
// 只在条目自己既没档位也没 disabled 时用它兜底（两者同时有时以 thinking_config
// 为准，否则会把兜底值当成真实声明）。
func thinkingOf(item map[string]any) (efforts []string, supportsDisabled bool, defaultEffort string) {
	configs := []map[string]any{}
	if tc, ok := item["thinking_config"].(map[string]any); ok {
		configs = append(configs, tc)
	}
	if len(configs) == 0 {
		if tc, ok := item["available_reasoning_effort"].(map[string]any); ok {
			configs = append(configs, tc)
		}
	}
	raw := []string{}
	for _, tc := range configs {
		raw = append(raw, effortsOfConfig(tc)...)
		if _, ok := tc["disabled"]; ok {
			supportsDisabled = true
		}
	}
	// 蛇形与驼峰都认：官方客户端两个腿都读，只认一个就会漏掉另一种下发形态。
	if truthy(item["supports_disabled"]) || truthy(item["supportsDisabled"]) {
		supportsDisabled = true
	}
	efforts = normalizeEfforts(raw)
	// 默认档位是顶层 default_effort；空值或不在白名单里就当没声明（不猜）。
	defaultEffort = strings.ToLower(strings.TrimSpace(text(item, "default_effort", "defaultEffort")))
	if !isKnownEffort(defaultEffort) {
		defaultEffort = ""
	}
	return efforts, supportsDisabled, defaultEffort
}

// effortsOfConfig 从一个 thinking_config 对象里取档位（enabled 三种形态都认）。
func effortsOfConfig(tc map[string]any) []string {
	enabled, ok := tc["enabled"]
	if !ok {
		return nil
	}
	if field, ok := enabled.(map[string]any); ok {
		return stringList(field["efforts"])
	}
	return stringList(enabled)
}

// stringList 把「对象键集 / 数组 / 逗号或空格分隔的字符串」统一成字符串切片。
func stringList(value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		out := make([]string, 0, len(typed))
		for key := range typed {
			out = append(out, key)
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, raw := range typed {
			if s, ok := raw.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return typed
	case string:
		return strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == '|' || r == '/' || r == ' ' || r == '	' || r == '\n'
		})
	}
	return nil
}

// normalizeEfforts 去重、丢掉白名单外的值、按 effortOrder 排序（大小写不敏感）。
func normalizeEfforts(efforts []string) []string {
	if len(efforts) == 0 {
		return []string{}
	}
	seen := map[string]bool{}
	for _, raw := range efforts {
		for _, part := range stringList(raw) {
			if key := strings.ToLower(strings.TrimSpace(part)); isKnownEffort(key) {
				seen[key] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for _, key := range effortOrder {
		if seen[key] {
			out = append(out, key)
		}
	}
	return out
}

func isKnownEffort(effort string) bool {
	for _, key := range effortOrder {
		if key == effort {
			return true
		}
	}
	return false
}

func numberOrNil(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		return &typed
	case int:
		f := float64(typed)
		return &f
	case int64:
		f := float64(typed)
		return &f
	case json.Number:
		if f, err := typed.Float64(); err == nil {
			return &f
		}
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%g", &f); err == nil {
			return &f
		}
	}
	return nil
}

func int64Of(value any) int64 {
	if f := numberOrNil(value); f != nil {
		return int64(*f)
	}
	return 0
}

// trimFactor 把上游的价倍率字段规整成展示串。缺失或负数返回空串（= 上游没给），
// 0 会如实返回 "0"——「没给」与「给了 0」必须能区分开，前者不标限免。
func trimFactor(value any) string {
	number := numberOrNil(value)
	if number == nil || *number < 0 {
		return ""
	}
	return trimFloat(*number)
}

// factorIsZero 判断上游价倍率是否为 0（限免的唯一信号）。只认真的 0：空字符串
// （上游没给 price_factor）不算 —— 「没给」和「给了 0」是两件事，不能混。
func factorIsZero(factor string) bool {
	factor = strings.TrimSpace(factor)
	if factor == "" {
		return false
	}
	value, err := strconv.ParseFloat(factor, 64)
	return err == nil && value == 0
}

func trimFloat(value float64) string {
	rounded := float64(int64(value*100+0.5)) / 100
	if rounded == float64(int64(rounded)) {
		return fmt.Sprintf("%d", int64(rounded))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", rounded), "0"), ".")
}
