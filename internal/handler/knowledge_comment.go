package handler

import (
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func (h *KnowledgeHandler) ListKnowledgeComments(c *gin.Context) {
	_, ctx, err := h.resolveKnowledgeAndValidateKBAccess(c, c.Param("id"), types.OrgRoleViewer)
	if err != nil {
		c.Error(err)
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid offset"})
		return
	}
	page, err := h.kgService.ListKnowledgeComments(ctx, c.Param("id"), offset, c.DefaultQuery("status", "all"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": page})
}
func (h *KnowledgeHandler) CreateKnowledgeComment(c *gin.Context) {
	_, ctx, err := h.resolveKnowledgeAndValidateKBAccess(c, c.Param("id"), types.OrgRoleEditor)
	if err != nil {
		c.Error(err)
		return
	}
	var req types.KnowledgeCommentInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid comment"})
		return
	}
	name := ""
	if userID := types.CallerFromContext(ctx).UserID; userID != "" && !types.IsSyntheticUserID(userID) {
		user, err := h.userSvc.GetUserByID(ctx, userID)
		if err != nil {
			c.Error(err)
			return
		}
		if user != nil {
			name = user.Username
		}
	}
	row, err := h.kgService.CreateKnowledgeComment(ctx, c.Param("id"), &req, name)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}
func (h *KnowledgeHandler) MutateKnowledgeComment(c *gin.Context) {
	_, ctx, err := h.resolveKnowledgeAndValidateKBAccess(c, c.Param("id"), types.OrgRoleEditor)
	if err != nil {
		c.Error(err)
		return
	}
	var req types.KnowledgeCommentMutation
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid comment mutation"})
		return
	}
	if err := h.kgService.MutateKnowledgeComment(ctx, c.Param("id"), c.Param("comment_id"), &req); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
