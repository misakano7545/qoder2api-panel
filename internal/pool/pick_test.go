package pool

import (
	"testing"

	"github.com/misakano7545/qoder2api-panel/internal/auth"
)

func testAccount(uid, realm string) *auth.Auth {
	return &auth.Auth{
		AccessToken: "dt-" + uid,
		UID:         uid,
		RealmID:     realm,
		MachineID:   "m-" + uid,
	}
}

// TestPickWithoutCreditsOrExpiry 是「哨兵值不能当不可用」的守卫：
// 余额未查到（credits=0）与没有到期信息（expire=0）的账号必须**仍然参与选号**，
// 只是不享受「快过期优先」加成。参考实现曾把「无到期」与「不可用」混成一个
// 初值，结果全部账号都是永久积分时一个都选不出来，却报「没有已登录账号」。
func TestPickWithoutCreditsOrExpiry(t *testing.T) {
	p := New("")
	for _, uid := range []string{"u-1", "u-2", "u-3"} {
		p.Add(testAccount(uid, auth.RealmCN))
	}
	seen := map[string]int{}
	for i := 0; i < 90; i++ {
		a := p.Pick()
		if a == nil {
			t.Fatalf("第 %d 次选号返回 nil：无积分/无到期的账号被当成了不可用", i)
		}
		seen[a.UID]++
		// Pick 会占在途名额（max_in_flight 默认 3），不释放的话第 10 次左右
		// 就会因为「全都在途」返回 nil —— 那是限流，不是本用例要测的。
		p.Release(a.UID)
	}
	// 加性权重下三个号概率相同，90 次里漏掉任一个的概率是 (2/3)^90，可忽略。
	if len(seen) != 3 {
		t.Errorf("三个同等账号应都被抽到（不能永久饿死某个号）：%v", seen)
	}
}

// TestPickExcludesDisabledAndCooling 对照：真正「不可用」只有禁用与冷却两条，
// 它们必须被剔除——上面的哨兵放宽不能顺手把这层也放宽。
func TestPickExcludesDisabledAndCooling(t *testing.T) {
	p := New("")
	p.Add(testAccount("u-1", auth.RealmCN))
	p.Add(testAccount("u-2", auth.RealmCN))
	p.Disable("u-2", "test")
	if a := p.Pick(); a == nil || a.UID != "u-1" {
		t.Fatalf("禁用号不该被选中：%+v", a)
	}

	p.Add(testAccount("u-3", auth.RealmCN))
	p.Cooldown("u-1", CoolSoft, 0, "test") // 0 时长 = 立刻可用的边界，不参与剔除
	if a := p.Pick(); a == nil {
		t.Fatalf("冷却时长为 0 不该把账号判成不可用")
	}
}

// TestPickRealmIsolation 地区不得串：国际号不能顶上国内请求（模型可见性不同）。
func TestPickRealmIsolation(t *testing.T) {
	p := New("")
	p.Add(testAccount("u-cn", auth.RealmCN))
	p.Add(testAccount("u-intl", auth.RealmGlobal))
	if a := p.PickRealmExcluding(auth.RealmGlobal, nil); a == nil || a.UID != "u-intl" {
		t.Fatalf("intl 请求应选到国际号：%+v", a)
	}
	if a := p.PickRealmExcluding(auth.RealmCN, nil); a == nil || a.UID != "u-cn" {
		t.Fatalf("cn 请求应选到国内号：%+v", a)
	}
}
