package types

import "time"

// Comments follow the document ID across KB/workspace moves. Authorization is
// always resolved from the persisted knowledge row; no independent share scope.
type KnowledgeComment struct {
	ID             string              `json:"id" gorm:"primaryKey;type:varchar(36)"`
	KnowledgeID    string              `json:"knowledge_id" gorm:"index;type:varchar(36);not null"`
	ParentID       string              `json:"parent_id" gorm:"type:varchar(36);not null;default:''"`
	AuthorID       string              `json:"author_id" gorm:"type:varchar(36);not null"`
	AuthorName     string              `json:"author_name" gorm:"not null"`
	Body           string              `json:"body" gorm:"not null"`
	Quote          string              `json:"quote" gorm:"not null;default:''"`
	Prefix         string              `json:"prefix" gorm:"not null;default:''"`
	Suffix         string              `json:"suffix" gorm:"not null;default:''"`
	ContentVersion int                 `json:"content_version"`
	Resolved       bool                `json:"resolved"`
	Deleted        bool                `json:"deleted"`
	Revision       int                 `json:"revision" gorm:"not null;default:1"`
	CreatedAt      time.Time           `json:"created_at"`
	Replies        []*KnowledgeComment `json:"replies,omitempty" gorm:"-"`
}

func (KnowledgeComment) TableName() string { return "knowledge_comments" }

type KnowledgeCommentInput struct {
	ClientID       string `json:"client_id"`
	ParentID       string `json:"parent_id"`
	Body           string `json:"body"`
	Quote          string `json:"quote"`
	Prefix         string `json:"prefix"`
	Suffix         string `json:"suffix"`
	ContentVersion int    `json:"content_version"`
}
type KnowledgeCommentMutation struct {
	Action       string `json:"action"`
	BaseRevision int    `json:"base_revision"`
	Resolved     bool   `json:"resolved"`
}
type KnowledgeCommentPage struct {
	Items      []*KnowledgeComment `json:"items"`
	NextOffset int                 `json:"next_offset"`
	HasMore    bool                `json:"has_more"`
}
