package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// count_tokens 是本地估算（Qoder 没有计数接口）：正文越长估得越多，且不被结构字段带偏。
func TestCountTokens(t *testing.T) {
	h := newTestHandler(t, "http://127.0.0.1:1") // 无上游调用
	post := func(body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(body))
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var out struct {
			InputTokens int `json:"input_tokens"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("解响应失败: %v", err)
		}
		return out.InputTokens
	}

	small := post(`{"model":"Qwen3.8-Max","messages":[{"role":"user","content":"hi"}]}`)
	big := post(`{"model":"Qwen3.8-Max","messages":[{"role":"user","content":"` + strings.Repeat("长", 4000) + `"}]}`)
	if small <= 0 || big < 900 || big <= small {
		t.Fatalf("估算不合比例：small=%d big=%d", small, big)
	}

	// 4000 个汉字 → 约 1000 个「token」（字符/4）；结构字段不该把它抬高太多。
	if big > 1100 || big < 900 {
		t.Errorf("4000 汉字估出 %d，超出 900–1100", big)
	}

	// 两条路径都在（/messages/count_tokens 也有）。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/messages/count_tokens",
		strings.NewReader(`{"model":"Auto","messages":[{"role":"user","content":"hi"}]}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/messages/count_tokens status = %d", rec.Code)
	}
}

// 坏 JSON 要按 Anthropic 错误形状回 400，不能 500。
func TestCountTokensBadJSON(t *testing.T) {
	h := newTestHandler(t, "http://127.0.0.1:1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"type":"error"`) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
