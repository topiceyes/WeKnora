package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// UserAdminHandler 平台级用户管理端点（二开 fork 新增）。
// 路由层统一挂 SystemAdmin 门控（见 router.RegisterUserAdminRoutes），
// handler 内不再重复鉴权。
type UserAdminHandler struct {
	userSvc   interfaces.UserService
	userRepo  interfaces.UserRepository
	tokenRepo interfaces.AuthTokenRepository
}

// NewUserAdminHandler 构造 handler。userSvc 负责用户读写与系统管理员
// 计数，userRepo 负责用户列表分页，tokenRepo 负责禁用后的会话吊销。
func NewUserAdminHandler(userSvc interfaces.UserService, userRepo interfaces.UserRepository, tokenRepo interfaces.AuthTokenRepository) *UserAdminHandler {
	return &UserAdminHandler{userSvc: userSvc, userRepo: userRepo, tokenRepo: tokenRepo}
}

// ListUsers godoc
// @Summary      列出全部用户
// @Description  分页返回平台所有用户（SystemAdmin 仅 JWT 会话可用）。
// @Description  供管理界面展示账号、邮箱、激活状态、是否系统管理员。
// @Tags         系统管理-用户
// @Accept       json
// @Produce      json
// @Param        page       query  int  false  "页码，从 1 开始"  default(1)
// @Param        page_size  query  int  false  "每页条数"          default(20)
// @Success      200  {object}  map[string]interface{}  "{success:true, data:{list, total, page, page_size}}"
// @Failure      403  {object}  map[string]interface{}  "非系统管理员"
// @Security     Bearer
// @Router       /system/users [get]
func (h *UserAdminHandler) ListUsers(c *gin.Context) {
	ctx := c.Request.Context()

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize

	users, err := h.userRepo.ListUsers(ctx, offset, pageSize)
	if err != nil {
		logger.Errorf(ctx, "ListUsers: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list users"})
		return
	}
	total, err := h.userRepo.CountUsers(ctx)
	if err != nil {
		logger.Errorf(ctx, "CountUsers: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count users"})
		return
	}

	list := make([]gin.H, 0, len(users))
	for _, u := range users {
		list = append(list, gin.H{
			"id":              u.ID,
			"username":        u.Username,
			"email":           u.Email,
			"avatar":          u.Avatar,
			"is_active":       u.IsActive,
			"is_system_admin": u.IsSystemAdmin,
			"created_at":      u.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"list":      list,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// UpdateUserStatusRequest 是 PUT /system/users/:id/status 的请求体。
// IsActive 用指针 + binding:"required"，让缺失字段与显式 false 可区分
// （gin 的 required 对指针按 nil 判定）。
type UpdateUserStatusRequest struct {
	IsActive *bool `json:"is_active" binding:"required"`
}

// UpdateUserStatus godoc
// @Summary      禁用/启用用户
// @Description  设置用户的 is_active 状态（SystemAdmin 仅 JWT 会话可用）。
// @Description  禁用时同步吊销该用户全部 access/refresh token 踢下线——Login
// @Description  的 is_active 检查只拦新登录，已签发的 JWT 必须显式吊销。
// @Description  保护规则：不能操作自己；不能禁用最后一个 active 的系统管理员
// @Description  （与 RevokeSystemAdmin 的 last-admin 规则一致，防止平台失管）。
// @Description  幂等：状态未变化时不写库、不吊销会话，直接返回当前状态。
// @Tags         系统管理-用户
// @Accept       json
// @Produce      json
// @Param        id       path  string                   true  "目标用户 ID"
// @Param        request  body  UpdateUserStatusRequest  true  "目标状态"
// @Success      200  {object}  map[string]interface{}  "{success:true, data:{id, is_active}}"
// @Failure      400  {object}  map[string]interface{}  "请求非法 / 操作自己 / 最后一个系统管理员"
// @Failure      403  {object}  map[string]interface{}  "非系统管理员"
// @Failure      404  {object}  map[string]interface{}  "用户不存在"
// @Security     Bearer
// @Router       /system/users/{id}/status [put]
func (h *UserAdminHandler) UpdateUserStatus(c *gin.Context) {
	ctx := c.Request.Context()

	targetID := strings.TrimSpace(c.Param("id"))
	if targetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User id is required"})
		return
	}

	var req UpdateUserStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: is_active (boolean) is required"})
		return
	}

	callerID, _ := types.UserIDFromContext(ctx)
	if callerID == targetID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot change your own active status"})
		return
	}

	user, err := h.userSvc.GetUserByID(ctx, targetID)
	if err != nil || user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// 幂等：状态未变化时不写库、不踢下线，直接返回当前状态。
	if user.IsActive == *req.IsActive {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    gin.H{"id": user.ID, "is_active": user.IsActive},
		})
		return
	}

	// 禁用系统管理员的额外保护：不允许禁用最后一个 active 的系统管理员，
	// 否则平台将无人可执行系统级管理（与 RevokeSystemAdmin 的 last-admin
	// 规则一致）。调用者本身必然是系统管理员（组级门控），所以无需再校验
	// 操作者身份。
	if user.IsSystemAdmin && !*req.IsActive {
		admins, _, err := h.userSvc.ListSystemAdmins(ctx, 0, 0)
		if err != nil {
			logger.Errorf(ctx, "UpdateUserStatus: list system admins failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user status"})
			return
		}
		activeAdmins := 0
		for _, admin := range admins {
			if admin.IsActive {
				activeAdmins++
			}
		}
		if activeAdmins <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Cannot disable the last active system administrator",
			})
			return
		}
	}

	user.IsActive = *req.IsActive
	if err := h.userSvc.UpdateUser(ctx, user); err != nil {
		logger.Errorf(ctx, "UpdateUserStatus: update user %s failed: %v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user status"})
		return
	}

	// 禁用时踢下线：吊销该用户全部 access/refresh token，否则已签发的 JWT
	// 在过期前仍可使用。顺序与 AdminResetPassword 一致（先落库再吊销）；
	// 吊销失败返回 500，让操作者知道会话仍未失效。
	if !*req.IsActive {
		if err := h.tokenRepo.RevokeTokensByUserID(ctx, user.ID); err != nil {
			logger.Errorf(ctx, "UpdateUserStatus: revoke tokens for user %s failed: %v", user.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "User disabled but failed to revoke sessions",
			})
			return
		}
	}

	logger.Infof(ctx, "User %s is_active set to %v by system admin %s", user.ID, *req.IsActive, callerID)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"id": user.ID, "is_active": user.IsActive},
	})
}
