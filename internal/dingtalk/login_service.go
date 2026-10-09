package dingtalk

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
)

// LoginService 钉钉扫码登录编排：authCode → userToken → users/me → unionId 换 userid。
// 只负责钉钉侧身份解析并返回统一用户资料，不生成 JWT——本地会话签发是
// user service 的职责（见 service.userService.LoginWithDingTalk）。
type LoginService struct {
	client *Client
	repo   *Repository
}

// NewLoginService 构造 LoginService。repo 目前仅作保留依赖（后续持久化
// userToken / 登录审计时使用），登录主流程不直接读写它。
func NewLoginService(client *Client, repo *Repository) *LoginService {
	return &LoginService{client: client, repo: repo}
}

// LoginWithAuthCode 用扫码回调的 authCode 完成钉钉侧链路：
//
//  1. GetUserAccessToken 换个人 userToken；
//  2. GetUserMe 拿 unionId/nick/email/mobile；
//  3. GetUserIDByUnionID 换企业内部 userid；
//  4. 有 userid 时 user/get 补查工号（首登建号用户名用 dt-工号 格式）。
//
// 第 3 步失败（企业权限不足等）不阻断登录：记 warn，DingUserID 留空，
// 先用 unionId 登录，等通讯录同步时补 userid。第 4 步同理——工号拿不到
// 时首登用户名走随机兜底格式，绝不让登录失败。
func (s *LoginService) LoginWithAuthCode(ctx context.Context, authCode string) (*DingTalkUserProfile, error) {
	authCode = strings.TrimSpace(authCode)
	if authCode == "" {
		return nil, errors.New("authCode is required")
	}

	tokenResp, err := s.client.GetUserAccessToken(ctx, authCode)
	if err != nil {
		return nil, err
	}

	me, err := s.client.GetUserMe(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(me.UnionID) == "" {
		return nil, errors.New("dingtalk users/me returned empty unionId")
	}

	profile := &DingTalkUserProfile{
		UnionID: strings.TrimSpace(me.UnionID),
		CorpID:  s.client.CorpID(),
		Nick:    strings.TrimSpace(me.Nick),
		Mobile:  strings.TrimSpace(me.Mobile),
		Email:   strings.TrimSpace(me.Email),
	}

	dingUserID, err := s.client.GetUserIDByUnionID(ctx, profile.UnionID)
	if err != nil {
		logger.Warnf(ctx,
			"DingTalk login: getbyunionid failed (unionId=%s), continue without ding userid: %v",
			profile.UnionID, err)
		return profile, nil
	}
	profile.DingUserID = strings.TrimSpace(dingUserID)

	if detail, derr := s.client.GetUserDetailByUserID(ctx, profile.DingUserID); derr != nil {
		logger.Warnf(ctx,
			"DingTalk login: user/get failed (userid=%s), continue without job number: %v",
			profile.DingUserID, derr)
	} else {
		profile.JobNumber = strings.TrimSpace(detail.JobNumber)
	}
	return profile, nil
}
