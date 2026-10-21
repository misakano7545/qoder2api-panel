package pool

import (
	"testing"
	"time"

	"github.com/misakano7545/qoder2api-panel/internal/auth"
)

// 本文件钉住「权益不足」硬冷却的时长判据：冷到**北京时间次日 0 点**（额度重置点），
// 再用 plan_credit 作上限截断。两个失败方向都要防：
//
//	冷得太短 → 号提前回池，反复撞 1005/429，面板原因被改写；
//	冷得太长 → 额度 0 点就恢复了却还在躺（旧的固定 12h 在 23:00 撞上就是白躺 11 小时）。
//
// 时刻一律用 UTC 写：业务边界是北京时间，本地时区不该参与计算。

func TestPlanCooldownAt(t *testing.T) {
	const cap12h = 12 * time.Hour
	// 北京 23:40 == UTC 15:40
	at2340 := time.Date(2026, 10, 21, 15, 40, 0, 0, time.UTC)
	// 北京 12:00 == UTC 04:00
	at1200 := time.Date(2026, 10, 21, 4, 0, 0, 0, time.UTC)
	// 北京 00:30 == UTC 前一日 16:30
	at0030 := time.Date(2026, 10, 20, 16, 30, 0, 0, time.UTC)
	// 北京 23:59:59 == UTC 15:59:59
	at235959 := time.Date(2026, 10, 21, 15, 59, 59, 0, time.UTC)

	cases := []struct {
		name string
		now  time.Time
		cap  time.Duration
		want time.Duration
	}{
		// 23:40 撞上：离重置只剩 20 分钟，旧的固定 12h 会白躺 11 小时。
		{"23:40 冷到 0 点", at2340, cap12h, 20 * time.Minute},
		{"23:59:59 冷到 0 点", at235959, cap12h, time.Second},
		// 12:00 撞上：到 0 点正好 12h，与上限相等（这条锁住「边界不抖」）。
		{"12:00 恰好等于上限", at1200, cap12h, 12 * time.Hour},
		// 00:30 撞上：到 0 点还有 23.5h，被上限截断到 12h —— 误判兜底就靠这一格。
		{"00:30 被上限截断", at0030, cap12h, 12 * time.Hour},
		// cap=0 表示不设上限：一律到 0 点。
		{"00:30 无上限", at0030, 0, 23*time.Hour + 30*time.Minute},
		{"23:40 无上限", at2340, 0, 20 * time.Minute},
		// 负 cap 与 0 同义（零值 Limits 也要能安全工作）。
		{"00:30 负上限等同无上限", at0030, -time.Hour, 23*time.Hour + 30*time.Minute},
	}

	for _, tc := range cases {
		got := planCooldownAt(tc.now, tc.cap)
		if got != tc.want {
			t.Errorf("%s：得到 %v，want %v", tc.name, got, tc.want)
		}
	}
}

// TestPlanCooldownAtIgnoresLocalZone 业务边界不该随部署时区漂移。
// 同一个 UTC 时刻换个构造方式（带时区偏移）必须得到同一结果 —— 判据只看 UTC+8 的日历日。
func TestPlanCooldownAtIgnoresLocalZone(t *testing.T) {
	// 北京 23:40 的两种等价写法：UTC 15:40 / +08:00 23:40
	inUTC := time.Date(2026, 10, 21, 15, 40, 0, 0, time.UTC)
	cst := time.FixedZone("CST", 8*3600)
	inCST := time.Date(2026, 10, 21, 23, 40, 0, 0, cst)

	a := planCooldownAt(inUTC, 12*time.Hour)
	b := planCooldownAt(inCST, 12*time.Hour)
	if a != b {
		t.Fatalf("同一时刻不同时区写法结果不一致：%v vs %v", a, b)
	}
	if a != 20*time.Minute {
		t.Fatalf("北京时间 23:40 应冷 20 分钟，得到 %v", a)
	}
}

// TestCooldownPlanWiresToMidnight 是**接线**用例（纯函数用例守不住这里）：
// 把 CooldownPlan 改回 `p.lim.PlanCooldown` 而 planCooldownAt 变成死函数时，
// 只测纯函数会全绿 —— 这正是「守卫有牙」与「判据被用上」是两件事。
//
// 时钟拨到北京 23:40：正确实现给 20 分钟，固定 12h 的实现给 12 小时，两种结果分叉。
func TestCooldownPlanWiresToMidnight(t *testing.T) {
	const cap12h = 12 * time.Hour
	at2340 := time.Date(2026, 10, 21, 15, 40, 0, 0, time.UTC) // 北京 23:40
	restore := poolNow
	poolNow = func() time.Time { return at2340 }
	defer func() { poolNow = restore }()

	p := New("")
	p.ApplyLimits(Limits{PlanCooldown: cap12h})
	p.Add(testAccount("u-1", auth.RealmCN))

	before := time.Now()
	p.CooldownPlan("u-1")

	st, ok := p.Status("u-1")
	if !ok {
		t.Fatal("账号不在池里")
	}
	// 断言的是**剩余时长**而不是绝对时刻：cooldownLocked 用真实时钟算 until
	// （注入时钟只决定时长），所以这里按差值判。差 12 小时即为接线错误。
	remain := st.Until.Sub(before)
	if remain < 19*time.Minute || remain > 21*time.Minute {
		t.Fatalf("北京 23:40 撞 1005 应只剩约 20 分钟（冷到次日 0 点），得到 %v ——"+
			"若约 12 小时，说明 CooldownPlan 没用 planCooldownAt", remain)
	}
	if st.Reason != "plan 权益不足" {
		t.Errorf("冷却原因 = %q", st.Reason)
	}
}

// TestPlanCooldownAtNeverZero 冷却时长恒为正：0 会让上一次硬冷却被 setCooldownLocked
// 的「只延长不缩短」判据忽略，甚至算出一个已经过去的时刻。
func TestPlanCooldownAtNeverZero(t *testing.T) {
	base := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 24*4; i++ { // 每 15 分钟采一格，覆盖全天
		now := base.Add(time.Duration(i) * 15 * time.Minute)
		if d := planCooldownAt(now, 12*time.Hour); d <= 0 {
			t.Fatalf("%v：冷却时长必须为正，得到 %v", now, d)
		}
		if d := planCooldownAt(now, 0); d <= 0 {
			t.Fatalf("%v（无上限）：冷却时长必须为正，得到 %v", now, d)
		}
	}
}
