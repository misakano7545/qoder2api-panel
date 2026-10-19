package panel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/misakano7545/qoder2api-panel/internal/auth"
	"github.com/misakano7545/qoder2api-panel/internal/pool"
	"github.com/misakano7545/qoder2api-panel/internal/upstream"
)

// newTestPanel 组装一个带单账号、指向假上游的面板。
func newTestPanel(t *testing.T) (*Panel, *upstream.Client, *pool.Pool) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/algo/api/v2/model/list":
			_, _ = io.WriteString(w, `{"chat":[{"key":"qmodel_38max","display_name":"Qwen3.8-Max","is_reasoning":true,"enable":true}]}`)
		case "/api/v2/quota/usage":
			_, _ = io.WriteString(w, `{"userQuota":{"remaining":120,"total":200,"used":80},"expiresAt":1900000000000}`)
		case "/api/v2/user/plan":
			_, _ = io.WriteString(w, `{"plan_tier_name":"Pro"}`)
		case "/sash/api/v1/me/campaigns":
			_, _ = io.WriteString(w, `{"campaigns":[{"campaignId":"c-1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMED"}]}`)
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

func TestLoginRefreshRejectsGarbage(t *testing.T) {
	pn, _, _ := newTestPanel(t)
	code, _ := call(t, pn, http.MethodPost, "/panel/api/login/refresh", `{"refresh_token":""}`)
	if code != http.StatusBadRequest {
		t.Errorf("空 refreshToken 应 400，得到 %d", code)
	}
}
