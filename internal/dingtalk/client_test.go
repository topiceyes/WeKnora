package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 测试用 client 指向 httptest server（覆盖 baseURL 常量不可行，故用 helper 注入）。
// 由于 baseURL 是常量，这里通过替换 http.Client.Transport 来劫持请求到测试 server。
func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewClient("test-key", "test-secret", "corp-1")
	c.http = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &rewriteTransport{
			target: srv.URL,
			base:   http.DefaultTransport,
		},
	}
	return c
}

// rewriteTransport 把所有请求改写到测试 server（保留 path+query）。
type rewriteTransport struct {
	target string
	base   http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 保留 path 和 RawQuery，把 scheme+host 换成测试 server
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = strings.TrimPrefix(rt.target, "http://")
	return rt.base.RoundTrip(cloned)
}

func TestGetCorpAccessTokenCachesAndParses(t *testing.T) {
	var calls int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gettoken" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		atomic.AddInt32(&calls, 1)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errcode":      0,
			"access_token": "corp-token-abc",
			"expire_in":    7200,
		})
	}))

	tok, err := c.GetCorpAccessToken(context.Background())
	if err != nil {
		t.Fatalf("GetCorpAccessToken: %v", err)
	}
	if tok != "corp-token-abc" {
		t.Fatalf("got %q", tok)
	}
	// 第二次应命中缓存，不再请求
	if _, err := c.GetCorpAccessToken(context.Background()); err != nil {
		t.Fatalf("cached call: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 http call (cached), got %d", calls)
	}
}

func TestGetCorpAccessTokenError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errcode": 40078,
			"errmsg":  "invalid appsecret test-secret",
		})
	}))
	_, err := c.GetCorpAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "40078") {
		t.Fatalf("error should carry errcode, got %v", err)
	}
	// secret 必须被脱敏
	if strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("secret leaked in error: %v", err)
	}
}

func TestGetUserAccessToken(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/oauth2/userAccessToken" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["code"] != "authcode-1" || body["grantType"] != "authorization_code" {
			t.Fatalf("bad body %+v", body)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken": "user-token-xyz",
			"expireIn":    7200,
			"corpId":      "corp-1",
		})
	}))
	resp, err := c.GetUserAccessToken(context.Background(), "authcode-1")
	if err != nil {
		t.Fatalf("GetUserAccessToken: %v", err)
	}
	if resp.AccessToken != "user-token-xyz" {
		t.Fatalf("got %+v", resp)
	}
}

func TestGetUserMeSendsUserTokenHeader(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-acs-dingtalk-access-token"); got != "user-token-xyz" {
			t.Fatalf("missing user token header, got %q", got)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"unionId": "union-1",
			"nick":    "张三",
			"email":   "zhangsan@corp.com",
			"mobile":  "13800000000",
		})
	}))
	me, err := c.GetUserMe(context.Background(), "user-token-xyz")
	if err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if me.UnionID != "union-1" || me.Email != "zhangsan@corp.com" {
		t.Fatalf("got %+v", me)
	}
}

func TestGetUserIDByUnionID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gettoken":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0, "access_token": "corp-token", "expire_in": 7200,
			})
		case "/topapi/user/getbyunionid":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0,
				"result":  map[string]string{"userid": "ding-user-9"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	uid, err := c.GetUserIDByUnionID(context.Background(), "union-1")
	if err != nil {
		t.Fatalf("GetUserIDByUnionID: %v", err)
	}
	if uid != "ding-user-9" {
		t.Fatalf("got %q", uid)
	}
}

func TestGetUserDetailByUserID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gettoken":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0, "access_token": "corp-token", "expire_in": 7200,
			})
		case "/topapi/v2/user/get":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["userid"] != "ding-user-9" {
				t.Fatalf("bad body %+v", body)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0,
				"result": map[string]interface{}{
					"userid":     "ding-user-9",
					"job_number": "8823",
					"name":       "张三",
					"email":      "zhangsan@corp.com",
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	detail, err := c.GetUserDetailByUserID(context.Background(), "ding-user-9")
	if err != nil {
		t.Fatalf("GetUserDetailByUserID: %v", err)
	}
	if detail.JobNumber != "8823" || detail.Name != "张三" {
		t.Fatalf("got %+v", detail)
	}
}

func TestListDeptUsersPagination(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gettoken":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0, "access_token": "corp-token", "expire_in": 7200,
			})
		case "/topapi/v2/user/list":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0,
				"result": map[string]interface{}{
					"list": []map[string]interface{}{
						{"userid": "u1", "name": "A", "email": "a@c.com"},
						{"userid": "u2", "name": "B"},
					},
					"next_cursor": 100,
					"has_more":    true,
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	page, err := c.ListDeptUsers(context.Background(), 1, 0, 50)
	if err != nil {
		t.Fatalf("ListDeptUsers: %v", err)
	}
	if len(page.List) != 2 || !page.HasMore || page.NextCursor != 100 {
		t.Fatalf("got %+v", page)
	}
}

func TestRetryOn5xx(t *testing.T) {
	var calls int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"errcode": 0, "access_token": "tok", "expire_in": 7200,
		})
	}))
	tok, err := c.GetCorpAccessToken(context.Background())
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if tok != "tok" || atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("calls=%d tok=%q", calls, tok)
	}
}
