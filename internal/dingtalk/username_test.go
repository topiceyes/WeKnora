package dingtalk

import (
	"regexp"
	"testing"
)

var generatedUsernamePattern = regexp.MustCompile(`^dt-[a-z0-9]+$`)

func TestGenerateUsernameWithJobNumber(t *testing.T) {
	name := GenerateUsername("8823")
	if !regexp.MustCompile(`^dt-8823[a-z0-9]{4}$`).MatchString(name) {
		t.Fatalf("GenerateUsername(8823) = %q, want dt-8823 + 4 random chars", name)
	}
}

func TestGenerateUsernameSanitizesJobNumber(t *testing.T) {
	// 钉钉侧工号可能带字母/符号/前后空格，清洗后只留 [a-z0-9]。
	name := GenerateUsername(" GK-1024 ")
	if !regexp.MustCompile(`^dt-gk1024[a-z0-9]{4}$`).MatchString(name) {
		t.Fatalf("GenerateUsername(\" GK-1024 \") = %q, want dt-gk1024 + 4 random chars", name)
	}
}

func TestGenerateUsernameFallbackWithoutJobNumber(t *testing.T) {
	for _, job := range []string{"", "   ", "---", "中文"} {
		name := GenerateUsername(job)
		if !regexp.MustCompile(`^dt-[a-z0-9]{8}$`).MatchString(name) {
			t.Fatalf("GenerateUsername(%q) = %q, want dt- + 8 random chars", job, name)
		}
	}
}

func TestGenerateUsernameRandomness(t *testing.T) {
	// 同一工号多次生成，随机后缀应几乎必不同（36^4 空间，30 次全撞概率 ~1e-6）。
	seen := map[string]bool{}
	for i := 0; i < 30; i++ {
		name := GenerateUsername("8823")
		if !generatedUsernamePattern.MatchString(name) {
			t.Fatalf("GenerateUsername(8823) = %q, unexpected format", name)
		}
		seen[name] = true
	}
	if len(seen) < 29 {
		t.Fatalf("30 generations produced only %d distinct names, suffix not random", len(seen))
	}
}

func TestUsernamePrefix(t *testing.T) {
	if got := UsernamePrefix("8823"); got != "dt-8823" {
		t.Fatalf("UsernamePrefix(8823) = %q, want dt-8823", got)
	}
	// 清洗后为空的工号返回 ""，调用方（同步改名）据此跳过。
	for _, job := range []string{"", "  ", "-", "中文"} {
		if got := UsernamePrefix(job); got != "" {
			t.Fatalf("UsernamePrefix(%q) = %q, want empty", job, got)
		}
	}
}
