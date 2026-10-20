package panel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/misakano7545/qoder2api-panel/internal/auth"
	"github.com/misakano7545/qoder2api-panel/internal/pool"
	"github.com/misakano7545/qoder2api-panel/internal/upstream"
)

// newTestPanel 组装一个带单账号、指向假上游的面板。
func newTestPanel(t *testing.T) (*Panel, *upstream.Client, *pool.Pool) {
	t.Helper()
	var polls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 重测只读首帧：给一个 200 信封 + 一段内容就算「上游接单了」。
		if strings.HasSuffix(r.URL.Path, "/agent_chat_generation") {
			w.Header().Set("Content-Type", "text/event-stream")
			chunk, _ := json.Marshal(map[string]any{"statusCodeValue": 200,
				"body": `{"choices":[{"delta":{"content":"pong"}}]}`})
			_, _ = io.WriteString(w, "data: "+string(chunk)+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		switch r.URL.Path {
		case "/algo/api/v2/model/list":
			_, _ = io.WriteString(w, `{"chat":[{"key":"qmodel_38max","display_name":"Qwen3.8-Max","is_reasoning":true,"enable":true}]}`)
		case "/api/v2/quota/usage":
			_, _ = io.WriteString(w, `{"userQuota":{"remaining":120,"total":200,"used":80},"expiresAt":1900000000000}`)
		case "/api/v2/user/plan":
			_, _ = io.WriteString(w, `{"plan_tier_name":"Pro"}`)
		case "/sash/api/v1/me/campaigns":
			_, _ = io.WriteString(w, `{"campaigns":[{"campaignId":"c-1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMED"}]}`)
		case "/api/v1/deviceToken/poll":
			// 第一次：用户还没授权（上游 202）；之后：授权完成。
			polls.Add(1)
			if polls.Load() < 2 {
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, `{}`)
				return
			}
			_, _ = io.WriteString(w, `{"token":"dt-dev","refresh_token":"drt-dev","user_id":"u-1","expires_at":4102444800000}`)
		case "/api/v1/deviceToken/refresh":
			_, _ = io.WriteString(w, `{"device_token":"dt-r","refresh_token":"drt-r","expires_at":4102444800000}`)
		case "/api/v1/jobToken/exchange":
			_, _ = io.WriteString(w, `{"token":"jt-x","refresh_token":"jrt-x","expires_at":4102444800000}`)
		case "/api/v1/userinfo":
			_, _ = io.WriteString(w, `{"id":"u-1","name":"测试号"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	up := upstream.New()
	up.SetAgentHosts(srv.URL, srv.URL)
	up.SetAPIHosts(srv.URL, srv.URL)

	p := pool.New("")
	p.Add(&auth.Auth{
		AccessToken: "dt-1", RefreshToken: auth.PackRefresh("drt-1", "u-1", "m-1"),
		ExpiresAt: time.Now().Add(48 * time.Hour).UnixMilli(), RealmID: auth.RealmGlobal,
		MachineID: "m-1", UID: "u-1", Nickname: "测试号",
	})
	h := &apiShim{up: up}
	pn := New(Config{
		Pool: p, Upstream: up, AuthDir: t.TempDir(), Version: "test",
		Models: h.models, Usage: nil,
	})
	return pn, up, p
}

// apiShim 把 /v1/models 的形状借给面板（面板只要一个 []map）。
type apiShim struct{ up *upstream.Client }

func (s *apiShim) models() []map[string]any {
	out := []map[string]any{}
	for _, mi := range s.up.Catalog() {
		out = append(out, map[string]any{"id": mi.Realm + ":" + mi.ID, "name": mi.Name})
	}
	return out
}

func call(t *testing.T, pn *Panel, method, path, body string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.RemoteAddr = "127.0.0.1:5555" // 空密钥 = 只信本机；测试请求要落在回环上
	rec := httptest.NewRecorder()
	pn.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func TestOverview(t *testing.T) {
	pn, _, _ := newTestPanel(t)
	code, body := call(t, pn, http.MethodGet, "/panel/api/overview", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body["total"].(float64) != 1 {
		t.Errorf("total = %v", body["total"])
	}
	rows := body["accounts"].([]any)
	if len(rows) != 1 {
		t.Fatalf("账号行数 = %d", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["machine_id"] != "m-1" {
		t.Errorf("machine_id 没有回显: %v", row["machine_id"])
	}
}

func TestPackages(t *testing.T) {
	pn, _, _ := newTestPanel(t)
	code, body := call(t, pn, http.MethodGet, "/panel/api/packages", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	list := body["accounts"].([]any)
	first := list[0].(map[string]any)
	if first["remain"].(float64) != 120 {
		t.Errorf("remain = %v", first["remain"])
	}
	packs := first["packages"].([]any)
	if len(packs) == 0 {
		t.Fatalf("积分构成为空")
	}
	if packs[0].(map[string]any)["name"] != "Pro" {
		t.Errorf("包名 = %v", packs[0].(map[string]any)["name"])
	}
}

func TestCheckinStatusEndpoint(t *testing.T) {
	pn, _, _ := newTestPanel(t)
	code, body := call(t, pn, http.MethodGet, "/panel/api/checkin_status", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body["checked"].(float64) != 1 {
		t.Errorf("checked = %v", body["checked"])
	}
}

func TestImportPAT(t *testing.T) {
	pn, _, p := newTestPanel(t)
	code, body := call(t, pn, http.MethodPost, "/panel/api/accounts/import", "pt-1")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, body)
	}
	if body["action"] != "imported" {
		t.Errorf("action = %v", body["action"])
	}
	if _, ok := p.Status("u-1"); !ok {
		t.Errorf("导入后账号应进池")
	}
}

// 自动收号：授权未完成时 done=false，完成那次直接入池（前端只轮询，不需要按钮）。
func TestDeviceLoginAutoPoll(t *testing.T) {
	pn, _, p := newTestPanel(t)
	code, body := call(t, pn, http.MethodPost, "/panel/api/login/start", `{"realm":"global"}`)
	if code != http.StatusOK {
		t.Fatalf("login/start status = %d", code)
	}
	id := fmt.Sprint(body["id"])
	if code, first := call(t, pn, http.MethodGet, "/panel/api/login/poll?id="+id, ""); code != http.StatusOK || first["done"] != false {
		t.Fatalf("第一次轮询应 done=false，得到 %d %v", code, first)
	}
	code, second := call(t, pn, http.MethodGet, "/panel/api/login/poll?id="+id, "")
	if code != http.StatusOK || second["done"] != true || second["uid"] != "u-1" {
		t.Fatalf("第二次轮询应收号，得到 %d %v", code, second)
	}
	if _, ok := p.Status("u-1"); !ok {
		t.Errorf("收号后账号应进池")
	}
	// 会话收号即删：再问一次是 410，前端据此停轮询（不会重复入池）。
	if code, _ := call(t, pn, http.MethodGet, "/panel/api/login/poll?id="+id, ""); code != http.StatusGone {
		t.Errorf("已收号的会话应 410，得到 %d", code)
	}
}

// 设备授权：start 给出链接与任务 id；未知/过期会话轮询要 410（前端据此停轮询）。
func TestDeviceLoginStartAndUnknownFinish(t *testing.T) {
	pn, _, _ := newTestPanel(t)
	code, body := call(t, pn, http.MethodPost, "/panel/api/login/start", `{"realm":"cn"}`)
	if code != http.StatusOK {
		t.Fatalf("login/start status = %d", code)
	}
	if body["id"] == "" || !strings.Contains(fmt.Sprint(body["url"]), "challenge_method=S256") {
		t.Errorf("授权链接形态不对: %v", body)
	}
	if code, _ := call(t, pn, http.MethodGet, "/panel/api/login/poll?id=nope", ""); code != http.StatusGone {
		t.Errorf("未知会话应 410，得到 %d", code)
	}
}

// refreshToken 直登：一条裸刷新令牌 → 续期 → userinfo → 落盘入池。
func TestLoginRefreshBareToken(t *testing.T) {
	pn, _, p := newTestPanel(t)
	code, body := call(t, pn, http.MethodPost, "/panel/api/login/refresh", `{"refresh_token":"drt-1","realm":"global"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, body)
	}
	if body["uid"] != "u-1" {
		t.Errorf("uid = %v", body["uid"])
	}
	if _, ok := p.Status("u-1"); !ok {
		t.Errorf("账号应进池")
	}
}

func TestLoginRefreshRejectsGarbage(t *testing.T) {
	pn, _, _ := newTestPanel(t)
	code, _ := call(t, pn, http.MethodPost, "/panel/api/login/refresh", `{"refresh_token":""}`)
	if code != http.StatusBadRequest {
		t.Errorf("空 refreshToken 应 400，得到 %d", code)
	}
}

// TestModelsAvailability 钉住 E：目录里的每条模型都要带「当前池里该地区有没有可用号」。
// 本用例的号在国际版，所以 global 条目 usable、cn 条目不可用——这正是用户要能一眼看出的。
func TestModelsAvailability(t *testing.T) {
	pn, up, p := newTestPanel(t)
	if _, err := up.RefreshModels(p.Peek(), true); err != nil {
		t.Fatalf("拉模型目录失败: %v", err)
	}
	code, body := call(t, pn, http.MethodGet, "/panel/api/models", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	avail := body["availability"].(map[string]any)
	if avail["global"].(float64) != 1 {
		t.Errorf("availability[global] = %v，want 1", avail["global"])
	}
	if _, ok := avail["cn"]; ok {
		t.Errorf("没有国内号时不该出现 cn 计数：%v", avail)
	}
	entries := body["data"].([]any)
	if len(entries) == 0 {
		t.Fatalf("模型列表为空")
	}
	for _, raw := range entries {
		entry := raw.(map[string]any)
		id := entry["id"].(string)
		want := strings.HasPrefix(id, "global:")
		if entry["usable"] != want {
			t.Errorf("%s usable = %v，want %v", id, entry["usable"], want)
		}
	}
	// 身份要能核对：目录条数受「身份 + 版本」闸门控制，模型少了先看这一项。
	identity := body["identity"].(map[string]any)
	if identity["cosy_version"] == "" || identity["client_type"] == "" {
		t.Errorf("identity 不完整：%v", identity)
	}
}

// TestOverviewDiagnosis 钉住 H1：首屏要直说「为什么接不了单」，而不是只给一个红点。
// 诊断是纯本地推断（零网络），所以冷却中的号一定会带一条原因。
func TestOverviewDiagnosis(t *testing.T) {
	pn, _, p := newTestPanel(t)
	p.Cooldown("u-1", pool.CoolPlan, time.Hour, "额度不足/套餐不支持")
	_, body := call(t, pn, http.MethodGet, "/panel/api/overview", "")
	row := body["accounts"].([]any)[0].(map[string]any)
	diag, _ := row["diagnosis"].([]any)
	if len(diag) == 0 {
		t.Fatalf("冷却中的号没有诊断结论：%v", row)
	}
	if !strings.Contains(diag[0].(string), "冷却中") || !strings.Contains(diag[0].(string), "额度不足") {
		t.Errorf("诊断结论没说清原因：%v", diag)
	}
}

// TestAccountProbeClearsCooldown 钉住 H2 的两个动作语义不同：
//   - 重测（probe）：真发一次请求，成功才清标记；
//   - 重置（clear-cooldown）：不发请求，直接清显示。
//
// 这里覆盖「重测成功 → 冷却被清掉」这条主路径。
func TestAccountProbeClearsCooldown(t *testing.T) {
	pn, up, p := newTestPanel(t)
	p.Cooldown("u-1", pool.CoolSoft, time.Hour, "soft rate limit")
	code, body := call(t, pn, http.MethodPost, "/panel/api/accounts/u-1/probe", "{}")
	if code != http.StatusOK {
		t.Fatalf("status = %d body=%v", code, body)
	}
	if body["ok"] != true {
		t.Fatalf("重测应通过：%v", body)
	}
	if body["cleared"] != true {
		t.Errorf("通过后应解除冷却：%v", body)
	}
	// 再来一次：这次本来就没标记，cleared 必须是 false（否则面板会喊「已解除冷却」，
	// 而用户根本没看到过冷却）。
	_, again := call(t, pn, http.MethodPost, "/panel/api/accounts/u-1/probe", "{}")
	if again["ok"] != true || again["cleared"] != false {
		t.Errorf("无标记时重测应 ok=true/cleared=false：%v", again)
	}
	st, _ := p.Status("u-1")
	if st.Cooling {
		t.Errorf("重测通过后仍在冷却：%+v", st)
	}
	// 重测必须用**该地区**的模型：拿国际号打国内模型会必然 4001，测了等于没测。
	realmModels := map[string]bool{}
	for _, mi := range up.Catalog() {
		if mi.Realm == auth.RealmGlobal {
			realmModels[mi.ID] = true
		}
	}
	if !realmModels[body["model"].(string)] {
		t.Errorf("重测模型 = %v，不在国际版目录里：%v", body["model"], realmModels)
	}
}
