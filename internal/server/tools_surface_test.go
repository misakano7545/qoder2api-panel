package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/misakano7545/qoder2api-panel/internal/reqlog"
)

// 工具观测面：tools_declared = 入站 tools 数组长度，tool_calls 按流式/非流式各自记账。
// 声明了却零调用才是要抓的那类回合（客户端看到的是「什么都没做」的完成）。
func TestRequestLogToolsSurface(t *testing.T) {
	textChunks := []string{
		`{"id":"c1","object":"chat.completion.chunk","model":"Qwen3.8-Max","choices":[{"index":0,"delta":{"content":"你好"},"finish_reason":null}]}`,
		`{"id":"c1","object":"chat.completion.chunk","model":"Qwen3.8-Max","choices":[{"index":0,"delta":{"content":"世界"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
	}
	toolChunks := []string{
		`{"id":"c1","object":"chat.completion.chunk","model":"Qwen3.8-Max","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		`{"id":"c1","object":"chat.completion.chunk","model":"Qwen3.8-Max","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"玉林\"}"}}]},"finish_reason":"tool_calls"}]}`,
	}
	oneTool := `,"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}]`
	twoTools := `,"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}},` +
		`{"type":"function","function":{"name":"get_time","parameters":{"type":"object"}}}]`

	cases := []struct {
		name     string
		chunks   []string
		stream   bool
		tools    string
		declared int
		calls    int
	}{
		{"流式声明了工具但零调用", textChunks, true, twoTools, 2, 0},
		{"流式收到 tool_calls 帧", toolChunks, true, oneTool, 1, 1},
		{"非流式调用合并进 message", toolChunks, false, oneTool, 1, 1},
		{"纯聊天不计入分母", textChunks, false, "", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := fakeQoder(t, tc.chunks)
			h := newTestHandler(t, fake.URL)
			h.cfg.RequestLog = reqlog.New(reqlog.Config{})
			body := `{"model":"Qwen3.8-Max","messages":[{"role":"user","content":"hi"}],"stream":` +
				strconv.FormatBool(tc.stream) + tc.tools + `}`
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			ev := h.cfg.RequestLog.Snapshot().Recent
			if len(ev) != 1 {
				t.Fatalf("请求记录 = %d 条, want 1", len(ev))
			}
			if ev[0].ToolsDeclared != tc.declared || ev[0].ToolCalls != tc.calls {
				t.Errorf("tools_declared/tool_calls = %d/%d, want %d/%d",
					ev[0].ToolsDeclared, ev[0].ToolCalls, tc.declared, tc.calls)
			}
		})
	}
}

// 非流式计数认两种组包形态：只认一种会在线上恒记 0。
func TestToolCallCountBothShapes(t *testing.T) {
	wrap := func(calls any) map[string]any {
		return map[string]any{"choices": []any{map[string]any{
			"message": map[string]any{"tool_calls": calls},
		}}}
	}
	if got := toolCallCount(wrap([]any{map[string]any{}, map[string]any{}})); got != 2 {
		t.Errorf("[]any 形态 = %d, want 2", got)
	}
	if got := toolCallCount(wrap([]map[string]any{{}, {}, {}})); got != 3 {
		t.Errorf("[]map 形态 = %d, want 3", got)
	}
	if got := toolCallCount(map[string]any{"choices": []any{}}); got != 0 {
		t.Errorf("无 choices = %d, want 0", got)
	}
}
