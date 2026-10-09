package dingtalk

import (
	"regexp"
	"testing"
)

func TestGenerateUsernameNameAndJobNumber(t *testing.T) {
	// 规范格式：姓名-工号（无随机段，工号唯一保证不碰撞）
	if got := GenerateUsername("汪利辉", "0003"); got != "汪利辉-0003" {
		t.Fatalf("GenerateUsername(汪利辉, 0003) = %q, want 汪利辉-0003", got)
	}
}

func TestGenerateUsernameSanitizes(t *testing.T) {
	// 姓名里的空白/符号剔除；工号清洗成小写 [a-z0-9]
	if got := GenerateUsername("张 三", "8823"); got != "张三-8823" {
		t.Fatalf("GenerateUsername(\"张 三\") = %q, want 张三-8823", got)
	}
	if got := GenerateUsername("Jack.M", " GK-1024 "); got != "JackM-gk1024" {
		t.Fatalf("GenerateUsername(Jack.M, \" GK-1024 \") = %q, want JackM-gk1024", got)
	}
}

func TestGenerateUsernameNameOnly(t *testing.T) {
	// 无工号：姓名-4位随机（重点姓名，随机段保唯一）
	if got := GenerateUsername("汪利辉", ""); !regexp.MustCompile(`^汪利辉-[a-z0-9]{4}$`).MatchString(got) {
		t.Fatalf("GenerateUsername(name only) = %q, want 姓名 + 4 random chars", got)
	}
	if got := GenerateUsername("汪利辉", "中文"); !regexp.MustCompile(`^汪利辉-[a-z0-9]{4}$`).MatchString(got) {
		t.Fatalf("non-alnum job should be treated as missing: %q", got)
	}
}

func TestGenerateUsernameJobOnlyAndNeither(t *testing.T) {
	// 仅工号（钉钉昵称为空）：沿用 dt- 前缀 + 工号 + 随机
	if got := GenerateUsername("", "8823"); !regexp.MustCompile(`^dt-8823[a-z0-9]{4}$`).MatchString(got) {
		t.Fatalf("GenerateUsername(job only) = %q, want dt-8823 + 4 random", got)
	}
	// 都没有：dt-8位随机兜底
	for _, in := range [][2]string{{"", ""}, {"  ", "---"}} {
		if got := GenerateUsername(in[0], in[1]); !regexp.MustCompile(`^dt-[a-z0-9]{8}$`).MatchString(got) {
			t.Fatalf("GenerateUsername(%q,%q) = %q, want dt- + 8 random", in[0], in[1], got)
		}
	}
}

func TestGenerateUsernameRandomSuffixVaries(t *testing.T) {
	// 无工号时随机段应几乎必不同（36^4 空间，30 次全撞概率 ~1e-6）
	seen := map[string]bool{}
	for i := 0; i < 30; i++ {
		seen[GenerateUsername("张三", "")] = true
	}
	if len(seen) < 29 {
		t.Fatalf("30 generations produced only %d distinct names, suffix not random", len(seen))
	}
}

func TestExpectedUsername(t *testing.T) {
	if got := ExpectedUsername("汪利辉", "0003"); got != "汪利辉-0003" {
		t.Fatalf("ExpectedUsername = %q, want 汪利辉-0003", got)
	}
	// 姓名或工号任一缺失 → ""（调用方据此跳过改名）
	for _, in := range [][2]string{{"", "0003"}, {"汪利辉", ""}, {"---", "中文"}, {"", ""}} {
		if got := ExpectedUsername(in[0], in[1]); got != "" {
			t.Fatalf("ExpectedUsername(%q,%q) = %q, want empty", in[0], in[1], got)
		}
	}
}
