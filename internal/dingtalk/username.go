package dingtalk

import (
	crand "crypto/rand"
	"math/big"
	"strings"
)

// 钉钉来源账号的用户名格式（首登建号 + 通讯录同步存量改名共用）：
//
//	有工号: dt-<工号><4位随机>   如 dt-8823x7k2
//	无工号: dt-<8位随机>        如 dt-x7k2m9ab4
//
// helper 放在 dingtalk 包导出：首登建号在 application/service（已依赖
// 本包），同步改名在本包，共用一份格式定义避免两处漂移。

// usernameRandomCharset 随机段字符集：小写字母 + 数字（36^4=168 万组合，
// 工号本身唯一，碰撞概率可忽略；调用方仍有查重重试兜底）。
const usernameRandomCharset = "abcdefghijklmnopqrstuvwxyz0123456789"

const (
	// usernamePrefix 钉钉来源用户名的固定前缀。
	usernamePrefix = "dt-"
	// usernameRandomSuffixLen 有工号时的随机后缀长度。
	usernameRandomSuffixLen = 4
	// usernameRandomFallbackLen 无工号兜底格式的随机段长度。
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

// UsernamePrefix 返回该工号对应的新格式用户名前缀（"dt-<工号>"）。
// 同步改名用它判断「是否已是新格式」：用户名以本人当前工号的前缀开头
// 即视为新格式，不再改名。工号清洗后为空返回 ""（调用方跳过改名）。
func UsernamePrefix(jobNumber string) string {
	job := sanitizeJobNumber(jobNumber)
	if job == "" {
		return ""
	}
	return usernamePrefix + job
}

// GenerateUsername 生成一个钉钉来源用户名候选：
// 有工号 dt-<工号><4位随机>，无工号 dt-<8位随机>。
// 只保证格式，不保证唯一——唯一性由调用方查重重试（随机源失败返回
// 纯前缀，调用方的查重循环会给它撞出唯一名或走时间戳兜底）。
func GenerateUsername(jobNumber string) string {
	if prefix := UsernamePrefix(jobNumber); prefix != "" {
		return prefix + randomUsernameChars(usernameRandomSuffixLen)
	}
	return usernamePrefix + randomUsernameChars(usernameRandomFallbackLen)
}
