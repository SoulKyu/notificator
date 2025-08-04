package services

import (
	"context"

	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
)

type AcknowledgmentService struct {
	db *database.GormDB
}

func NewAcknowledgmentService(db *database.GormDB) *AcknowledgmentService {
	return &AcknowledgmentService{db: db}
}

func (s *AcknowledgmentService) AddAcknowledgment(ctx context.Context, alertKey, userID, reason string) (*models.AcknowledgmentWithUser, error) {
	return s.db.CreateAcknowledgment(alertKey, userID, reason)
}

func (s *AcknowledgmentService) GetAcknowledgments(ctx context.Context, alertKey string) ([]models.AcknowledgmentWithUser, error) {
	return s.db.GetAcknowledgments(alertKey)
}

func (s *AcknowledgmentService) DeleteAcknowledgment(ctx context.Context, alertKey, userID string) error {
	return s.db.DeleteAcknowledgment(alertKey, userID)
}

func (s *AcknowledgmentService) GetAllAcknowledgedAlerts(ctx context.Context) (map[string]models.AcknowledgmentWithUser, error) {
	return s.db.GetAllAcknowledgedAlerts()
}
