package dingtalk

import (
	crand "crypto/rand"
	"math/big"
	"strings"
	"unicode"
)

// 钉钉来源账号的用户名格式（首登建号 + 通讯录同步存量改名共用）：
//
//	姓名+工号: <姓名>-<工号>     如 汪利辉-0003（工号唯一 → 天然无碰撞）
//	仅姓名:    <姓名>-<4位随机>  （无工号时随机段保唯一）
//	仅工号:    dt-<工号><4位随机>（钉钉昵称为空的罕见情形）
//	都无:      dt-<8位随机>     （兜底）
//
// 姓名（钉钉昵称）是主要识别信息，展示位优先。历史两代格式
// （dt-<unionId>、dt-<工号><随机>）都以 "dt-" 开头——这是同步改名时
// 识别「自动注册账号」的唯一标志；新格式无前缀，钉钉侧改名/改工号后
// 用户名不跟随（无法安全识别，要跟随时手动改）。
//
// helper 放在 dingtalk 包导出：首登建号在 application/service（已依赖
// 本包），同步改名在本包，共用一份格式定义避免两处漂移。

// usernameRandomCharset 随机段字符集：小写字母 + 数字（36^4=168 万组合，
// 工号本身唯一，碰撞概率可忽略；调用方仍有查重重试兜底）。
const usernameRandomCharset = "abcdefghijklmnopqrstuvwxyz0123456789"

const (
	// usernameLegacyPrefix 历史自动注册格式的前缀，也是改名范围的判据。
	usernameLegacyPrefix = "dt-"
	// usernameRandomSuffixLen 随机后缀长度。
	usernameRandomSuffixLen = 4
	// usernameRandomFallbackLen 无姓名无工号兜底格式的随机段长度。
	usernameRandomFallbackLen = 8
)

// sanitizeJobNumber 工号清洗：小写、剔除非 [a-z0-9]。工号一般是纯数字，
// 防御管理员在钉钉侧填了字母/符号的组合。
func sanitizeJobNumber(jobNumber string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(jobNumber)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizePersonName 姓名清洗：保留汉字/字母/数字，剔除空白与符号。
// 不能复用 service 包的 sanitizeUsernameCandidate——那里会小写化且剔除
// 汉字，中文姓名会被清成空串。"张 三"→"张三"、"Jack.M"→"JackM"。
func sanitizePersonName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.Is(unicode.Han, r) || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// randomUsernameChars 用 crypto/rand 生成 n 位 [a-z0-9] 随机串。
// 随机源失败属不可恢复错误，返回空串由调用方兜底。
func randomUsernameChars(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		idx, err := crand.Int(crand.Reader, big.NewInt(int64(len(usernameRandomCharset))))
		if err != nil {
			return ""
		}
		b.WriteByte(usernameRandomCharset[idx.Int64()])
	}
	return b.String()
}

// ExpectedUsername 返回该用户的规范用户名（"<姓名>-<工号>"）。
// 同步改名用它判断「是否已达标」：当前用户名等于规范名即不再动。
// 姓名或工号任一缺失返回 ""（没有规范名，调用方跳过改名）。
func ExpectedUsername(name, jobNumber string) string {
	job := sanitizeJobNumber(jobNumber)
	person := sanitizePersonName(name)
	if job == "" || person == "" {
		return ""
	}
	return person + "-" + job
}

// GenerateUsername 生成一个钉钉来源用户名候选（格式见文件头）。
// 只保证格式，不保证唯一——唯一性由调用方查重重试（随机源失败返回
// 空串，调用方的时间戳兜底会给出可用名）。
func GenerateUsername(name, jobNumber string) string {
	person := sanitizePersonName(name)
	job := sanitizeJobNumber(jobNumber)
	switch {
	case person != "" && job != "":
		return person + "-" + job
	case person != "":
		return person + "-" + randomUsernameChars(usernameRandomSuffixLen)
	case job != "":
		return usernameLegacyPrefix + job + randomUsernameChars(usernameRandomSuffixLen)
	default:
		return usernameLegacyPrefix + randomUsernameChars(usernameRandomFallbackLen)
	}
}
