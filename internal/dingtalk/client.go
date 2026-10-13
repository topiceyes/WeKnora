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
	// 普通可重试错误（429/5xx/网络抖动）的重试预算。
	maxAttempts       = 3
	defaultRetryDelay = 200 * time.Millisecond

	// 平台限流（errcode 88 / subcode 90002）单独一条预算：这是钉钉
	// 全平台共享的 QPS 配额被压满，窗口能从几秒一路续到几十秒，默认
	// 3 次 × 200ms 的预算等不出来。退避 3s/6s/12s/24s（合计约 45s）。
	maxFlowLimitAttempts = 5
	flowLimitBaseDelay   = 3 * time.Second
	retryDelayCap        = 30 * time.Second

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
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpireIn     int64  `json:"expireIn"` // 秒
	CorpID       string `json:"corpId"`
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
	UnionID   string `json:"unionId"`
	Nick      string `json:"nick"`
	AvatarURL string `json:"avatarUrl"`
	Mobile    string `json:"mobile"`
	Email     string `json:"email"`
	OpenID    string `json:"openId"`
	StateCode string `json:"stateCode"` // 手机号国家码
}

// GetUserMe 用个人 userToken 获取当前用户信息。
func (c *Client) GetUserMe(ctx context.Context, userToken string) (*ContactUserMe, error) {
	var resp ContactUserMe
	if err := c.do(ctx, http.MethodGet, apiBase+"/v1.0/contact/users/me", nil, userToken, &resp); err != nil {
		return nil, fmt.Errorf("get users/me: %w", err)
	}
	return &resp, nil
}

// GetUserIDByUnionID 用 unionId 换企业内部 userid（POST /topapi/user/getbyunionid，需企业 token）。
// 注意：该接口没有 /v2/ 变体，v2 路径会返回 errcode=22「不合法ApiName」。
func (c *Client) GetUserIDByUnionID(ctx context.Context, unionID string) (string, error) {
	token, err := c.GetCorpAccessToken(ctx)
	if err != nil {
		return "", err
	}
	u := fmt.Sprintf("%s/topapi/user/getbyunionid?access_token=%s", corpAPIBase, token)
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

// GetUserDetailByUserID 拉取单个成员详情（POST /topapi/v2/user/get，需企业 token）。
// 登录链路用：首登建号需要工号，而 users/me / getbyunionid 都不返回。
// 响应字段与 user/list 同形，复用 DeptUserDetail。
func (c *Client) GetUserDetailByUserID(ctx context.Context, userID string) (*DeptUserDetail, error) {
	token, err := c.GetCorpAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/topapi/v2/user/get?access_token=%s", corpAPIBase, token)
	var resp struct {
		ErrCode int            `json:"errcode"`
		ErrMsg  string         `json:"errmsg"`
		Result  DeptUserDetail `json:"result"`
	}
	if err := c.do(ctx, http.MethodPost, u, map[string]string{"userid": userID}, "", &resp); err != nil {
		return nil, err
	}
	if resp.ErrCode != 0 {
		return nil, fmt.Errorf("user/get(%s) failed: errcode=%d errmsg=%s", userID, resp.ErrCode, redact(resp.ErrMsg, c.appSecret))
	}
	return &resp.Result, nil
}

// ---------------------------------------------------------------------------
// 通讯录同步：部门列表 + 部门成员分页
// ---------------------------------------------------------------------------

// SubDepartment 子部门信息（组织架构展示用）。
type SubDepartment struct {
	DeptID   int64  `json:"dept_id"`
	Name     string `json:"name"`
	ParentID int64  `json:"parent_id"`
}

// ListSubDepartments 拉取某部门的直接子部门（POST /topapi/v2/department/listsub，
// 需企业 token）。size 单页上限 100；终止条件除短页外，还包括「本页没有
// 带来新部门」——listsub 的 offset 分页行为没有明确文档保证，若接口
// 忽略 offset 且某部门直属子部门 ≥100，每次都会返回同一份全量，
// 只靠短页判断会死循环。
func (c *Client) ListSubDepartments(ctx context.Context, parentID int64) ([]SubDepartment, error) {
	token, err := c.GetCorpAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/topapi/v2/department/listsub?access_token=%s", corpAPIBase, token)
	const pageSize = 100
	var all []SubDepartment
	seen := map[int64]bool{}
	for offset := 0; ; offset += pageSize {
		var resp struct {
			ErrCode int             `json:"errcode"`
			ErrMsg  string          `json:"errmsg"`
			Result  []SubDepartment `json:"result"`
		}
		body := map[string]int64{"dept_id": parentID, "size": pageSize, "offset": int64(offset)}
		if err := c.do(ctx, http.MethodPost, u, body, "", &resp); err != nil {
			return nil, err
		}
		if resp.ErrCode != 0 {
			return nil, fmt.Errorf("listsub(dept=%d) failed: errcode=%d errmsg=%s", parentID, resp.ErrCode, redact(resp.ErrMsg, c.appSecret))
		}
		added := 0
		for _, d := range resp.Result {
			if !seen[d.DeptID] {
				seen[d.DeptID] = true
				all = append(all, d)
				added++
			}
		}
		if len(resp.Result) < pageSize || added == 0 {
			return all, nil
		}
	}
}

// ListDepartmentTree 从根部门递归拉取全量部门（带名称，供组织架构展示）。
// 返回扁平列表（不含根部门本身），前端按 parent_id 组树。
func (c *Client) ListDepartmentTree(ctx context.Context) ([]SubDepartment, error) {
	var all []SubDepartment
	queue := []int64{1}
	seen := map[int64]bool{1: true}
	for len(queue) > 0 {
		deptID := queue[0]
		queue = queue[1:]
		subs, err := c.ListSubDepartments(ctx, deptID)
		if err != nil {
			return nil, fmt.Errorf("list sub departments (dept=%d): %w", deptID, err)
		}
		for _, d := range subs {
			if seen[d.DeptID] {
				continue
			}
			seen[d.DeptID] = true
			all = append(all, d)
			queue = append(queue, d.DeptID)
		}
	}
	return all, nil
}

// DeptUserDetail 部门成员详情（对账用）。UnionID 用于把登录时创建的
// "union:" 占位绑定升级成真实企业 userid。
type DeptUserDetail struct {
	UserID     string  `json:"userid"`
	UnionID    string  `json:"unionid"`
	Name       string  `json:"name"`
	Mobile     string  `json:"mobile"`
	Email      string  `json:"email"`
	Title      string  `json:"title"`
	JobNumber  string  `json:"job_number"`
	DeptIDList []int64 `json:"dept_id_list"`
}

// DeptUserPage 部门成员分页结果。
type DeptUserPage struct {
	List       []DeptUserDetail
	NextCursor int64
	HasMore    bool
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

	// attempts 在遇到限流时会被抬高到 maxFlowLimitAttempts；退避时长由
	// 各失败分支显式设置，避免「顶部统一翻倍」和「分支重置」互相打架。
	attempts := maxAttempts
	flowLimitHits := 0
	delay := defaultRetryDelay
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
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
			delay = min(delay*2, retryDelayCap)
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			delay = min(delay*2, retryDelayCap)
			continue
		}

		// 限流/服务端错误可重试
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("dingtalk api %s: status=%d body=%s", url, resp.StatusCode, redact(string(raw), c.appSecret))
			delay = min(delay*2, retryDelayCap)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("dingtalk api %s: status=%d body=%s", url, resp.StatusCode, redact(string(raw), c.appSecret))
		}
		// 钉钉旧版 oapi 接口的限流是 HTTP 200 + body errcode=88
		// （subcode 90002，全平台 QPS 限流，整点前后高发），响应里直接
		// 给出限流窗口，但配额被持续压满时窗口会不断续期。这里抬高
		// 重试预算并按 3s/6s/12s/24s 递增退避，骑过整点的饱和期。
		var flowProbe struct {
			ErrCode int `json:"errcode"`
		}
		if json.Unmarshal(raw, &flowProbe) == nil && flowProbe.ErrCode == 88 {
			lastErr = fmt.Errorf("dingtalk api %s: flow limited: %s", url, redact(string(raw), c.appSecret))
			flowLimitHits++
			attempts = maxFlowLimitAttempts
			delay = min(flowLimitBaseDelay<<(flowLimitHits-1), retryDelayCap)
			continue
		}
		if result != nil {
			if err := json.Unmarshal(raw, result); err != nil {
				return fmt.Errorf("decode response from %s: %w", url, err)
			}
		}
		return nil
	}
	return fmt.Errorf("dingtalk api %s: %w (after %d attempts)", url, lastErr, attempts)
}

// redact 从错误信息里抹掉 secret，避免凭证随日志泄露。
func redact(s, secret string) string {
	if secret != "" {
		s = strings.ReplaceAll(s, secret, "[REDACTED]")
	}
	return s
}
