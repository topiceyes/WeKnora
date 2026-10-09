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

func TestGenerateDingTalkUsernameUsesJobNumber(t *testing.T) {
	svc := &userService{userRepo: &stubUserRepoForAuth{}}

	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{
		JobNumber: "8823",
	})
	if !regexp.MustCompile(`^dt-8823[a-z0-9]{4}$`).MatchString(name) {
		t.Fatalf("generateDingTalkUsername(job=8823) = %q, want dt-8823 + 4 random chars", name)
	}
}

func TestGenerateDingTalkUsernameFallbackWithoutJobNumber(t *testing.T) {
	svc := &userService{userRepo: &stubUserRepoForAuth{}}

	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{})
	if !regexp.MustCompile(`^dt-[a-z0-9]{8}$`).MatchString(name) {
		t.Fatalf("generateDingTalkUsername(no job) = %q, want dt- + 8 random chars", name)
	}
}

func TestGenerateDingTalkUsernameCollisionFallsBackToTimestamp(t *testing.T) {
	// 查重永远命中（撞名 20 次）→ 时间戳兜底，保证返回可用用户名。
	svc := &userService{userRepo: &collidingUserRepo{}}

	name := svc.generateDingTalkUsername(context.Background(), &dingtalk.DingTalkUserProfile{
		JobNumber: "8823",
	})
	if !regexp.MustCompile(`^dt-[0-9]+$`).MatchString(name) {
		t.Fatalf("generateDingTalkUsername(collision) = %q, want dt- + timestamp fallback", name)
	}
}
