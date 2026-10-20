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
