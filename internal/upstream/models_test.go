package upstream

import (
	"encoding/json"
	"testing"

	"github.com/misakano7545/qoder2api-panel/internal/auth"
)

// TestParseCatalogLimitedFree 钉住限免判据：上游的限时免费就是**把价倍率压到 0**
// （官方口径：Qwen3.8-Flash 0.1× → 0.0×、Efficient 0.3× → 0.0×），没有单独的促销字段。
// 所以 price_factor=0 必须落成 Free=true；没给 price_factor 与给了 0 是两件事，
// 前者只表示「上游没说多少钱」，不能顺手标成限免。
func TestParseCatalogLimitedFree(t *testing.T) {
	raw := `{"chat":[
		{"key":"qfmodel","display_name":"Qwen3.8-Flash","enable":true,"price_factor":0},
		{"key":"qmodel_38max","display_name":"Qwen3.8-Max","enable":true,"price_factor":0.5},
		{"key":"dfmodel","display_name":"DeepSeek-Flash","enable":true}
	]}`
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("目录不是 JSON: %v", err)
	}
	got := map[string]ModelInfo{}
	for _, m := range parseCatalog(payload, auth.RealmCN) {
		got[m.ID] = m
	}
	if len(got) != 3 {
		t.Fatalf("解析出 %d 条，want 3: %+v", len(got), got)
	}
	if free := got["Qwen3.8-Flash"]; !free.Free || free.Credits != "x0 credits" {
		t.Errorf("price_factor=0 应判限免：free=%v credits=%q", free.Free, free.Credits)
	}
	if discounted := got["Qwen3.8-Max"]; discounted.Free || discounted.Credits != "x0.5 credits" {
		t.Errorf("0.5× 不是限免：free=%v credits=%q", discounted.Free, discounted.Credits)
	}
	if unknown := got["DeepSeek-Flash"]; unknown.Free || unknown.Credits != "" {
		t.Errorf("上游没给 price_factor 不该判成限免：free=%v credits=%q", unknown.Free, unknown.Credits)
	}
}

func parseOne(t *testing.T, item map[string]any) ModelInfo {
	t.Helper()
	// 只关心能力字段的用例不必每次都写 key/display_name；缺了它会被解析器当成
	// 无效条目丢掉，那样断言会变成「解析出 0 条」，测不到想测的东西。
	if _, ok := item["key"]; !ok {
		item["key"] = "fixture"
	}
	if _, ok := item["display_name"]; !ok {
		item["display_name"] = "Fixture"
	}
	got := parseCatalog(map[string]any{"chat": []any{item}}, auth.RealmCN)
	if len(got) != 1 {
		t.Fatalf("应解析出 1 条，得到 %d", len(got))
	}
	return got[0]
}

// TestParseCatalogExplicitFree 真机目录里限免是**显式下发**的：
// dfmodel 给 price_factor=0 + is_free + original_price_factor=0.1。
// is_free 要认（比从 0 反推更权威），原价要带出来给面板划掉——
// 早先注释写「原倍率上游不下发」是错的，这里把它钉住。
func TestParseCatalogExplicitFree(t *testing.T) {
	explicit := parseOne(t, map[string]any{
		"key": "dfmodel", "display_name": "DeepSeek-Flash", "enable": true,
		"is_reasoning": false, "price_factor": 0,
		"is_free": true, "original_price_factor": 0.1,
	})
	if !explicit.Free || explicit.Credits != "x0 credits" {
		t.Errorf("is_free 应判限免：free=%v credits=%q", explicit.Free, explicit.Credits)
	}
	if explicit.OriginalFactor != "0.1" {
		t.Errorf("original_price_factor 应带出（面板要划掉它）：%q", explicit.OriginalFactor)
	}
	// is_free 但倍率没归零：同样按限免标（显式信号优先于价格推断）。
	explicit2 := parseOne(t, map[string]any{
		"key": "qfmodel", "display_name": "Qwen3.8-Flash", "enable": true,
		"price_factor": 0.1, "is_free": true,
	})
	if !explicit2.Free {
		t.Errorf("显式 is_free 优先于倍率推断：%+v", explicit2)
	}
}

// TestThinkingEfforts 钉住思考档位解析：官方 thinking_config.enabled.efforts 有
// 对象 / 数组 / 字符串三种形态，必须都认；档位有白名单（无 minimal）；
// available_reasoning_effort 是 thinking_config 对象而不是档位表；
// supports_disabled 蛇形驼峰都认；default_effort 不在白名单就当没声明。
func TestThinkingEfforts(t *testing.T) {
	cases := []struct {
		name string
		item map[string]any
		want []string
		def  string
		dis  bool
	}{
		{
			name: "对象形态",
			item: map[string]any{"thinking_config": map[string]any{
				"enabled": map[string]any{"efforts": map[string]any{
					"low": map[string]any{}, "medium": map[string]any{}, "xhigh": map[string]any{}}}}},
			want: []string{"low", "medium", "xhigh"},
		},
		{
			name: "数组形态",
			item: map[string]any{"thinking_config": map[string]any{
				"enabled": map[string]any{"efforts": []any{"medium", "low"}}}},
			want: []string{"low", "medium"},
		},
		{
			name: "字符串形态",
			item: map[string]any{"thinking_config": map[string]any{
				"enabled": map[string]any{"efforts": "high, low  MAX"}}},
			want: []string{"low", "high", "max"},
		},
		{
			name: "白名单外丢弃且去重",
			item: map[string]any{"thinking_config": map[string]any{
				"enabled": map[string]any{"efforts": []any{"minimal", "low", "LOW", "turbo"}}}},
			want: []string{"low"},
		},
		{
			name: "available_reasoning_effort 兜底",
			item: map[string]any{"available_reasoning_effort": map[string]any{
				"enabled": map[string]any{"efforts": []any{"none", "high"}}}},
			want: []string{"none", "high"},
		},
		{
			name: "thinking_config 优先于兜底",
			item: map[string]any{
				"thinking_config":            map[string]any{"enabled": map[string]any{"efforts": []any{"low"}}},
				"available_reasoning_effort": map[string]any{"enabled": map[string]any{"efforts": []any{"max"}}}},
			want: []string{"low"},
		},
		{
			name: "默认档位与可关（蛇形）",
			item: map[string]any{
				"thinking_config":   map[string]any{"enabled": map[string]any{"efforts": []any{"low", "high"}}, "disabled": map[string]any{}},
				"default_effort":    "high",
				"supports_disabled": true},
			want: []string{"low", "high"}, def: "high", dis: true,
		},
		{
			name: "可关（驼峰）与非法默认档位",
			item: map[string]any{
				"thinking_config":  map[string]any{"enabled": map[string]any{"efforts": []any{"medium"}}},
				"supportsDisabled": true,
				"defaultEffort":    "ludicrous"},
			want: []string{"medium"}, dis: true,
		},
	}
	for _, c := range cases {
		model := parseOne(t, c.item)
		if len(model.Efforts) != len(c.want) {
			t.Fatalf("%s：档位 = %v，want %v", c.name, model.Efforts, c.want)
		}
		for i := range c.want {
			if model.Efforts[i] != c.want[i] {
				t.Fatalf("%s：档位 = %v，want %v", c.name, model.Efforts, c.want)
			}
		}
		if model.DefaultEffort != c.def {
			t.Errorf("%s：默认档位 = %q，want %q", c.name, model.DefaultEffort, c.def)
		}
		if model.SupportsDisabled != c.dis {
			t.Errorf("%s：supports_disabled = %v，want %v", c.name, model.SupportsDisabled, c.dis)
		}
	}
}

// TestReasoningFlagIgnoresIsReasoning 钉住 C：is_reasoning=false 只说明「不是推理
// 模型」，不代表不能思考。真机实测 CN dfmodel 的 is_reasoning 是 false，但
// reasoning_effort=low/max 会真的产生思考（只有 none 才是 0 字符）。
// 所以有档位就必须仍算「能思考」；谁把它按 is_reasoning 改回去，这条会红。
func TestReasoningFlagIgnoresIsReasoning(t *testing.T) {
	model := parseOne(t, map[string]any{
		"key": "dfmodel", "display_name": "DeepSeek-Flash",
		"is_reasoning": false, "enable": true, "price_factor": 0.1,
		"thinking_config": map[string]any{"enabled": map[string]any{"efforts": []any{"none", "low", "max"}}},
	})
	if !model.Reasoning {
		t.Errorf("声明了档位就应算能思考：%+v", model)
	}
	if len(model.Efforts) != 3 {
		t.Errorf("档位不该被 is_reasoning 抹掉：%v", model.Efforts)
	}
	// 反过来：两个信号都没有时不能凭空说能思考。
	plain := parseOne(t, map[string]any{"key": "kmodel", "display_name": "Kimi-K3", "enable": true})
	if plain.Reasoning || len(plain.Efforts) != 0 {
		t.Errorf("无任何思考声明时不该判成推理模型：%+v", plain)
	}
}

// TestParseCatalogKeepsPayloadKeys 是一条「解析成功 ≠ 透出成功」的守卫：
// 上游字段解析进 ModelInfo 之后必须真的出现在 JSON 里——早先 Free 解析对了但
// 没写进输出，面板看不到限免，而只测解析的单测会全绿。
func TestParseCatalogKeepsPayloadKeys(t *testing.T) {
	model := parseOne(t, map[string]any{
		"key": "qfmodel", "display_name": "Qwen3.8-Flash", "enable": true,
		"price_factor": 0, "is_free": true, "original_price_factor": 0.1,
		"default_effort": "low", "supports_disabled": true,
		"thinking_config": map[string]any{"enabled": map[string]any{"efforts": []any{"low"}}},
		"promotion":       map[string]any{"active": true, "window_start": "22:00", "window_end": "08:00"},
	})
	raw, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"free", "original_factor", "default_effort", "supports_disabled", "promo_window", "promo_active"} {
		if _, ok := out[key]; !ok {
			t.Errorf("ModelInfo 输出缺 %q：%s", key, raw)
		}
	}
}
