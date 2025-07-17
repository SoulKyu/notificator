package services

import (
	"context"
	"fmt"

	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
)

type CommentService struct {
	db *database.GormDB
}

func NewCommentService(db *database.GormDB) *CommentService {
	return &CommentService{db: db}
}

func (s *CommentService) AddComment(ctx context.Context, alertKey, userID, content string) (*models.CommentWithUser, error) {
	if len(content) > 2000 {
		return nil, fmt.Errorf("comment too long (max 2000 characters)")
	}

	return s.db.CreateComment(alertKey, userID, content)
}

func (s *CommentService) GetComments(ctx context.Context, alertKey string) ([]models.CommentWithUser, error) {
	return s.db.GetComments(alertKey)
}

func (s *CommentService) DeleteComment(ctx context.Context, commentID, userID string) error {
	return s.db.DeleteComment(commentID, userID)
}
