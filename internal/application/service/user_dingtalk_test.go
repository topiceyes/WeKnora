package service

import (
	"context"
	"regexp"
	"testing"

	"github.com/Tencent/WeKnora/internal/dingtalk"
	"github.com/Tencent/WeKnora/internal/types"
)

// collidingUserRepo 让 GetUserByUsername 永远命中已有用户，模拟用户名撞名。
type collidingUserRepo struct {
	stubUserRepoForAuth
}

func (c *collidingUserRepo) GetUserByUsername(context.Context, string) (*types.User, error) {
	return &types.User{ID: "taken", Username: "taken"}, nil
}

func TestGenerateDingTalkUsernameCanonical(t *testing.T) {
	// 规范格式：姓名-工号（无随机段）
	svc := &userService{userRepo: &stubUserRepoForAuth{}}
	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{
		Nick: "汪利辉", JobNumber: "0003",
	})
	if name != "汪利辉-0003" {
		t.Fatalf("generateDingTalkUsername(汪利辉, 0003) = %q, want 汪利辉-0003", name)
	}
}

func TestGenerateDingTalkUsernameNameOnlyFallback(t *testing.T) {
	// 无工号：姓名-4位随机
	svc := &userService{userRepo: &stubUserRepoForAuth{}}
	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{
		Nick: "汪利辉",
	})
	if !regexp.MustCompile(`^汪利辉-[a-z0-9]{4}$`).MatchString(name) {
		t.Fatalf("generateDingTalkUsername(no job) = %q, want 姓名 + 4 random chars", name)
	}
}

func TestGenerateDingTalkUsernameNothingFallback(t *testing.T) {
	// 姓名工号全无：dt-8位随机兜底
	svc := &userService{userRepo: &stubUserRepoForAuth{}}
	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{})
	if !regexp.MustCompile(`^dt-[a-z0-9]{8}$`).MatchString(name) {
		t.Fatalf("generateDingTalkUsername(nothing) = %q, want dt- + 8 random chars", name)
	}
}

func TestGenerateDingTalkUsernameCollisionFallsBackToTimestamp(t *testing.T) {
	// 查重永远命中（规范名是确定性的，撞 20 次）→ 时间戳兜底。
	svc := &userService{userRepo: &collidingUserRepo{}}
	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{
		Nick: "张三", JobNumber: "8823",
	})
	if !regexp.MustCompile(`^dt-[0-9]+$`).MatchString(name) {
		t.Fatalf("generateDingTalkUsername(collision) = %q, want dt- + timestamp fallback", name)
	}
}
