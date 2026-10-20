package upstream

import "testing"

// TestFirstFrameError 钉住重测的首帧判定：200 的信封里也可能装着 403/额度/排队，
// 那种情况必须算失败；而正常回答里出现 "subscription" 之类的词不能被当成额度不足
// （所以只认结构字段 statusCodeValue 与排队码，不拿整段原文跑关键词分类）。
func TestFirstFrameError(t *testing.T) {
	ok := []string{
		`data: {"statusCodeValue":200,"body":{"choices":[{"delta":{"content":"pong"}}]}}`,
		`data: {"statusCodeValue":200,"body":{"choices":[{"delta":{"content":"your subscription is active"}}]}}`,
	}
	for _, chunk := range ok {
		if err := firstFrameError([]byte(chunk), nil); err != nil {
			t.Errorf("正常首帧被判成错误：%q → %v", chunk, err)
		}
	}

	cases := []struct {
		chunk string
		want  ErrKind
	}{
		{`data: {"statusCodeValue":403,"body":"{\"code\":\"110\"}"}`, ErrPlanLimit},
		{`data: {"statusCodeValue":401}`, ErrSessionDead},
		{`data: {"statusCodeValue":403,"body":"{\"code\":\"10605\",\"message\":\"{\\\"isQueued\\\":true}\"}"}`, ErrBusy},
	}
	for _, c := range cases {
		err := firstFrameError([]byte(c.chunk), nil)
		if err == nil {
			t.Fatalf("错误信封应被判失败：%q", c.chunk)
		}
		typed, isTyped := err.(*Error)
		if !isTyped {
			t.Fatalf("错误类型应是 *Error：%T", err)
		}
		if typed.Kind != c.want {
			t.Errorf("首帧 %q 分类 = %v，want %v", c.chunk, typed.Kind, c.want)
		}
	}
}
