// Package dingtalk 钉钉扫码登录 + 通讯录同步的公共 API client 与数据模型（二开 fork 新增）。
//
// 设计说明：项目里已有两处钉钉 gettoken 实现（datasource connector、im adapter），
// 均为包私有。本包独立实现一份带缓存/重试/脱敏的 client 供登录与同步使用，
// 不改旧文件以避免上游冲突；后续可考虑让旧代码复用本包。
package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// 企业内部应用 OpenAPI（通讯录/凭证）
	corpAPIBase = "https://oapi.dingtalk.com"
	// 新版统一身份认证/个人 API
	apiBase = "https://api.dingtalk.com"

	apiTimeout       = 30 * time.Second
	maxResponseBytes = 16 << 20
	maxAttempts      = 3
	// token 提前过期余量，避免临界点使用旧 token
	tokenExpirySkew = 5 * time.Minute
)

// Client 钉钉 API client，封装企业内部应用 access_token 缓存与用户级 userToken 调用。
type Client struct {
	appKey    string
	appSecret string
	corpID    string
	http      *http.Client

	mu          sync.Mutex
	corpToken   string
	corpExpires time.Time
}

// NewClient 构造钉钉 client。appKey/appSecret 来自企业内部应用，corpID 用于多企业隔离键。
func NewClient(appKey, appSecret, corpID string) *Client {
	return &Client{
		appKey:    strings.TrimSpace(appKey),
		appSecret: strings.TrimSpace(appSecret),
		corpID:    strings.TrimSpace(corpID),
		http:      &http.Client{Timeout: apiTimeout},
	}
}

// CorpID 返回企业 CorpId。
func (c *Client) CorpID() string { return c.corpID }

// ---------------------------------------------------------------------------
// 企业内部应用 access_token（gettoken，缓存）
// ---------------------------------------------------------------------------

type corpTokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpireIn    int    `json:"expire_in"` // 秒
}

// GetCorpAccessToken 获取企业内部应用 access_token（内存缓存，提前过期）。
func (c *Client) GetCorpAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.corpToken != "" && time.Now().Before(c.corpExpires) {
		token := c.corpToken
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	var resp corpTokenResponse
	u := fmt.Sprintf("%s/gettoken?appkey=%s&appsecret=%s",
		corpAPIBase, c.appKey, c.appSecret)
	if err := c.do(ctx, http.MethodGet, u, nil, "", &resp); err != nil {
		return "", err
	}
	if resp.ErrCode != 0 {
		return "", fmt.Errorf("dingtalk gettoken failed: errcode=%d errmsg=%s", resp.ErrCode, redact(resp.ErrMsg, c.appSecret))
	}

	c.mu.Lock()
	c.corpToken = resp.AccessToken
	c.corpExpires = time.Now().Add(time.Duration(resp.ExpireIn)*time.Second - tokenExpirySkew)
	c.mu.Unlock()
	return resp.AccessToken, nil
}

// ---------------------------------------------------------------------------
// 扫码登录：authCode → userToken → users/me
// ---------------------------------------------------------------------------

// UserTokenResp 个人访问令牌响应。
type UserTokenResp struct {
	AccessToken string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpireIn    int64  `json:"expireIn"` // 秒
	CorpID      string `json:"corpId"`
}

// GetUserAccessToken 用授权码换个人 userToken（POST /v1.0/oauth2/userAccessToken）。
func (c *Client) GetUserAccessToken(ctx context.Context, authCode string) (*UserTokenResp, error) {
	body := map[string]string{
		"clientId":     c.appKey,
		"clientSecret": c.appSecret,
		"code":         authCode,
		"grantType":    "authorization_code",
	}
	var resp UserTokenResp
	if err := c.do(ctx, http.MethodPost, apiBase+"/v1.0/oauth2/userAccessToken", body, "", &resp); err != nil {
		return nil, fmt.Errorf("exchange user access token: %w", err)
	}
	if resp.AccessToken == "" {
		return nil, fmt.Errorf("empty user access token in response")
	}
	return &resp, nil
}

// ContactUserMe 当前登录用户信息（/v1.0/contact/users/me）。
type ContactUserMe struct {
	UnionID      string `json:"unionId"`
	Nick         string `json:"nick"`
	AvatarURL    string `json:"avatarUrl"`
	Mobile       string `json:"mobile"`
	Email        string `json:"email"`
	OpenID       string `json:"openId"`
	StateCode    string `json:"stateCode"` // 手机号国家码
}

// GetUserMe 用个人 userToken 获取当前用户信息。
func (c *Client) GetUserMe(ctx context.Context, userToken string) (*ContactUserMe, error) {
	var resp ContactUserMe
	if err := c.do(ctx, http.MethodGet, apiBase+"/v1.0/contact/users/me", nil, userToken, &resp); err != nil {
		return nil, fmt.Errorf("get users/me: %w", err)
	}
	return &resp, nil
}

// GetUserIDByUnionID 用 unionId 换企业内部 userid（POST /topapi/v2/user/getbyunionid，需企业 token）。
func (c *Client) GetUserIDByUnionID(ctx context.Context, unionID string) (string, error) {
	token, err := c.GetCorpAccessToken(ctx)
	if err != nil {
		return "", err
	}
	u := fmt.Sprintf("%s/topapi/v2/user/getbyunionid?access_token=%s", corpAPIBase, token)
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Result  struct {
			UserID string `json:"userid"`
		} `json:"result"`
	}
	if err := c.do(ctx, http.MethodPost, u, map[string]string{"unionid": unionID}, "", &resp); err != nil {
		return "", err
	}
	if resp.ErrCode != 0 {
		return "", fmt.Errorf("getbyunionid failed: errcode=%d errmsg=%s", resp.ErrCode, redact(resp.ErrMsg, c.appSecret))
	}
	return resp.Result.UserID, nil
}

// ---------------------------------------------------------------------------
// 通讯录同步：部门列表 + 部门成员分页
// ---------------------------------------------------------------------------

// ListDepartmentIDs 递归获取全部部门 id（从根部门 1 开始）。
func (c *Client) ListDepartmentIDs(ctx context.Context) ([]int64, error) {
	token, err := c.GetCorpAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	var all []int64
	queue := []int64{1}
	seen := map[int64]bool{1: true}
	for len(queue) > 0 {
		deptID := queue[0]
		queue = queue[1:]
		all = append(all, deptID)

		u := fmt.Sprintf("%s/topapi/v2/department/listsubid?access_token=%s", corpAPIBase, token)
		var resp struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
			Result  []struct {
				DeptID int64 `json:"dept_id"`
			} `json:"result"`
		}
		if err := c.do(ctx, http.MethodPost, u, map[string]int64{"dept_id": deptID}, "", &resp); err != nil {
			return nil, err
		}
		if resp.ErrCode != 0 {
			return nil, fmt.Errorf("listsubid(dept=%d) failed: errcode=%d errmsg=%s", deptID, resp.ErrCode, redact(resp.ErrMsg, c.appSecret))
		}
		for _, r := range resp.Result {
			if !seen[r.DeptID] {
				seen[r.DeptID] = true
				queue = append(queue, r.DeptID)
			}
		}
	}
	return all, nil
}

// DeptUserDetail 部门成员详情（对账用）。
type DeptUserDetail struct {
	UserID     string  `json:"userid"`
	Name       string  `json:"name"`
	Mobile     string  `json:"mobile"`
	Email      string  `json:"email"`
	Title      string  `json:"title"`
	JobNumber  string  `json:"job_number"`
	DeptIDList []int64 `json:"dept_id_list"`
}

// DeptUserPage 部门成员分页结果。
type DeptUserPage struct {
	List      []DeptUserDetail
	NextCursor int64
	HasMore   bool
}

// ListDeptUsers 分页拉取部门成员（POST /topapi/v2/user/list，需企业 token）。
func (c *Client) ListDeptUsers(ctx context.Context, deptID, cursor, size int64) (*DeptUserPage, error) {
	token, err := c.GetCorpAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/topapi/v2/user/list?access_token=%s", corpAPIBase, token)
	body := map[string]interface{}{
		"dept_id": deptID,
		"cursor":  cursor,
		"size":    size,
	}
	var resp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Result  struct {
			List       []DeptUserDetail `json:"list"`
			NextCursor int64            `json:"next_cursor"`
			HasMore    bool             `json:"has_more"`
		} `json:"result"`
	}
	if err := c.do(ctx, http.MethodPost, u, body, "", &resp); err != nil {
		return nil, err
	}
	if resp.ErrCode != 0 {
		return nil, fmt.Errorf("user/list(dept=%d) failed: errcode=%d errmsg=%s", deptID, resp.ErrCode, redact(resp.ErrMsg, c.appSecret))
	}
	return &DeptUserPage{
		List:       resp.Result.List,
		NextCursor: resp.Result.NextCursor,
		HasMore:    resp.Result.HasMore,
	}, nil
}

// ---------------------------------------------------------------------------
// 底层 HTTP 执行器：JSON 编解码 + 重试 + 脱敏
// ---------------------------------------------------------------------------

// do 发起一次 JSON 请求。userToken 非空时注入个人访问令牌头，否则不注入（企业 token 走 URL query）。
// 429/5xx 指数退避重试（最多 maxAttempts 次），响应体上限 maxResponseBytes。
func (c *Client) do(ctx context.Context, method, url string, body interface{}, userToken string, result interface{}) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
	}

	delay := 200 * time.Millisecond
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			delay *= 2
		}

		var bodyReader io.Reader
		if payload != nil {
			bodyReader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		if userToken != "" {
			req.Header.Set("x-acs-dingtalk-access-token", userToken)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		// 限流/服务端错误可重试
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("dingtalk api %s: status=%d body=%s", url, resp.StatusCode, redact(string(raw), c.appSecret))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("dingtalk api %s: status=%d body=%s", url, resp.StatusCode, redact(string(raw), c.appSecret))
		}
		if result != nil {
			if err := json.Unmarshal(raw, result); err != nil {
				return fmt.Errorf("decode response from %s: %w", url, err)
			}
		}
		return nil
	}
	return fmt.Errorf("dingtalk api %s: %w (after %d attempts)", url, lastErr, maxAttempts)
}

// redact 从错误信息里抹掉 secret，避免凭证随日志泄露。
func redact(s, secret string) string {
	if secret != "" {
		s = strings.ReplaceAll(s, secret, "[REDACTED]")
	}
	return s
}
