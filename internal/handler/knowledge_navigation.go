package handler

import (
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *KnowledgeHandler) UpdateKnowledgeNavigation(c *gin.Context) {
	var req types.KnowledgeNavigationMutation
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid navigation request"})
		return
	}
	kbID, tenantID, err := h.requireKnowledgeWriteAccess(c, c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	ctx := types.WithExecutionTenant(c.Request.Context(), tenantID)
	affected, err := h.kgService.UpdateKnowledgeNavigation(ctx, kbID, &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"affected_count": affected}})
}
