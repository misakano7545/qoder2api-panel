package upstream

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// 本文件钉住「零输出」判据（对照 dsh-providers-hub 的 zero-output-guard.ts）：
// 上游有时**收下请求却一个字不吐**，只回一个 finish。两条边界的取舍相反：
//
//	finish=length + 零输出  → 输入被整段拒了（越上下文墙）⇒ 必须判失败，让上层换号；
//	finish=length + 有正文  → 模型真答到输出上限 ⇒ 必须原样透出，换号会丢正文；
//	finish=stop   + 零输出  → 服务端主动的空回答（如安全拒答）⇒ 仍按原样透出。
//
// 三种形态在 chunk 层几乎同形，只靠 finish_reason 与「有没有正文」区分，
// 所以每一条都要有独立用例 —— 只留一条会把另外两条的方向搞反。

// TestStreamZeroOutputLength 只回 finish_reason=length、正文为空：必须报 ErrEmptyStream，
// 且**一个字节都不能写给客户端**（响应未提交，上层换号重试才是安全的）。
func TestStreamZeroOutputLength(t *testing.T) {
	for _, finish := range []string{"length", "max_tokens"} {
		stream := sseStream(
			`{"id":"c1","model":"DeepSeek-V4-Pro","choices":[{"index":0,"delta":{},"finish_reason":"` + finish + `"}]}`,
		)
		rec := httptest.NewRecorder()
		_, err := StreamWithError(rec, strings.NewReader(stream), nil)
		if err != ErrEmptyStream {
			t.Errorf("finish_reason=%s 且零输出：应返回 ErrEmptyStream，得到 %v", finish, err)
		}
		if got := rec.Body.Len(); got != 0 {
			t.Errorf("finish_reason=%s：报错前不该写任何字节，实际写了 %d 字节：%s", finish, got, rec.Body.String())
		}
	}
}

// TestStreamLengthWithContent 有正文 + finish_reason=length：必须正常透出。
// 这是与上一条相反的方向 —— 这里换号会丢掉用户已经看到的那半句回答。
func TestStreamLengthWithContent(t *testing.T) {
	stream := sseStream(
		`{"id":"c1","model":"DeepSeek-V4-Pro","choices":[{"index":0,"delta":{"content":"前半句"}}]}`,
		`{"id":"c1","model":"DeepSeek-V4-Pro","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
	)
	rec := httptest.NewRecorder()
	if _, err := StreamWithError(rec, strings.NewReader(stream), nil); err != nil {
		t.Fatalf("有正文时不该报错，得到 %v", err)
	}
	out := rec.Body.String()
	for _, want := range []string{`"content":"前半句"`, `"finish_reason":"length"`, "data: [DONE]"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺 %q：%s", want, out)
		}
	}
}

// TestStreamEmptyStopStillPasses 零输出 + finish_reason=stop：保持原样透出。
// 这不是越墙，是服务端主动的空回答；判成失败会把每个号都白试一遍。
func TestStreamEmptyStopStillPasses(t *testing.T) {
	stream := sseStream(
		`{"id":"c1","model":"DeepSeek-V4-Pro","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)
	rec := httptest.NewRecorder()
	if _, err := StreamWithError(rec, strings.NewReader(stream), nil); err != nil {
		t.Fatalf("stop 的空回复应透出，得到 %v", err)
	}
	if !strings.Contains(rec.Body.String(), `"finish_reason":"stop"`) {
		t.Errorf("缺 finish_reason=stop：%s", rec.Body.String())
	}
}

// TestAggregateZeroOutputLength 非流式同一形态：Aggregate 的判据只看「有没有产出」，
// 与 finish 无关，本来就该报错 —— 这里把它钉住，防止有人把非流式改成对齐流式的旧判据。
func TestAggregateZeroOutputLength(t *testing.T) {
	stream := sseStream(
		`{"id":"c1","model":"DeepSeek-V4-Pro","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
	)
	if _, err := Aggregate(strings.NewReader(stream)); err != ErrEmptyStream {
		t.Fatalf("应返回 ErrEmptyStream，得到 %v", err)
	}
}
