package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/misakano7545/qoder2api-panel/internal/auth"
)

func testAccount() *auth.Auth {
	return &auth.Auth{
		AccessToken:  "dt-1",
		RefreshToken: auth.PackRefresh("drt-1", "u-1", "m-1"),
		ExpiresAt:    time.Now().Add(48 * time.Hour).UnixMilli(),
		RealmID:      auth.RealmGlobal,
		MachineID:    "m-1",
		UID:          "u-1",
		Nickname:     "测试",
	}
}

func TestBuildUpstreamBody(t *testing.T) {
	model := ModelInfo{ID: "Qwen3.8-Max", UpstreamKey: "qmodel_38max", Reasoning: true,
		Efforts: []string{"low", "medium", "xhigh"}, VL: true}
	in := map[string]any{
		"model": "Qwen3.8-Max",
		"messages": []any{
			map[string]any{"role": "system", "content": "你是助手"},
			map[string]any{"role": "user", "content": "写个函数"},
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
				map[string]any{"id": "c1", "type": "function", "function": map[string]any{
					"name": "run", "arguments": map[string]any{"x": 1},
				}},
			}},
			map[string]any{"role": "tool", "tool_call_id": "c1", "content": "ok"},
		},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{
			"name": "run", "description": "跑", "parameters": map[string]any{"type": "object"},
		}}},
		"reasoning_effort": "high",
		"max_tokens":       999999,
		"session_id":       "sess-1",
	}
	body := buildUpstreamBody(in, model, "u-1")

	if body["session_type"] != "app" || body["agent_id"] != "agent_common" {
		t.Errorf("信封字段不对: %v", body)
	}
	if body["stream"] != true {
		t.Errorf("上游必须走流式")
	}
	params := body["parameters"].(map[string]any)
	if params["enable_thinking"] != true {
		t.Errorf("thinking 没开: %v", params)
	}
	if params["reasoning_effort"] != "xhigh" {
		t.Errorf("high 应映射到 xhigh，得到 %v", params["reasoning_effort"])
	}
	if params["max_tokens"].(int64) != MaxOutputTokens {
		t.Errorf("max_tokens 应被钳到 %d，得到 %v", MaxOutputTokens, params["max_tokens"])
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("消息数 = %d", len(msgs))
	}
	assistant := msgs[2].(map[string]any)
	if assistant["content"] != " " {
		t.Errorf("只有工具调用的 assistant 应补非空 content，得到 %v", assistant["content"])
	}
	call := assistant["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if call["arguments"] != `{"x":1}` {
		t.Errorf("对象型 arguments 应序列化成字符串，得到 %v", call["arguments"])
	}
	cfg := body["model_config"].(map[string]any)
	if _, ok := cfg["thinking_config"]; ok {
		t.Errorf("model_config 不该带 thinking_config（会覆盖 enable_thinking）")
	}
	if cfg["key"] != "qmodel_38max" || cfg["is_reasoning"] != true {
		t.Errorf("model_config = %v", cfg)
	}
	sid := body["session_id"].(string)
	if !strings.HasSuffix(sid, "-sess-1") {
		t.Errorf("带 seed 的 session_id 应带上它: %q", sid)
	}
}

func TestParseSSELine(t *testing.T) {
	ok := parseSSELine(`{"statusCodeValue":200,"body":"{\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}"}`)
	if ok.Kind != sseChunk || ok.Chunk == nil {
		t.Fatalf("字符串 body 应拆成 chunk: %+v", ok)
	}
	obj := parseSSELine(`{"statusCodeValue":200,"body":{"choices":[]}}`)
	if obj.Kind != sseChunk {
		t.Fatalf("对象 body 应拆成 chunk: %+v", obj)
	}
	done := parseSSELine("[DONE]")
	if done.Kind != sseDone {
		t.Fatalf("[DONE] 应识别为结束: %+v", done)
	}
	quota := parseSSELine(`{"statusCodeValue":200,"body":"{\"code\":\"110\",\"message\":\"no quota\"}"}`)
	if quota.Kind != sseChunk {
		t.Fatalf("200 信封里的业务错误由分类层处理: %+v", quota)
	}
	bad := parseSSELine(`{"statusCodeValue":429,"body":"{\"code\":\"110\"}"}`)
	if bad.Kind != sseError || bad.Class != ErrPlanLimit {
		t.Fatalf("非 200 信封应报错并分类: %+v", bad)
	}
}

func sseStream(chunks ...string) string {
	var b strings.Builder
	for _, chunk := range chunks {
		envelope, _ := json.Marshal(map[string]any{"statusCodeValue": 200, "body": chunk})
		b.WriteString("data: " + string(envelope) + "\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func TestAggregate(t *testing.T) {
	stream := sseStream(
		`{"id":"c1","model":"Qwen3.8-Max","choices":[{"index":0,"delta":{"reasoning_content":"<thinking>想</thinking>正文"}}]}`,
		`{"id":"c1","model":"Qwen3.8-Max","choices":[{"index":0,"delta":{"content":"你好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
	)
	out, err := Aggregate(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("Aggregate 出错: %v", err)
	}
	choice := out["choices"].([]any)[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	if msg["content"] != "你好" {
		t.Errorf("content = %v", msg["content"])
	}
	if msg["reasoning_content"] != "想正文" {
		t.Errorf("思考标签应被剥掉: %v", msg["reasoning_content"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish = %v", choice["finish_reason"])
	}
	if out["usage"].(map[string]any)["total_tokens"] == nil {
		t.Errorf("缺 usage")
	}
}

func TestAggregateEmptyStream(t *testing.T) {
	_, err := Aggregate(strings.NewReader("data: [DONE]\n\n"))
	if err != ErrEmptyStream {
		t.Fatalf("空流应返回 ErrEmptyStream，得到 %v", err)
	}
}

func TestStreamWritesFrames(t *testing.T) {
	stream := sseStream(
		`{"id":"c1","model":"Qwen3.8-Max","choices":[{"index":0,"delta":{"content":"你"}}]}`,
		`{"id":"c1","model":"Qwen3.8-Max","choices":[{"index":0,"delta":{"content":"好"},"finish_reason":"stop"}]}`,
	)
	rec := httptest.NewRecorder()
	usage, err := StreamWithError(rec, strings.NewReader(stream), nil)
	if err != nil {
		t.Fatalf("StreamWithError 出错: %v", err)
	}
	if usage != nil {
		t.Errorf("没有 usage 时不该编一个: %v", usage)
	}
	out := rec.Body.String()
	for _, want := range []string{`"role":"assistant"`, `"content":"你"`, `"content":"好"`, `"finish_reason":"stop"`, "data: [DONE]"} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %s：%s", want, out)
		}
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
}

// serviceAvailable:false 埋在两层转义 JSON 里，也要认出来（等也没用，要快失败）。
func TestQueueUnavailableParse(t *testing.T) {
	raw := `{"code":"403","message":"{\"code\":\"10605\",\"message\":\"{\\\"isQueued\\\":true,\\\"serviceAvailable\\\":false}"}"}`
	if !queueUnavailable(raw) {
		t.Error("serviceAvailable:false 没认出来")
	}
	if queueUnavailable(`{"serviceAvailable":true}`) {
		t.Error("serviceAvailable:true 被误判成不可服务")
	}
	if got := retryAfterOf(raw); got != 0 {
		t.Errorf("这次样本没有 retryAfterSeconds，应为 0，得到 %d", got)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   ErrKind
	}{
		{401, "", ErrSessionDead},
		{429, "", ErrSoftRate},
		{503, "", ErrServer},
		{404, "", ErrNotFound},
		{400, `{"code":"110"}`, ErrPlanLimit},
		{400, `{"code":"10605"}`, ErrBusy},
		{400, "unknown field", ErrClient},
		{200, `{"pricingUrl":"https://qoder.com/pricing"}`, ErrPlanLimit},
	}
	for _, tc := range cases {
		if got := Classify(tc.status, tc.body); got != tc.want {
			t.Errorf("Classify(%d, %q) = %v, want %v", tc.status, tc.body, got, tc.want)
		}
	}
}

func TestResolveModel(t *testing.T) {
	c := New()
	for _, name := range []string{"Qwen3.8-Max", "qwen3.8-max", "Qwen 3.8 Max", "qmodel_38max"} {
		mi, ok := c.ResolveModel(name, auth.RealmGlobal)
		if !ok || mi.ID != "Qwen3.8-Max" {
			t.Errorf("ResolveModel(%q) = %+v, %v", name, mi, ok)
		}
	}
	if _, ok := c.ResolveModel("不存在的模型", auth.RealmGlobal); ok {
		t.Errorf("未知模型不该解析成功")
	}
}

// fakeUpstream 覆盖 openapi 一族的接口。
func fakeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/jobToken/exchange":
			_, _ = io.WriteString(w, `{"token":"jt-new","refresh_token":"jrt-new","expires_at":`+
				`4102444800000}`)
		case "/api/v1/deviceToken/refresh":
			_, _ = io.WriteString(w, `{"device_token":"dt-new","refresh_token":"drt-new","expires_at":4102444800000}`)
		case "/api/v1/userinfo":
			_, _ = io.WriteString(w, `{"id":"u-1","name":"新名字","email":"x@y.z"}`)
		case "/api/v2/quota/usage":
			_, _ = io.WriteString(w, `{"userQuota":{"remaining":120,"total":200,"used":80,"unit":"credits"},
				"addOnQuota":{"remaining":30,"total":50,"used":20},"expiresAt":1900000000000}`)
		case "/api/v2/user/plan":
			_, _ = io.WriteString(w, `{"plan_tier_name":"Pro"}`)
		case "/sash/api/v1/me/campaigns":
			if r.Header.Get("cosy-clienttype") != "10" {
				t.Errorf("签到接口必须带 cosy-clienttype: 10")
			}
			_, _ = io.WriteString(w, `{"campaigns":[{"campaignId":"c-1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMABLE"}]}`)
		case "/sash/api/v1/me/campaigns/c-1/claim":
			_, _ = io.WriteString(w, `{"status":"CLAIMED","benefit":{"amount":100}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRefreshPAT(t *testing.T) {
	srv := fakeUpstream(t)
	c := New()
	c.SetAPIHosts(srv.URL, srv.URL)
	a := &auth.Auth{RefreshToken: "pat|pt-1", RealmID: auth.RealmGlobal, UID: "u-1", MachineID: "m-1"}
	if err := c.RefreshToken(a); err != nil {
		t.Fatalf("RefreshToken 出错: %v", err)
	}
	if a.JWT() != "jt-new" {
		t.Errorf("access token = %q", a.JWT())
	}
	if pat, ok := a.PAT(); !ok || pat != "pt-1" {
		t.Errorf("PAT 应保留在刷新串里: %q", a.RefreshTokenValue())
	}
	if !strings.HasSuffix(a.RefreshTokenValue(), "|u-1|m-1") {
		t.Errorf("刷新串后缀应带 uid/设备: %q", a.RefreshTokenValue())
	}
}

func TestRefreshDevice(t *testing.T) {
	srv := fakeUpstream(t)
	c := New()
	c.SetAPIHosts(srv.URL, srv.URL)
	a := testAccount()
	a.ExpiresAt = 0 // 强制续期
	refreshed, err := c.RefreshTokenIfNeeded(a, time.Minute)
	if err != nil {
		t.Fatalf("RefreshTokenIfNeeded 出错: %v", err)
	}
	if !refreshed || a.JWT() != "dt-new" {
		t.Fatalf("续期没生效: refreshed=%v token=%q", refreshed, a.JWT())
	}
}

func TestQuotaAndCampaigns(t *testing.T) {
	srv := fakeUpstream(t)
	c := New()
	c.SetAPIHosts(srv.URL, srv.URL)
	a := testAccount()

	u, err := c.UserEntUsageDetail(a)
	if err != nil {
		t.Fatalf("UserEntUsageDetail 出错: %v", err)
	}
	if u.Remain != 150 || u.Total != 250 {
		t.Errorf("额度归一错误: %+v", u)
	}
	if u.Expire != 1900000000 {
		t.Errorf("到期秒 = %d", u.Expire)
	}

	packs, _, err := c.BalancePackages(a)
	if err != nil {
		t.Fatalf("BalancePackages 出错: %v", err)
	}
	if len(packs) != 2 || packs[0].Name != "Pro" || packs[0].Size != 200 {
		t.Errorf("积分构成错误: %+v", packs)
	}

	checked, credits, enable, err := c.CheckinStatus(a)
	if err != nil {
		t.Fatalf("CheckinStatus 出错: %v", err)
	}
	if checked || !enable {
		t.Errorf("签到状态错误: checked=%v enable=%v", checked, enable)
	}
	if credits != 150 {
		t.Errorf("credits = %d", credits)
	}
	if err := c.CheckinClaim(a); err != nil {
		t.Errorf("CheckinClaim 出错: %v", err)
	}
}

func TestExchangePAT(t *testing.T) {
	srv := fakeUpstream(t)
	c := New()
	c.SetAPIHosts(srv.URL, srv.URL)
	a, err := c.ExchangePAT(context.Background(), "pt-1", auth.RealmGlobal, "m-9")
	if err != nil {
		t.Fatalf("ExchangePAT 出错: %v", err)
	}
	if a.UID != "u-1" || a.Nickname != "新名字" || a.MachineID != "m-9" {
		t.Errorf("账号字段错误: %+v", a)
	}
	if a.JWT() != "jt-new" {
		t.Errorf("token = %q", a.JWT())
	}
}
