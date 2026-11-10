package repository

import (
	"context"
	"errors"
	"sort"
	"strings"
	"unicode"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *knowledgeRepository) GetKnowledgeNavigation(
	ctx context.Context,
	tenantID uint64,
	kbID string,
) (*types.KnowledgeNavigation, error) {
	n := &types.KnowledgeNavigation{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		Folders:         []string{},
		Orders:          map[string][]string{},
	}
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).First(n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return n, nil
	}
	return n, err
}

func validNavigationPath(path string, root bool) bool {
	return (root || path != "") && path == types.NormalizeKnowledgeFolderPath(path) &&
		!strings.ContainsAny(path, "<>") &&
		!strings.ContainsFunc(path, unicode.IsControl)
}
func withinFolder(path, parent string) bool {
	return path == parent || strings.HasPrefix(path, parent+"/")
}

// All folder rewrites, document moves and order changes share one transaction
// and one locked navigation row. Empty destinations and stale tabs cannot
// partially change a subtree or silently replace each other's ordering.
func (r *knowledgeRepository) MutateKnowledgeNavigation(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	m *types.KnowledgeNavigationMutation,
) (int64, error) {
	var affected int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		initial := types.KnowledgeNavigation{
			TenantID:        tenantID,
			KnowledgeBaseID: kbID,
			Folders:         []string{},
			Orders:          map[string][]string{},
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		var n types.KnowledgeNavigation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).
			First(&n).Error; err != nil {
			return err
		}
		if m.BaseRevision != nil && *m.BaseRevision != n.Revision {
			return apperrors.NewConflictError("目录已被修改，请刷新后重试")
		}
		var docs []types.KnowledgeTreeDocument
		if err := tx.Model(&types.Knowledge{}).
			Select("id, folder_path, title").
			Where("tenant_id = ? AND knowledge_base_id = ? AND parse_status <> ?",
				tenantID, kbID, types.ParseStatusDeleting).
			Find(&docs).Error; err != nil {
			return err
		}
		folders := map[string]bool{}
		addFolder := func(path string) {
			for path != "" {
				folders[path] = true
				path = types.KnowledgeFolderParent(path)
			}
		}
		for _, path := range n.Folders {
			addFolder(path)
		}
		for _, doc := range docs {
			addFolder(doc.FolderPath)
		}
		if n.Orders == nil {
			n.Orders = map[string][]string{}
		}
		switch m.Action {
		case "create_folder":
			if !validNavigationPath(m.To, false) {
				return apperrors.NewBadRequestError("文件夹路径不合法或超过限制")
			}
			if folders[m.To] {
				return apperrors.NewConflictError("文件夹已存在")
			}
			addFolder(m.To)
		case "delete_folder":
			if !validNavigationPath(m.From, false) || !folders[m.From] {
				return apperrors.NewNotFoundError("文件夹不存在")
			}
			for _, doc := range docs {
				if withinFolder(doc.FolderPath, m.From) {
					return apperrors.NewConflictError("请先移出文件夹内的文档，再删除文件夹")
				}
			}
			for path := range folders {
				if withinFolder(path, m.From) {
					delete(folders, path)
				}
			}
		case "move_folder":
			if !validNavigationPath(m.From, false) || !validNavigationPath(m.To, false) {
				return apperrors.NewBadRequestError("文件夹路径不合法或超过限制")
			}
			if !folders[m.From] {
				if m.AllowMerge {
					return nil
				}
				return apperrors.NewNotFoundError("文件夹不存在")
			}
			if m.To == m.From {
				return nil
			}
			if strings.HasPrefix(m.To, m.From+"/") {
				return apperrors.NewBadRequestError("不能移动到自身子目录")
			}
			if folders[m.To] && !m.AllowMerge {
				return apperrors.NewConflictError("目标文件夹已存在")
			}
			targets := map[string]string{}
			for path := range folders {
				if withinFolder(path, m.From) {
					target := m.To + strings.TrimPrefix(path, m.From)
					if !validNavigationPath(target, false) {
						return apperrors.NewBadRequestError("移动后子目录路径超过限制")
					}
					targets[path] = target
				}
			}
			for from := range targets {
				delete(folders, from)
			}
			for _, to := range targets {
				addFolder(to)
			}
			for i := range docs {
				if withinFolder(docs[i].FolderPath, m.From) {
					docs[i].FolderPath = m.To + strings.TrimPrefix(docs[i].FolderPath, m.From)
					result := tx.Model(&types.Knowledge{}).
						Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, docs[i].ID).
						UpdateColumn("folder_path", docs[i].FolderPath)
					if result.Error != nil {
						return result.Error
					}
					affected += result.RowsAffected
				}
			}
			rewritten := map[string][]string{}
			for parent, items := range n.Orders {
				newParent := parent
				if withinFolder(parent, m.From) {
					newParent = m.To + strings.TrimPrefix(parent, m.From)
				}
				for _, key := range items {
					if strings.HasPrefix(key, "f:") && withinFolder(strings.TrimPrefix(key, "f:"), m.From) {
						key = "f:" + m.To + strings.TrimPrefix(strings.TrimPrefix(key, "f:"), m.From)
					}
					rewritten[newParent] = append(rewritten[newParent], key)
				}
			}
			n.Orders = rewritten
		case "move_documents":
			if !validNavigationPath(m.To, true) {
				return apperrors.NewBadRequestError("文件夹路径不合法或超过限制")
			}
			if len(m.IDs) == 0 || len(m.IDs) > 200 {
				return apperrors.NewBadRequestError("每次移动需选择 1 到 200 篇文档")
			}
			lookup := map[string]int{}
			for i, d := range docs {
				lookup[d.ID] = i
			}
			seen := map[string]bool{}
			for _, id := range m.IDs {
				i, ok := lookup[id]
				if !ok || seen[id] {
					return apperrors.NewBadRequestError("文档不存在或不属于当前知识库")
				}
				seen[id] = true
				docs[i].FolderPath = m.To
			}
			result := tx.Model(&types.Knowledge{}).
				Where("tenant_id = ? AND knowledge_base_id = ? AND id IN ? AND parse_status <> ?",
					tenantID, kbID, m.IDs, types.ParseStatusDeleting).
				UpdateColumn("folder_path", m.To)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != int64(len(m.IDs)) {
				return apperrors.NewConflictError("文档已发生变化，请刷新重试")
			}
			affected = result.RowsAffected
			addFolder(m.To)
		case "reorder":
			if !validNavigationPath(m.From, true) || m.From != "" && !folders[m.From] {
				return apperrors.NewNotFoundError("文件夹不存在")
			}
		default:
			return apperrors.NewBadRequestError("未知目录操作")
		}
		// Prune stale IDs and ensure orders only contain current direct siblings.
		children := map[string][]string{}
		for path := range folders {
			parent := types.KnowledgeFolderParent(path)
			children[parent] = append(children[parent], "f:"+path)
		}
		for _, d := range docs {
			children[d.FolderPath] = append(children[d.FolderPath], "d:"+d.ID)
		}
		if m.Action == "reorder" {
			expected := children[m.From]
			if len(expected) != len(m.Items) {
				return apperrors.NewConflictError("目录内容已变化，请刷新后重新排序")
			}
			allowed := map[string]bool{}
			for _, key := range expected {
				allowed[key] = true
			}
			for _, key := range m.Items {
				if !allowed[key] {
					return apperrors.NewBadRequestError("排序必须包含当前文件夹全部直接子项且不能重复")
				}
				delete(allowed, key)
			}
			n.Orders[m.From] = append([]string{}, m.Items...)
		}
		clean := map[string][]string{}
		for parent, items := range children {
			allowed := map[string]bool{}
			for _, key := range items {
				allowed[key] = true
			}
			for _, key := range n.Orders[parent] {
				if allowed[key] {
					clean[parent] = append(clean[parent], key)
					delete(allowed, key)
				}
			}
			titles := map[string]string{}
			for _, doc := range docs {
				titles["d:"+doc.ID] = strings.ToLower(doc.Title)
			}
			sort.Slice(items, func(i, j int) bool {
				a, b := items[i], items[j]
				if strings.HasPrefix(a, "f:") != strings.HasPrefix(b, "f:") {
					return strings.HasPrefix(a, "f:")
				}
				if strings.HasPrefix(a, "f:") {
					return strings.ToLower(a) < strings.ToLower(b)
				}
				if titles[a] != titles[b] {
					return titles[a] < titles[b]
				}
				return a < b
			})
			for _, key := range items {
				if allowed[key] {
					clean[parent] = append(clean[parent], key)
				}
			}
		}
		n.Orders = clean
		n.Folders = []string{}
		for path := range folders {
			n.Folders = append(n.Folders, path)
		}
		sort.Strings(n.Folders)
		if len(n.Folders) > 5000 {
			return apperrors.NewBadRequestError("文件夹数量超过上限")
		}
		n.Revision++
		return tx.Model(&types.KnowledgeNavigation{}).
			Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).
			Select("revision", "folders", "orders").
			Updates(&n).
			Error
	})
	if err != nil {
		return 0, err
	}
	return affected, nil
}
