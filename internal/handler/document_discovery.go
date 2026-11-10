package handler

import (
	"net/http"
	"strconv"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func (h *KnowledgeHandler) SearchDocuments(c *gin.Context) {
	_, kb, tenant, _, err := h.validateKnowledgeBaseAccess(c)
	if err != nil {
		c.Error(err)
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid offset"})
		return
	}
	ctx := types.WithExecutionTenant(c.Request.Context(), tenant)
	if grant, ok := resolvedKBAccess(c, kb, types.OrgRoleViewer); ok {
		ctx = grant.Context(c.Request.Context())
	}
	page, err := h.kgService.SearchDocuments(ctx, kb, c.Query("query"), offset, "")
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": page})
}
func (h *KnowledgeHandler) DocumentBacklinks(c *gin.Context) {
	k, ctx, err := h.resolveKnowledgeAndValidateKBAccess(c, c.Param("id"), types.OrgRoleViewer)
	if err != nil {
		c.Error(err)
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid offset"})
		return
	}
	page, err := h.kgService.SearchDocuments(ctx, k.KnowledgeBaseID, "", offset, k.ID)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": page})
}
