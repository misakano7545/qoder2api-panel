// Package upstream 是 Qoder 上游客户端：地区与端点、COSY 签名、凭证续期、
// 用户资料/额度、模型目录与推理转发。
//
// 推理链路：OpenAI 请求体 → agent_chat_generation 请求体 → COSY 编码+签名 →
// SSE 信封（statusCodeValue + body）→ 内层 OpenAI chunk → 转发给客户端。
package upstream

import (
	"fmt"
	"strings"
)

// 地区：国际版 qoder.com / 国内版 qoder.com.cn，两边的凭证与模型目录不通用。
const (
	RegionGlobal = "global"
	RegionCN     = "cn"
)

// normalRealm 把任意写法归一成上面两个取值（空 = 国际版）。
func normalRealm(realm string) string {
	if ifCN(realm) {
		return RegionCN
	}
	return RegionGlobal
}

func ifCN(realm string) bool {
	switch strings.ToLower(strings.TrimSpace(realm)) {
	case RegionCN, "china":
		return true
	}
	return false
}

// 上游路径（openapi 主机一族）。
const (
	DeviceLoginPath   = "/device/selectAccounts"
	DevicePollPath    = "/api/v1/deviceToken/poll"
	ExchangePath      = "/api/v1/jobToken/exchange"
	UserInfoPath      = "/api/v1/userinfo"
	UsagePath         = "/api/v2/quota/usage"
	PlanPath          = "/api/v2/user/plan"
	CampaignsPath     = "/sash/api/v1/me/campaigns"
	campaignsQuery    = "?forceRefresh=true"
	deviceRefreshPath = "/api/v1/deviceToken/refresh"
	jobRefreshPath    = "/api/v1/jobToken/refresh"
)

// 推理网关（/algo/... 一族）上的路径。
const (
	// EpChat 是 agent_chat_generation 的完整路径。
	EpChat = "algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"
	// EpModelList 是官方模型清单；GET 也必须带与签名一致的 {} body，否则 403。
	EpModelList = "algo/api/v2/model/list?Encode=1"
)

// 默认主机基址。配置可覆盖（面板 upstream 段），这里只是兜底。
const (
	AgentHost     = "https://api3.qoder.sh/" // 国际版推理网关
	AgentHostIntl = "https://api3.qoder.sh/" // 兼容旧配置键：国际版推理网关
	GatewayCN     = "https://gateway.qoder.com.cn/"
	API2Global    = "https://api2.qoder.sh/"
	OpenAPIGlobal = "https://openapi.qoder.sh"
	OpenAPICN     = "https://openapi.qoder.com.cn"
	WebGlobal     = "https://qoder.com"
	WebCN         = "https://qoder.com.cn"

	// DefaultConfigName / Intl 是请求缺 model 时用的名字（官方目录里两区都有 Auto）。
	DefaultConfigName     = "Auto"
	DefaultConfigNameIntl = "Auto"

	// IdeVersion 等只用于面板展示「客户端身份」，不参与签名。
	IdeVersion     = "qoder-cli"
	IdeVersionIntl = "qoder-cli"
	ClientID       = "qoder-cli"

	// SashClientType 必须为 10（IDE 形态拿不到签到活动）。
	SashClientType = "10"
	SashUserAgent  = "Qoder"
	// OpenAPICosyVersion 是 openapi 一族用的协议版本（低于推理网关）。
	OpenAPICosyVersion = "1.0.1"
	// MaxOutputTokens 是单次输出上限；超过约 32K 上游行为会退化。
	MaxOutputTokens = 32768
	// DefaultContextWindow 模型条目缺 max_input_tokens 时的兜底。
	DefaultContextWindow = 200_000
)

// openAPIBase 返回 openapi 主机基址的默认值（userinfo/plan/quota/device poll 一族）。
// 实例可覆盖（见 SetGatewayHosts），测试与自建镜像都靠它把请求指向别的 host。
func openAPIBase(realm string) string {
	if ifCN(realm) {
		return OpenAPICN
	}
	return OpenAPIGlobal
}

// webOrigin 返回门户主机（设备授权页）。
func webOrigin(realm string) string {
	if ifCN(realm) {
		return WebCN
	}
	return WebGlobal
}

// OpenAPIHeaders 是 userinfo / plan / quota / device poll 一族的头（不打签名）。
func OpenAPIHeaders(token string) map[string]string {
	headers := map[string]string{
		"Cosy-Version":    OpenAPICosyVersion,
		"Cosy-ClientType": "5",
	}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return headers
}

// RefreshHeaders 是续期请求的头：不带签名、不带 Authorization。
func RefreshHeaders() map[string]string { return map[string]string{} }

// SashHeaders 是活动/签到接口的头：最小束、cosy-clienttype 必须是 10。
func SashHeaders(token string) map[string]string {
	return map[string]string{
		"authorization":   "Bearer " + token,
		"cosy-clienttype": SashClientType,
		"accept":          "application/json",
		"accept-language": "zh-CN",
		"user-agent":      SashUserAgent,
	}
}

// joinURL 拼接基址与路径：接受「带/不带结尾斜杠」的基址（配置项里两种写法都有人写）。
func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

// ChatURL 拼出推理请求的完整地址。
func ChatURL(base string) string { return joinURL(base, EpChat) }

// ModelListURL 拼出模型清单的完整地址。
func ModelListURL(base string) string { return joinURL(base, EpModelList) }

// 错误码（聚合了参考实现对上游业务码的结论）。
const (
	queueCode = "10605"
	authCode  = "105"
	// quotaCodes 是一族「额度/套餐不支持」的业务码。
	quotaCodesCsv = "110,112,113,114,115,116,117,118,119,122"
)

func quotaCodes() []string { return strings.Split(quotaCodesCsv, ",") }

// errf 是内部小工具，避免多处 fmt.Errorf 拼串重复。
func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }
