package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notificator/internal/backend/database"
	"notificator/internal/backend/models"
	alertpb "notificator/internal/backend/proto/alert"
	authpb "notificator/internal/backend/proto/auth"
	mainmodels "notificator/internal/models"
)

type AuthServiceGorm struct {
	authpb.UnimplementedAuthServiceServer
	db           *database.GormDB
	oauthService *OAuthService
}

func NewAuthServiceGorm(db *database.GormDB, oauthService *OAuthService) *AuthServiceGorm {
	return &AuthServiceGorm{
		db:           db,
		oauthService: oauthService,
	}
}

func (s *AuthServiceGorm) Register(ctx context.Context, req *authpb.RegisterRequest) (*authpb.RegisterResponse, error) {
	if req.Username == "" || req.Password == "" {
		return &authpb.RegisterResponse{
			Success: false,
			Message: "Username and password are required",
		}, nil
	}

	if len(req.Password) < 4 {
		return &authpb.RegisterResponse{
			Success: false,
			Message: "Password must be at least 4 characters long",
		}, nil
	}

	// Check if user already exists
	_, err := s.db.GetUserByUsername(req.Username)
	if err == nil {
		return &authpb.RegisterResponse{
			Success: false,
			Message: "Username already exists",
		}, nil
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		return &authpb.RegisterResponse{
			Success: false,
			Message: "Internal server error",
		}, nil
	}

	// Create user
	user, err := s.db.CreateUser(req.Username, req.Email, string(passwordHash))
	if err != nil {
		log.Printf("Error creating user: %v", err)
		return &authpb.RegisterResponse{
			Success: false,
			Message: "Failed to create user",
		}, nil
	}

	return &authpb.RegisterResponse{
		Success: true,
		Message: "User created successfully",
		UserId:  user.ID,
	}, nil
}

// Login implements the Login RPC method
func (s *AuthServiceGorm) Login(ctx context.Context, req *authpb.LoginRequest) (*authpb.LoginResponse, error) {
	if req.Username == "" || req.Password == "" {
		return &authpb.LoginResponse{
			Success: false,
			Message: "Username and password are required",
		}, nil
	}

	// Get user by username
	user, err := s.db.GetUserByUsername(req.Username)
	if err != nil {
		return &authpb.LoginResponse{
			Success: false,
			Message: "Invalid credentials",
		}, nil
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return &authpb.LoginResponse{
			Success: false,
			Message: "Invalid credentials",
		}, nil
	}

	// Generate session ID
	sessionID, err := generateSessionID()
	if err != nil {
		log.Printf("Error generating session ID: %v", err)
		return &authpb.LoginResponse{
			Success: false,
			Message: "Internal server error",
		}, nil
	}

	expiresAt := time.Now().Add(24 * time.Hour)
	if err := s.db.CreateSession(user.ID, sessionID, expiresAt); err != nil {
		log.Printf("Error creating session: %v", err)
		return &authpb.LoginResponse{
			Success: false,
			Message: "Failed to create session",
		}, nil
	}

	// Update last login
	if err := s.db.UpdateLastLogin(user.ID); err != nil {
		log.Printf("Error updating last login: %v", err)
		// Don't fail the login for this
	}

	return &authpb.LoginResponse{
		Success:   true,
		Message:   "Login successful",
		SessionId: sessionID,
		User: &authpb.User{
			Id:        user.ID,
			Username:  user.Username,
			Email:     user.Email,
			CreatedAt: timestamppb.New(user.CreatedAt),
		},
	}, nil
}

// Logout implements the Logout RPC method
func (s *AuthServiceGorm) Logout(ctx context.Context, req *authpb.LogoutRequest) (*authpb.LogoutResponse, error) {
	if req.SessionId == "" {
		return &authpb.LogoutResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Delete session
	if err := s.db.DeleteSession(req.SessionId); err != nil {
		return &authpb.LogoutResponse{
			Success: false,
			Message: "Failed to logout",
		}, nil
	}

	return &authpb.LogoutResponse{
		Success: true,
		Message: "Logout successful",
	}, nil
}

// ValidateSession implements the ValidateSession RPC method
func (s *AuthServiceGorm) ValidateSession(ctx context.Context, req *authpb.ValidateSessionRequest) (*authpb.ValidateSessionResponse, error) {
	if req.SessionId == "" {
		return &authpb.ValidateSessionResponse{
			Valid:   false,
			Message: "Session ID is required",
		}, nil
	}

	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &authpb.ValidateSessionResponse{
			Valid:   false,
			Message: "Invalid session",
		}, nil
	}

	return &authpb.ValidateSessionResponse{
		Valid:   true,
		Message: "Session is valid",
		User: &authpb.User{
			Id:        user.ID,
			Username:  user.Username,
			Email:     user.Email,
			CreatedAt: timestamppb.New(user.CreatedAt),
		},
	}, nil
}

// GetProfile implements the GetProfile RPC method
func (s *AuthServiceGorm) GetProfile(ctx context.Context, req *authpb.GetProfileRequest) (*authpb.GetProfileResponse, error) {
	if req.SessionId == "" {
		return &authpb.GetProfileResponse{
			User: nil,
		}, nil
	}

	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &authpb.GetProfileResponse{
			User: nil,
		}, nil
	}

	return &authpb.GetProfileResponse{
		User: &authpb.User{
			Id:        user.ID,
			Username:  user.Username,
			Email:     user.Email,
			CreatedAt: timestamppb.New(user.CreatedAt),
		},
	}, nil
}

// SearchUsers implements the SearchUsers RPC method
func (s *AuthServiceGorm) SearchUsers(ctx context.Context, req *authpb.SearchUsersRequest) (*authpb.SearchUsersResponse, error) {
	// Default limit if not specified
	limit := req.Limit
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	// Search users by username prefix
	users, err := s.db.SearchUsers(req.Query, int(limit))
	if err != nil {
		log.Printf("Error searching users: %v", err)
		return &authpb.SearchUsersResponse{
			Users:      []*authpb.User{},
			TotalCount: 0,
		}, nil
	}

	// Convert to proto users
	protoUsers := make([]*authpb.User, len(users))
	for i, user := range users {
		protoUsers[i] = &authpb.User{
			Id:        user.ID,
			Username:  user.Username,
			Email:     user.Email,
			CreatedAt: timestamppb.New(user.CreatedAt),
		}
		if user.LastLogin != nil {
			protoUsers[i].LastLogin = timestamppb.New(*user.LastLogin)
		}
	}

	return &authpb.SearchUsersResponse{
		Users:      protoUsers,
		TotalCount: int32(len(users)),
	}, nil
}

// ValidateSessionByID is a helper method for internal use
func (s *AuthServiceGorm) ValidateSessionByID(sessionID string) (*authpb.User, error) {
	user, err := s.db.GetUserBySession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("invalid session")
	}

	return &authpb.User{
		Id:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		CreatedAt: timestamppb.New(user.CreatedAt),
	}, nil
}

// Subscription represents an active subscription to alert updates
type Subscription struct {
	AlertKey string
	UserID   string
	Stream   grpc.ServerStreamingServer[alertpb.AlertUpdate]
}

// AlertServiceGorm implements the AlertService gRPC service
type AlertServiceGorm struct {
	alertpb.UnimplementedAlertServiceServer
	db            *database.GormDB
	subscriptions map[string][]*Subscription // alertKey -> []*Subscription
	subsMutex     sync.RWMutex
}

func NewAlertServiceGorm(db *database.GormDB) *AlertServiceGorm {
	return &AlertServiceGorm{
		db:            db,
		subscriptions: make(map[string][]*Subscription),
	}
}

// AddComment implements the AddComment RPC method
func (s *AlertServiceGorm) AddComment(ctx context.Context, req *alertpb.AddCommentRequest) (*alertpb.AddCommentResponse, error) {
	if req.SessionId == "" {
		return &alertpb.AddCommentResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.AlertKey == "" {
		return &alertpb.AddCommentResponse{
			Success: false,
			Message: "Alert key is required",
		}, nil
	}

	if req.Content == "" {
		return &alertpb.AddCommentResponse{
			Success: false,
			Message: "Comment content is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.AddCommentResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Create comment
	comment, err := s.db.CreateComment(req.AlertKey, user.ID, req.Content)
	if err != nil {
		log.Printf("Error creating comment: %v", err)
		return &alertpb.AddCommentResponse{
			Success: false,
			Message: "Failed to create comment",
		}, nil
	}

	// Create the protobuf comment
	protoComment := &alertpb.Comment{
		Id:        comment.ID,
		AlertKey:  comment.AlertKey,
		UserId:    comment.UserID,
		Username:  comment.Username,
		Content:   comment.Content,
		CreatedAt: timestamppb.New(comment.CreatedAt),
	}

	// Broadcast to subscribers
	s.broadcastUpdate(req.AlertKey, &alertpb.AlertUpdate{
		AlertKey:   req.AlertKey,
		UpdateType: alertpb.UpdateType_COMMENT_ADDED,
		UpdateData: &alertpb.AlertUpdate_Comment{Comment: protoComment},
		Timestamp:  timestamppb.Now(),
	})

	return &alertpb.AddCommentResponse{
		Success: true,
		Message: "Comment added successfully",
		Comment: protoComment,
	}, nil
}

// GetComments implements the GetComments RPC method
func (s *AlertServiceGorm) GetComments(ctx context.Context, req *alertpb.GetCommentsRequest) (*alertpb.GetCommentsResponse, error) {
	if req.AlertKey == "" {
		return &alertpb.GetCommentsResponse{
			Comments: []*alertpb.Comment{},
			Count:    0,
		}, nil
	}

	comments, err := s.db.GetComments(req.AlertKey)
	if err != nil {
		log.Printf("Error getting comments: %v", err)
		return &alertpb.GetCommentsResponse{
			Comments: []*alertpb.Comment{},
			Count:    0,
		}, nil
	}

	// Convert to protobuf format
	var pbComments []*alertpb.Comment
	for _, comment := range comments {
		pbComments = append(pbComments, &alertpb.Comment{
			Id:        comment.ID,
			AlertKey:  comment.AlertKey,
			UserId:    comment.UserID,
			Username:  comment.Username,
			Content:   comment.Content,
			CreatedAt: timestamppb.New(comment.CreatedAt),
		})
	}

	return &alertpb.GetCommentsResponse{
		Comments: pbComments,
		Count:    int32(len(pbComments)),
	}, nil
}

// DeleteComment implements the DeleteComment RPC method
func (s *AlertServiceGorm) DeleteComment(ctx context.Context, req *alertpb.DeleteCommentRequest) (*alertpb.DeleteCommentResponse, error) {
	if req.SessionId == "" {
		return &alertpb.DeleteCommentResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.CommentId == "" {
		return &alertpb.DeleteCommentResponse{
			Success: false,
			Message: "Comment ID is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.DeleteCommentResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Delete comment
	if err := s.db.DeleteComment(req.CommentId, user.ID); err != nil {
		log.Printf("Error deleting comment: %v", err)
		return &alertpb.DeleteCommentResponse{
			Success: false,
			Message: "Failed to delete comment or unauthorized",
		}, nil
	}

	return &alertpb.DeleteCommentResponse{
		Success: true,
		Message: "Comment deleted successfully",
	}, nil
}

// AddAcknowledgment implements the AddAcknowledgment RPC method
func (s *AlertServiceGorm) AddAcknowledgment(ctx context.Context, req *alertpb.AddAcknowledgmentRequest) (*alertpb.AddAcknowledgmentResponse, error) {
	if req.SessionId == "" {
		return &alertpb.AddAcknowledgmentResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.AlertKey == "" {
		return &alertpb.AddAcknowledgmentResponse{
			Success: false,
			Message: "Alert key is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.AddAcknowledgmentResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Create acknowledgment
	ack, err := s.db.CreateAcknowledgment(req.AlertKey, user.ID, req.Reason)
	if err != nil {
		log.Printf("Error creating acknowledgment: %v", err)
		return &alertpb.AddAcknowledgmentResponse{
			Success: false,
			Message: "Failed to create acknowledgment",
		}, nil
	}

	// Create the protobuf acknowledgment
	protoAck := &alertpb.Acknowledgment{
		Id:        ack.ID,
		AlertKey:  ack.AlertKey,
		UserId:    ack.UserID,
		Username:  ack.Username,
		Reason:    ack.Reason,
		CreatedAt: timestamppb.New(ack.CreatedAt),
	}

	// Broadcast to subscribers
	s.broadcastUpdate(req.AlertKey, &alertpb.AlertUpdate{
		AlertKey:   req.AlertKey,
		UpdateType: alertpb.UpdateType_ACKNOWLEDGMENT_ADDED,
		UpdateData: &alertpb.AlertUpdate_Acknowledgment{Acknowledgment: protoAck},
		Timestamp:  timestamppb.Now(),
	})

	return &alertpb.AddAcknowledgmentResponse{
		Success:        true,
		Message:        "Acknowledgment added successfully",
		Acknowledgment: protoAck,
	}, nil
}

// GetAcknowledgments implements the GetAcknowledgments RPC method
func (s *AlertServiceGorm) GetAcknowledgments(ctx context.Context, req *alertpb.GetAcknowledgmentsRequest) (*alertpb.GetAcknowledgmentsResponse, error) {
	if req.AlertKey == "" {
		return &alertpb.GetAcknowledgmentsResponse{
			Acknowledgments: []*alertpb.Acknowledgment{},
			Count:           0,
		}, nil
	}

	acks, err := s.db.GetAcknowledgments(req.AlertKey)
	if err != nil {
		log.Printf("Error getting acknowledgments: %v", err)
		return &alertpb.GetAcknowledgmentsResponse{
			Acknowledgments: []*alertpb.Acknowledgment{},
			Count:           0,
		}, nil
	}

	// Convert to protobuf format
	var pbAcks []*alertpb.Acknowledgment
	for _, ack := range acks {
		pbAcks = append(pbAcks, &alertpb.Acknowledgment{
			Id:        ack.ID,
			AlertKey:  ack.AlertKey,
			UserId:    ack.UserID,
			Username:  ack.Username,
			Reason:    ack.Reason,
			CreatedAt: timestamppb.New(ack.CreatedAt),
		})
	}

	return &alertpb.GetAcknowledgmentsResponse{
		Acknowledgments: pbAcks,
		Count:           int32(len(pbAcks)),
	}, nil
}

// GetAllAcknowledgedAlerts implements the GetAllAcknowledgedAlerts RPC method
func (s *AlertServiceGorm) GetAllAcknowledgedAlerts(ctx context.Context, req *alertpb.GetAllAcknowledgedAlertsRequest) (*alertpb.GetAllAcknowledgedAlertsResponse, error) {
	acknowledgedAlerts, err := s.db.GetAllAcknowledgedAlerts()
	if err != nil {
		log.Printf("Error getting all acknowledged alerts: %v", err)
		return &alertpb.GetAllAcknowledgedAlertsResponse{
			AcknowledgedAlerts: make(map[string]*alertpb.Acknowledgment),
			Count:              0,
		}, nil
	}

	// Convert to protobuf format
	pbAcknowledgedAlerts := make(map[string]*alertpb.Acknowledgment)
	for alertKey, ack := range acknowledgedAlerts {
		pbAcknowledgedAlerts[alertKey] = &alertpb.Acknowledgment{
			Id:        ack.ID,
			AlertKey:  ack.AlertKey,
			UserId:    ack.UserID,
			Username:  ack.Username,
			Reason:    ack.Reason,
			CreatedAt: timestamppb.New(ack.CreatedAt),
		}
	}

	return &alertpb.GetAllAcknowledgedAlertsResponse{
		AcknowledgedAlerts: pbAcknowledgedAlerts,
		Count:              int32(len(pbAcknowledgedAlerts)),
	}, nil
}

// DeleteAcknowledgment implements the DeleteAcknowledgment RPC method
func (s *AlertServiceGorm) DeleteAcknowledgment(ctx context.Context, req *alertpb.DeleteAcknowledgmentRequest) (*alertpb.DeleteAcknowledgmentResponse, error) {
	if req.SessionId == "" {
		return &alertpb.DeleteAcknowledgmentResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.AlertKey == "" {
		return &alertpb.DeleteAcknowledgmentResponse{
			Success: false,
			Message: "Alert key is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.DeleteAcknowledgmentResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Delete acknowledgment
	if err := s.db.DeleteAcknowledgment(req.AlertKey, user.ID); err != nil {
		log.Printf("Error deleting acknowledgment: %v", err)
		return &alertpb.DeleteAcknowledgmentResponse{
			Success: false,
			Message: "Failed to delete acknowledgment or unauthorized",
		}, nil
	}

	// Broadcast deletion to subscribers
	s.broadcastUpdate(req.AlertKey, &alertpb.AlertUpdate{
		AlertKey:   req.AlertKey,
		UpdateType: alertpb.UpdateType_ACKNOWLEDGMENT_DELETED,
		UpdateData: &alertpb.AlertUpdate_DeletedAcknowledgmentId{DeletedAcknowledgmentId: req.AlertKey},
		Timestamp:  timestamppb.Now(),
	})

	return &alertpb.DeleteAcknowledgmentResponse{
		Success: true,
		Message: "Acknowledgment deleted successfully",
	}, nil
}

// SubscribeToAlertUpdates implements the streaming RPC for real-time updates
func (s *AlertServiceGorm) SubscribeToAlertUpdates(req *alertpb.SubscribeToAlertUpdatesRequest, stream grpc.ServerStreamingServer[alertpb.AlertUpdate]) error {
	if req.SessionId == "" {
		return fmt.Errorf("session ID is required")
	}

	if req.AlertKey == "" {
		return fmt.Errorf("alert key is required")
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return fmt.Errorf("invalid session")
	}

	log.Printf("User %s subscribing to updates for alert %s", user.Username, req.AlertKey)

	// Add subscription
	sub := &Subscription{
		AlertKey: req.AlertKey,
		UserID:   user.ID,
		Stream:   stream,
	}

	s.addSubscription(sub)
	defer s.removeSubscription(sub)

	// Send initial connection confirmation
	initialUpdate := &alertpb.AlertUpdate{
		AlertKey:   req.AlertKey,
		UpdateType: alertpb.UpdateType_UNKNOWN_UPDATE,
		Timestamp:  timestamppb.Now(),
	}

	if err := stream.Send(initialUpdate); err != nil {
		log.Printf("Failed to send initial update: %v", err)
		return err
	}

	// Keep the stream alive
	<-stream.Context().Done()
	log.Printf("User %s unsubscribed from alert %s", user.Username, req.AlertKey)
	return nil
}

// addSubscription adds a new subscription to the manager
func (s *AlertServiceGorm) addSubscription(sub *Subscription) {
	s.subsMutex.Lock()
	defer s.subsMutex.Unlock()

	s.subscriptions[sub.AlertKey] = append(s.subscriptions[sub.AlertKey], sub)
	log.Printf("Added subscription for alert %s, total: %d", sub.AlertKey, len(s.subscriptions[sub.AlertKey]))
}

// removeSubscription removes a subscription from the manager
func (s *AlertServiceGorm) removeSubscription(sub *Subscription) {
	s.subsMutex.Lock()
	defer s.subsMutex.Unlock()

	subs := s.subscriptions[sub.AlertKey]
	for i, existingSub := range subs {
		if existingSub == sub {
			s.subscriptions[sub.AlertKey] = append(subs[:i], subs[i+1:]...)
			break
		}
	}

	if len(s.subscriptions[sub.AlertKey]) == 0 {
		delete(s.subscriptions, sub.AlertKey)
	}

	log.Printf("Removed subscription for alert %s", sub.AlertKey)
}

// broadcastUpdate sends an update to all subscribers of an alert
func (s *AlertServiceGorm) broadcastUpdate(alertKey string, update *alertpb.AlertUpdate) {
	s.subsMutex.RLock()
	subs := s.subscriptions[alertKey]
	s.subsMutex.RUnlock()

	if len(subs) == 0 {
		return
	}

	log.Printf("Broadcasting update to %d subscribers for alert %s", len(subs), alertKey)

	// Send to all subscribers
	for _, sub := range subs {
		go func(sub *Subscription) {
			if err := sub.Stream.Send(update); err != nil {
				log.Printf("Failed to send update to subscriber: %v", err)
				// Remove failed subscription
				s.removeSubscription(sub)
			}
		}(sub)
	}
}

// GetUserColorPreferences implements the GetUserColorPreferences RPC method
func (s *AlertServiceGorm) GetUserColorPreferences(ctx context.Context, req *alertpb.GetUserColorPreferencesRequest) (*alertpb.GetUserColorPreferencesResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetUserColorPreferencesResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetUserColorPreferencesResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get user color preferences
	preferences, err := s.db.GetUserColorPreferences(user.ID)
	if err != nil {
		log.Printf("Error getting user color preferences: %v", err)
		return &alertpb.GetUserColorPreferencesResponse{
			Success: false,
			Message: "Failed to get color preferences",
		}, nil
	}

	// Convert to protobuf format
	var pbPreferences []*alertpb.UserColorPreference
	for _, pref := range preferences {
		conditions, err := pref.GetLabelConditions()
		if err != nil {
			log.Printf("Error getting label conditions for preference %s: %v", pref.ID, err)
			continue
		}

		pbPreferences = append(pbPreferences, &alertpb.UserColorPreference{
			Id:                 pref.ID,
			UserId:             pref.UserID,
			LabelConditions:    conditions,
			Color:              pref.Color,
			ColorType:          pref.ColorType,
			Priority:           int32(pref.Priority),
			BgLightnessFactor:  float32(pref.BgLightnessFactor),
			TextDarknessFactor: float32(pref.TextDarknessFactor),
			CreatedAt:          timestamppb.New(pref.CreatedAt),
			UpdatedAt:          timestamppb.New(pref.UpdatedAt),
		})
	}

	return &alertpb.GetUserColorPreferencesResponse{
		Preferences: pbPreferences,
		Success:     true,
		Message:     "Color preferences retrieved successfully",
	}, nil
}

// SaveUserColorPreferences implements the SaveUserColorPreferences RPC method
func (s *AlertServiceGorm) SaveUserColorPreferences(ctx context.Context, req *alertpb.SaveUserColorPreferencesRequest) (*alertpb.SaveUserColorPreferencesResponse, error) {
	if req.SessionId == "" {
		return &alertpb.SaveUserColorPreferencesResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.SaveUserColorPreferencesResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Convert protobuf preferences to model preferences
	var modelPreferences []mainmodels.UserColorPreference
	for _, pbPref := range req.Preferences {
		modelPref := mainmodels.UserColorPreference{
			ID:                 pbPref.Id,
			UserID:             user.ID,
			Color:              pbPref.Color,
			ColorType:          pbPref.ColorType,
			Priority:           int(pbPref.Priority),
			BgLightnessFactor:  float32(pbPref.BgLightnessFactor),
			TextDarknessFactor: float32(pbPref.TextDarknessFactor),
		}

		// Set label conditions
		if err := modelPref.SetLabelConditions(pbPref.LabelConditions); err != nil {
			log.Printf("Error setting label conditions: %v", err)
			return &alertpb.SaveUserColorPreferencesResponse{
				Success: false,
				Message: "Invalid label conditions format",
			}, nil
		}

		// Generate ID if not provided
		if modelPref.ID == "" {
			modelPref.ID = generateUUID()
		}

		modelPreferences = append(modelPreferences, modelPref)
	}

	// Save preferences
	if err := s.db.SaveUserColorPreferences(user.ID, modelPreferences); err != nil {
		log.Printf("Error saving user color preferences: %v", err)
		return &alertpb.SaveUserColorPreferencesResponse{
			Success: false,
			Message: "Failed to save color preferences",
		}, nil
	}

	return &alertpb.SaveUserColorPreferencesResponse{
		Success: true,
		Message: "Color preferences saved successfully",
	}, nil
}

func (s *AlertServiceGorm) DeleteUserColorPreference(ctx context.Context, req *alertpb.DeleteUserColorPreferenceRequest) (*alertpb.DeleteUserColorPreferenceResponse, error) {
	if req.SessionId == "" {
		return &alertpb.DeleteUserColorPreferenceResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.PreferenceId == "" {
		return &alertpb.DeleteUserColorPreferenceResponse{
			Success: false,
			Message: "Preference ID is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.DeleteUserColorPreferenceResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Delete preference
	if err := s.db.DeleteUserColorPreference(user.ID, req.PreferenceId); err != nil {
		log.Printf("Error deleting color preference: %v", err)
		return &alertpb.DeleteUserColorPreferenceResponse{
			Success: false,
			Message: "Failed to delete color preference or unauthorized",
		}, nil
	}

	return &alertpb.DeleteUserColorPreferenceResponse{
		Success: true,
		Message: "Color preference deleted successfully",
	}, nil
}

// CreateResolvedAlert implements the CreateResolvedAlert RPC method
func (s *AlertServiceGorm) CreateResolvedAlert(ctx context.Context, req *alertpb.CreateResolvedAlertRequest) (*alertpb.CreateResolvedAlertResponse, error) {
	if req.Fingerprint == "" {
		return &alertpb.CreateResolvedAlertResponse{
			Success: false,
			Message: "Fingerprint is required",
		}, nil
	}

	if req.Source == "" {
		return &alertpb.CreateResolvedAlertResponse{
			Success: false,
			Message: "Source is required",
		}, nil
	}

	if len(req.AlertData) == 0 {
		return &alertpb.CreateResolvedAlertResponse{
			Success: false,
			Message: "Alert data is required",
		}, nil
	}

	// Default TTL to 24 hours if not specified
	ttlHours := int(req.TtlHours)
	if ttlHours <= 0 {
		ttlHours = 24
	}

	// Create resolved alert in database
	resolvedAlert, err := s.db.CreateResolvedAlert(
		req.Fingerprint,
		req.Source,
		req.AlertData,
		req.Comments,
		req.Acknowledgments,
		ttlHours,
	)
	if err != nil {
		log.Printf("Error creating resolved alert: %v", err)
		return &alertpb.CreateResolvedAlertResponse{
			Success: false,
			Message: "Failed to create resolved alert",
		}, nil
	}

	// Convert to protobuf message
	pbResolvedAlert := &alertpb.ResolvedAlertInfo{
		Id:              resolvedAlert.ID,
		Fingerprint:     resolvedAlert.Fingerprint,
		AlertData:       []byte(resolvedAlert.AlertData),
		Comments:        []byte(resolvedAlert.Comments),
		Acknowledgments: []byte(resolvedAlert.Acknowledgments),
		ResolvedAt:      timestamppb.New(resolvedAlert.ResolvedAt),
		ExpiresAt:       timestamppb.New(resolvedAlert.ExpiresAt),
		Source:          resolvedAlert.Source,
		CreatedAt:       timestamppb.New(resolvedAlert.CreatedAt),
		UpdatedAt:       timestamppb.New(resolvedAlert.UpdatedAt),
	}

	// Broadcast resolved alert update to subscribers
	go s.broadcastResolvedAlertUpdate(req.Fingerprint, &alertpb.ResolvedAlertUpdate{
		Fingerprint:   req.Fingerprint,
		UpdateType:    alertpb.ResolvedAlertUpdateType_RESOLVED_ALERT_CREATED,
		ResolvedAlert: pbResolvedAlert,
		Timestamp:     timestamppb.Now(),
	})

	return &alertpb.CreateResolvedAlertResponse{
		Success:       true,
		ResolvedAlert: pbResolvedAlert,
		Message:       "Resolved alert created successfully",
	}, nil
}

// GetResolvedAlerts implements the GetResolvedAlerts RPC method
func (s *AlertServiceGorm) GetResolvedAlerts(ctx context.Context, req *alertpb.GetResolvedAlertsRequest) (*alertpb.GetResolvedAlertsResponse, error) {
	limit := int(req.Limit)
	offset := int(req.Offset)

	// Default limit to 100 if not specified
	if limit <= 0 {
		limit = 100
	}

	resolvedAlerts, err := s.db.GetResolvedAlerts(limit, offset)
	if err != nil {
		log.Printf("Error fetching resolved alerts: %v", err)
		return &alertpb.GetResolvedAlertsResponse{
			Success: false,
			Message: "Failed to fetch resolved alerts",
		}, nil
	}

	// Get total count
	totalCount, err := s.db.GetResolvedAlertsCount()
	if err != nil {
		log.Printf("Error getting resolved alerts count: %v", err)
		totalCount = int64(len(resolvedAlerts))
	}

	// Convert to protobuf messages
	pbResolvedAlerts := make([]*alertpb.ResolvedAlertInfo, len(resolvedAlerts))
	for i, resolvedAlert := range resolvedAlerts {
		pbResolvedAlerts[i] = &alertpb.ResolvedAlertInfo{
			Id:              resolvedAlert.ID,
			Fingerprint:     resolvedAlert.Fingerprint,
			AlertData:       []byte(resolvedAlert.AlertData),
			Comments:        []byte(resolvedAlert.Comments),
			Acknowledgments: []byte(resolvedAlert.Acknowledgments),
			ResolvedAt:      timestamppb.New(resolvedAlert.ResolvedAt),
			ExpiresAt:       timestamppb.New(resolvedAlert.ExpiresAt),
			Source:          resolvedAlert.Source,
			CreatedAt:       timestamppb.New(resolvedAlert.CreatedAt),
			UpdatedAt:       timestamppb.New(resolvedAlert.UpdatedAt),
		}
	}

	return &alertpb.GetResolvedAlertsResponse{
		ResolvedAlerts: pbResolvedAlerts,
		TotalCount:     int32(totalCount),
		Success:        true,
		Message:        fmt.Sprintf("Found %d resolved alerts", len(resolvedAlerts)),
	}, nil
}

// RemoveAllResolvedAlerts implements the RemoveAllResolvedAlerts RPC method
func (s *AlertServiceGorm) RemoveAllResolvedAlerts(ctx context.Context, req *alertpb.RemoveAllResolvedAlertsRequest) (*alertpb.RemoveAllResolvedAlertsResponse, error) {
	log.Printf("RemoveAllResolvedAlerts: Attempting to remove all resolved alerts")

	removedCount, err := s.db.RemoveAllResolvedAlerts()
	if err != nil {
		log.Printf("Error removing all resolved alerts: %v", err)
		return &alertpb.RemoveAllResolvedAlertsResponse{
			Success: false,
			Message: "Failed to remove resolved alerts",
		}, nil
	}

	log.Printf("RemoveAllResolvedAlerts: Successfully removed %d resolved alerts", removedCount)

	return &alertpb.RemoveAllResolvedAlertsResponse{
		Success:      true,
		RemovedCount: int32(removedCount),
		Message:      fmt.Sprintf("Successfully removed %d resolved alerts", removedCount),
	}, nil
}

// GetResolvedAlert implements the GetResolvedAlert RPC method
func (s *AlertServiceGorm) GetResolvedAlert(ctx context.Context, req *alertpb.GetResolvedAlertRequest) (*alertpb.GetResolvedAlertResponse, error) {
	if req.Fingerprint == "" {
		return &alertpb.GetResolvedAlertResponse{
			Success: false,
			Message: "Fingerprint is required",
		}, nil
	}

	resolvedAlert, err := s.db.GetResolvedAlert(req.Fingerprint)
	if err != nil {
		return &alertpb.GetResolvedAlertResponse{
			Success: false,
			Message: "Resolved alert not found",
		}, nil
	}

	pbResolvedAlert := &alertpb.ResolvedAlertInfo{
		Id:              resolvedAlert.ID,
		Fingerprint:     resolvedAlert.Fingerprint,
		AlertData:       []byte(resolvedAlert.AlertData),
		Comments:        []byte(resolvedAlert.Comments),
		Acknowledgments: []byte(resolvedAlert.Acknowledgments),
		ResolvedAt:      timestamppb.New(resolvedAlert.ResolvedAt),
		ExpiresAt:       timestamppb.New(resolvedAlert.ExpiresAt),
		Source:          resolvedAlert.Source,
		CreatedAt:       timestamppb.New(resolvedAlert.CreatedAt),
		UpdatedAt:       timestamppb.New(resolvedAlert.UpdatedAt),
	}

	return &alertpb.GetResolvedAlertResponse{
		Success:       true,
		ResolvedAlert: pbResolvedAlert,
		Message:       "Resolved alert found",
	}, nil
}

// StreamResolvedAlertUpdates implements the StreamResolvedAlertUpdates RPC method
func (s *AlertServiceGorm) StreamResolvedAlertUpdates(req *alertpb.StreamResolvedAlertUpdatesRequest, stream grpc.ServerStreamingServer[alertpb.ResolvedAlertUpdate]) error {
	if req.SessionId == "" {
		return fmt.Errorf("session ID is required")
	}

	// Validate session
	_, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return fmt.Errorf("invalid session")
	}

	// Create subscription for resolved alert updates
	sub := &ResolvedAlertSubscription{
		SessionID: req.SessionId,
		Stream:    stream,
		Done:      make(chan bool),
	}

	// Add subscription
	s.addResolvedAlertSubscription(sub)
	defer s.removeResolvedAlertSubscription(sub)

	// Wait for stream to close
	<-sub.Done

	return nil
}

// Helper methods for resolved alert subscriptions
type ResolvedAlertSubscription struct {
	SessionID string
	Stream    grpc.ServerStreamingServer[alertpb.ResolvedAlertUpdate]
	Done      chan bool
}

// Add resolved alert subscription tracking
var (
	resolvedAlertSubscriptions      = make(map[string][]*ResolvedAlertSubscription)
	resolvedAlertSubscriptionsMutex sync.RWMutex
)

func (s *AlertServiceGorm) addResolvedAlertSubscription(sub *ResolvedAlertSubscription) {
	resolvedAlertSubscriptionsMutex.Lock()
	defer resolvedAlertSubscriptionsMutex.Unlock()

	resolvedAlertSubscriptions["global"] = append(resolvedAlertSubscriptions["global"], sub)
}

func (s *AlertServiceGorm) removeResolvedAlertSubscription(sub *ResolvedAlertSubscription) {
	resolvedAlertSubscriptionsMutex.Lock()
	defer resolvedAlertSubscriptionsMutex.Unlock()

	subs := resolvedAlertSubscriptions["global"]
	for i, s := range subs {
		if s == sub {
			resolvedAlertSubscriptions["global"] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

func (s *AlertServiceGorm) broadcastResolvedAlertUpdate(fingerprint string, update *alertpb.ResolvedAlertUpdate) {
	resolvedAlertSubscriptionsMutex.RLock()
	defer resolvedAlertSubscriptionsMutex.RUnlock()

	subs := resolvedAlertSubscriptions["global"]
	for _, sub := range subs {
		go func(s *ResolvedAlertSubscription) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Recovered from panic in resolved alert broadcast: %v", r)
					close(s.Done)
				}
			}()

			if err := s.Stream.Send(update); err != nil {
				log.Printf("Error sending resolved alert update to subscriber %s: %v", s.SessionID, err)
				close(s.Done)
			}
		}(sub)
	}
}

// Helper function to generate secure session ID
func generateSessionID() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GetOAuthConfig implements the GetOAuthConfig RPC method
func (s *AuthServiceGorm) GetOAuthConfig(ctx context.Context, req *authpb.GetOAuthConfigRequest) (*authpb.GetOAuthConfigResponse, error) {
	// If OAuth service is not available, return disabled state
	if s.oauthService == nil {
		return &authpb.GetOAuthConfigResponse{
			Enabled:            false,
			DisableClassicAuth: false,
			Providers:          []*authpb.OAuthProvider{},
		}, nil
	}

	config := s.oauthService.GetConfig()
	if config == nil || !config.Enabled {
		return &authpb.GetOAuthConfigResponse{
			Enabled:            false,
			DisableClassicAuth: false,
			Providers:          []*authpb.OAuthProvider{},
		}, nil
	}

	// Convert providers to protobuf format
	var pbProviders []*authpb.OAuthProvider
	for name, provider := range config.Providers {
		if provider.Enabled {
			// Generate display name from provider name if not set
			displayName := name
			switch name {
			case "google":
				displayName = "Google"
			case "github":
				displayName = "GitHub"
			case "microsoft":
				displayName = "Microsoft"
			default:
				// Capitalize first letter for other providers
				if len(name) > 0 {
					displayName = strings.ToUpper(string(name[0])) + name[1:]
				}
			}

			pbProviders = append(pbProviders, &authpb.OAuthProvider{
				Name:        name,
				DisplayName: displayName,
				Enabled:     provider.Enabled,
			})
		}
	}

	return &authpb.GetOAuthConfigResponse{
		Enabled:            config.Enabled,
		DisableClassicAuth: config.DisableClassicAuth,
		Providers:          pbProviders,
	}, nil
}

func (s *AuthServiceGorm) GetOAuthProviders(ctx context.Context, req *authpb.GetOAuthProvidersRequest) (*authpb.GetOAuthProvidersResponse, error) {
	// If OAuth service is not available, return empty providers
	if s.oauthService == nil {
		return &authpb.GetOAuthProvidersResponse{
			Providers: []*authpb.OAuthProvider{},
		}, nil
	}

	config := s.oauthService.GetConfig()
	if config == nil || !config.Enabled {
		return &authpb.GetOAuthProvidersResponse{
			Providers: []*authpb.OAuthProvider{},
		}, nil
	}

	// Convert providers to protobuf format
	var pbProviders []*authpb.OAuthProvider
	for name, provider := range config.Providers {
		if provider.Enabled {
			// Generate display name from provider name if not set
			displayName := name
			switch name {
			case "google":
				displayName = "Google"
			case "github":
				displayName = "GitHub"
			case "microsoft":
				displayName = "Microsoft"
			default:
				// Capitalize first letter for other providers
				if len(name) > 0 {
					displayName = strings.ToUpper(string(name[0])) + name[1:]
				}
			}

			pbProviders = append(pbProviders, &authpb.OAuthProvider{
				Name:        name,
				DisplayName: displayName,
				Enabled:     provider.Enabled,
			})
		}
	}

	return &authpb.GetOAuthProvidersResponse{
		Providers: pbProviders,
	}, nil
}

// GetOAuthAuthURL implements the GetOAuthAuthURL RPC method
func (s *AuthServiceGorm) GetOAuthAuthURL(ctx context.Context, req *authpb.OAuthAuthURLRequest) (*authpb.OAuthAuthURLResponse, error) {
	if req.Provider == "" {
		return &authpb.OAuthAuthURLResponse{
			Success: false,
			Error:   "Provider is required",
		}, nil
	}

	if req.State == "" {
		return &authpb.OAuthAuthURLResponse{
			Success: false,
			Error:   "State is required",
		}, nil
	}

	// Check if OAuth service is available
	if s.oauthService == nil {
		log.Printf("OAuth service not available for GetOAuthAuthURL")
		return &authpb.OAuthAuthURLResponse{
			Success: false,
			Error:   "OAuth service not configured",
		}, nil
	}

	// Get auth URL from OAuth service
	authURL, err := s.oauthService.GetAuthURL(req.Provider, req.State)
	if err != nil {
		log.Printf("Failed to get OAuth auth URL: %v", err)
		return &authpb.OAuthAuthURLResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to generate auth URL: %v", err),
		}, nil
	}

	log.Printf("Generated OAuth auth URL for provider %s", req.Provider)
	return &authpb.OAuthAuthURLResponse{
		Success: true,
		AuthUrl: authURL,
	}, nil
}

// OAuthCallback implements the OAuthCallback RPC method
func (s *AuthServiceGorm) OAuthCallback(ctx context.Context, req *authpb.OAuthCallbackRequest) (*authpb.LoginResponse, error) {
	if req.Provider == "" {
		return &authpb.LoginResponse{
			Success: false,
			Message: "Provider is required",
		}, nil
	}

	if req.Code == "" {
		return &authpb.LoginResponse{
			Success: false,
			Message: "Authorization code is required",
		}, nil
	}

	if req.State == "" {
		return &authpb.LoginResponse{
			Success: false,
			Message: "State parameter is required",
		}, nil
	}

	// Check if OAuth service is available
	if s.oauthService == nil {
		log.Printf("OAuth service not available for OAuthCallback")
		return &authpb.LoginResponse{
			Success: false,
			Message: "OAuth service not configured",
		}, nil
	}

	// Exchange code for token
	token, err := s.oauthService.ExchangeCodeForToken(req.Provider, req.Code, req.State)
	if err != nil {
		log.Printf("Failed to exchange OAuth code for token: %v", err)
		return &authpb.LoginResponse{
			Success: false,
			Message: "Failed to exchange authorization code",
			Error:   err.Error(),
		}, nil
	}

	// Get user info from OAuth provider
	userInfo, err := s.oauthService.GetUserInfo(req.Provider, token)
	if err != nil {
		log.Printf("Failed to get OAuth user info: %v", err)
		return &authpb.LoginResponse{
			Success: false,
			Message: "Failed to get user information",
			Error:   err.Error(),
		}, nil
	}

	// Create or update OAuth user
	user, err := s.oauthService.CreateOrUpdateOAuthUser(req.Provider, userInfo)
	if err != nil {
		log.Printf("Failed to create/update OAuth user: %v", err)
		return &authpb.LoginResponse{
			Success: false,
			Message: "Failed to create user account",
			Error:   err.Error(),
		}, nil
	}

	// Generate session ID
	sessionID, err := generateSessionID()
	if err != nil {
		log.Printf("Error generating session ID for OAuth user: %v", err)
		return &authpb.LoginResponse{
			Success: false,
			Message: "Internal server error",
		}, nil
	}

	// Create session
	expiresAt := time.Now().Add(24 * time.Hour)
	if err := s.db.CreateSession(user.ID, sessionID, expiresAt); err != nil {
		log.Printf("Error creating session for OAuth user: %v", err)
		return &authpb.LoginResponse{
			Success: false,
			Message: "Failed to create session",
		}, nil
	}

	// Update last login
	if err := s.db.UpdateLastLogin(user.ID); err != nil {
		log.Printf("Error updating last login for OAuth user: %v", err)
		// Don't fail the login for this
	}

	log.Printf("OAuth login successful for user %s via provider %s", user.Username, req.Provider)

	return &authpb.LoginResponse{
		Success:   true,
		Message:   "OAuth login successful",
		SessionId: sessionID,
		User: &authpb.User{
			Id:            user.ID,
			Username:      user.Username,
			Email:         user.Email,
			CreatedAt:     timestamppb.New(user.CreatedAt),
			OauthProvider: req.Provider,
			OauthId:       userInfo.ID,
		},
		UserId:   user.ID,
		Username: user.Username,
		Email:    user.Email,
	}, nil
}

// GetUserGroups implements the GetUserGroups RPC method
func (s *AuthServiceGorm) GetUserGroups(ctx context.Context, req *authpb.GetUserGroupsRequest) (*authpb.GetUserGroupsResponse, error) {
	if req.UserId == "" {
		return &authpb.GetUserGroupsResponse{
			Groups: []*authpb.UserGroup{},
		}, nil
	}

	// Get user groups from database
	groups, err := s.db.GetUserGroups(req.UserId)
	if err != nil {
		log.Printf("Failed to get user groups for user %s: %v", req.UserId, err)
		return &authpb.GetUserGroupsResponse{
			Groups: []*authpb.UserGroup{},
		}, nil
	}

	// Convert to protobuf format
	var pbGroups []*authpb.UserGroup
	for _, group := range groups {
		permissionsStr := ""
		if group.Permissions != nil {
			permissionsStr = string(group.Permissions)
		}

		pbGroups = append(pbGroups, &authpb.UserGroup{
			Id:          group.ID,
			Name:        group.GroupName,
			Provider:    group.Provider,
			Type:        group.GroupType,
			Role:        "", // UserGroup model doesn't have Role field
			Permissions: permissionsStr,
		})
	}

	log.Printf("Retrieved %d groups for user %s", len(pbGroups), req.UserId)
	return &authpb.GetUserGroupsResponse{
		Groups: pbGroups,
	}, nil
}

// SyncUserGroups implements the SyncUserGroups RPC method
func (s *AuthServiceGorm) SyncUserGroups(ctx context.Context, req *authpb.SyncUserGroupsRequest) (*authpb.SyncUserGroupsResponse, error) {
	if req.UserId == "" {
		return &authpb.SyncUserGroupsResponse{
			Success: false,
			Error:   "User ID is required",
		}, nil
	}

	if req.Provider == "" {
		return &authpb.SyncUserGroupsResponse{
			Success: false,
			Error:   "Provider is required",
		}, nil
	}

	// Check if OAuth service is available
	if s.oauthService == nil {
		log.Printf("OAuth service not available for SyncUserGroups")
		return &authpb.SyncUserGroupsResponse{
			Success: false,
			Error:   "OAuth service not configured",
		}, nil
	}

	// Get user by ID to verify it exists and get OAuth info
	user, err := s.db.GetUserByID(req.UserId)
	if err != nil {
		log.Printf("Failed to get user %s for group sync: %v", req.UserId, err)
		return &authpb.SyncUserGroupsResponse{
			Success: false,
			Error:   "User not found",
		}, nil
	}

	// Verify this is an OAuth user for the specified provider
	if user.OAuthProvider == nil || *user.OAuthProvider != req.Provider {
		return &authpb.SyncUserGroupsResponse{
			Success: false,
			Error:   "User is not authenticated with the specified OAuth provider",
		}, nil
	}

	// Get the user's OAuth token for group sync
	oauthToken, err := s.db.GetOAuthToken(req.UserId, req.Provider)
	if err != nil {
		log.Printf("Failed to get OAuth token for user %s: %v", req.UserId, err)
		return &authpb.SyncUserGroupsResponse{
			Success: false,
			Error:   "OAuth token not found or expired",
		}, nil
	}

	// Convert stored token to oauth2.Token format
	token := &oauth2.Token{
		AccessToken:  oauthToken.AccessToken,
		RefreshToken: oauthToken.RefreshToken,
		TokenType:    oauthToken.TokenType,
		Expiry:       *oauthToken.ExpiresAt,
	}

	// Get user info with groups from OAuth provider
	userInfo, err := s.oauthService.GetUserInfo(req.Provider, token)
	if err != nil {
		log.Printf("Failed to get user info for group sync: %v", err)
		return &authpb.SyncUserGroupsResponse{
			Success: false,
			Error:   "Failed to retrieve user information from provider",
		}, nil
	}

	// Sync groups to database
	if len(userInfo.Groups) > 0 {
		err = s.db.SyncUserGroups(req.UserId, req.Provider, userInfo.Groups)
		if err != nil {
			log.Printf("Failed to sync user groups: %v", err)
			return &authpb.SyncUserGroupsResponse{
				Success: false,
				Error:   "Failed to update user groups",
			}, nil
		}
	}

	log.Printf("Successfully synced %d groups for user %s from provider %s", len(userInfo.Groups), req.UserId, req.Provider)
	return &authpb.SyncUserGroupsResponse{
		Success:      true,
		GroupsSynced: int32(len(userInfo.Groups)),
	}, nil
}

// GetUserHiddenAlerts implements the GetUserHiddenAlerts RPC method
func (s *AlertServiceGorm) GetUserHiddenAlerts(ctx context.Context, req *alertpb.GetUserHiddenAlertsRequest) (*alertpb.GetUserHiddenAlertsResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetUserHiddenAlertsResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetUserHiddenAlertsResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get hidden alerts from database
	hiddenAlerts, err := s.db.GetUserHiddenAlerts(user.ID)
	if err != nil {
		log.Printf("Error getting hidden alerts for user %s: %v", user.ID, err)
		return &alertpb.GetUserHiddenAlertsResponse{
			Success: false,
			Message: "Failed to get hidden alerts",
		}, nil
	}

	// Convert to protobuf format
	var pbHiddenAlerts []*alertpb.UserHiddenAlert
	for _, hiddenAlert := range hiddenAlerts {
		pbHiddenAlerts = append(pbHiddenAlerts, &alertpb.UserHiddenAlert{
			Id:          hiddenAlert.ID,
			UserId:      hiddenAlert.UserID,
			Fingerprint: hiddenAlert.Fingerprint,
			AlertName:   hiddenAlert.AlertName,
			Instance:    hiddenAlert.Instance,
			Reason:      hiddenAlert.Reason,
			CreatedAt:   timestamppb.New(hiddenAlert.CreatedAt),
			UpdatedAt:   timestamppb.New(hiddenAlert.UpdatedAt),
		})
	}

	return &alertpb.GetUserHiddenAlertsResponse{
		HiddenAlerts: pbHiddenAlerts,
		Success:      true,
		Message:      "Hidden alerts retrieved successfully",
	}, nil
}

// HideAlert implements the HideAlert RPC method
func (s *AlertServiceGorm) HideAlert(ctx context.Context, req *alertpb.HideAlertRequest) (*alertpb.HideAlertResponse, error) {
	if req.SessionId == "" {
		return &alertpb.HideAlertResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.Fingerprint == "" {
		return &alertpb.HideAlertResponse{
			Success: false,
			Message: "Fingerprint is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.HideAlertResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Create hidden alert in database
	hiddenAlert, err := s.db.CreateUserHiddenAlert(user.ID, req.Fingerprint, req.AlertName, req.Instance, req.Reason)
	if err != nil {
		log.Printf("Error creating hidden alert for user %s: %v", user.ID, err)
		return &alertpb.HideAlertResponse{
			Success: false,
			Message: "Failed to hide alert",
		}, nil
	}

	pbHiddenAlert := &alertpb.UserHiddenAlert{
		Id:          hiddenAlert.ID,
		UserId:      hiddenAlert.UserID,
		Fingerprint: hiddenAlert.Fingerprint,
		AlertName:   hiddenAlert.AlertName,
		Instance:    hiddenAlert.Instance,
		Reason:      hiddenAlert.Reason,
		CreatedAt:   timestamppb.New(hiddenAlert.CreatedAt),
		UpdatedAt:   timestamppb.New(hiddenAlert.UpdatedAt),
	}

	return &alertpb.HideAlertResponse{
		Success:     true,
		HiddenAlert: pbHiddenAlert,
		Message:     "Alert hidden successfully",
	}, nil
}

// UnhideAlert implements the UnhideAlert RPC method
func (s *AlertServiceGorm) UnhideAlert(ctx context.Context, req *alertpb.UnhideAlertRequest) (*alertpb.UnhideAlertResponse, error) {
	if req.SessionId == "" {
		return &alertpb.UnhideAlertResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.Fingerprint == "" {
		return &alertpb.UnhideAlertResponse{
			Success: false,
			Message: "Fingerprint is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.UnhideAlertResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Delete hidden alert from database
	err = s.db.RemoveUserHiddenAlert(user.ID, req.Fingerprint)
	if err != nil {
		log.Printf("Error removing hidden alert for user %s: %v", user.ID, err)
		return &alertpb.UnhideAlertResponse{
			Success: false,
			Message: "Failed to unhide alert",
		}, nil
	}

	return &alertpb.UnhideAlertResponse{
		Success: true,
		Message: "Alert unhidden successfully",
	}, nil
}

// ClearAllHiddenAlerts implements the ClearAllHiddenAlerts RPC method
func (s *AlertServiceGorm) ClearAllHiddenAlerts(ctx context.Context, req *alertpb.ClearAllHiddenAlertsRequest) (*alertpb.ClearAllHiddenAlertsResponse, error) {
	if req.SessionId == "" {
		return &alertpb.ClearAllHiddenAlertsResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.ClearAllHiddenAlertsResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Clear all hidden alerts for user
	clearedCount, err := s.db.ClearUserHiddenAlerts(user.ID)
	if err != nil {
		log.Printf("Error clearing hidden alerts for user %s: %v", user.ID, err)
		return &alertpb.ClearAllHiddenAlertsResponse{
			Success: false,
			Message: "Failed to clear hidden alerts",
		}, nil
	}

	return &alertpb.ClearAllHiddenAlertsResponse{
		Success:      true,
		ClearedCount: int32(clearedCount),
		Message:      fmt.Sprintf("Cleared %d hidden alerts", clearedCount),
	}, nil
}

// GetUserHiddenRules implements the GetUserHiddenRules RPC method
func (s *AlertServiceGorm) GetUserHiddenRules(ctx context.Context, req *alertpb.GetUserHiddenRulesRequest) (*alertpb.GetUserHiddenRulesResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetUserHiddenRulesResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetUserHiddenRulesResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get hidden rules from database
	hiddenRules, err := s.db.GetUserHiddenRules(user.ID)
	if err != nil {
		log.Printf("Error getting hidden rules for user %s: %v", user.ID, err)
		return &alertpb.GetUserHiddenRulesResponse{
			Success: false,
			Message: "Failed to get hidden rules",
		}, nil
	}

	// Convert to protobuf format
	var pbHiddenRules []*alertpb.UserHiddenRule
	for _, rule := range hiddenRules {
		pbHiddenRules = append(pbHiddenRules, &alertpb.UserHiddenRule{
			Id:          rule.ID,
			UserId:      rule.UserID,
			Name:        rule.Name,
			Description: rule.Description,
			LabelKey:    rule.LabelKey,
			LabelValue:  rule.LabelValue,
			IsRegex:     rule.IsRegex,
			IsEnabled:   rule.IsEnabled,
			Priority:    int32(rule.Priority),
			CreatedAt:   timestamppb.New(rule.CreatedAt),
			UpdatedAt:   timestamppb.New(rule.UpdatedAt),
		})
	}

	return &alertpb.GetUserHiddenRulesResponse{
		HiddenRules: pbHiddenRules,
		Success:     true,
		Message:     "Hidden rules retrieved successfully",
	}, nil
}

// SaveHiddenRule implements the SaveHiddenRule RPC method
func (s *AlertServiceGorm) SaveHiddenRule(ctx context.Context, req *alertpb.SaveHiddenRuleRequest) (*alertpb.SaveHiddenRuleResponse, error) {
	if req.SessionId == "" {
		return &alertpb.SaveHiddenRuleResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.Rule == nil {
		return &alertpb.SaveHiddenRuleResponse{
			Success: false,
			Message: "Rule data is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.SaveHiddenRuleResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Convert protobuf to database model
	rule := &models.UserHiddenRule{
		ID:          req.Rule.Id,
		UserID:      user.ID,
		Name:        req.Rule.Name,
		Description: req.Rule.Description,
		LabelKey:    req.Rule.LabelKey,
		LabelValue:  req.Rule.LabelValue,
		IsRegex:     req.Rule.IsRegex,
		IsEnabled:   req.Rule.IsEnabled,
		Priority:    int(req.Rule.Priority),
	}

	// Save to database
	savedRule, err := s.db.SaveUserHiddenRule(user.ID, rule)
	if err != nil {
		log.Printf("Error saving hidden rule for user %s: %v", user.ID, err)
		return &alertpb.SaveHiddenRuleResponse{
			Success: false,
			Message: "Failed to save hidden rule",
		}, nil
	}

	pbRule := &alertpb.UserHiddenRule{
		Id:          savedRule.ID,
		UserId:      savedRule.UserID,
		Name:        savedRule.Name,
		Description: savedRule.Description,
		LabelKey:    savedRule.LabelKey,
		LabelValue:  savedRule.LabelValue,
		IsRegex:     savedRule.IsRegex,
		IsEnabled:   savedRule.IsEnabled,
		Priority:    int32(savedRule.Priority),
		CreatedAt:   timestamppb.New(savedRule.CreatedAt),
		UpdatedAt:   timestamppb.New(savedRule.UpdatedAt),
	}

	return &alertpb.SaveHiddenRuleResponse{
		Success: true,
		Rule:    pbRule,
		Message: "Hidden rule saved successfully",
	}, nil
}

// RemoveHiddenRule implements the RemoveHiddenRule RPC method
func (s *AlertServiceGorm) RemoveHiddenRule(ctx context.Context, req *alertpb.RemoveHiddenRuleRequest) (*alertpb.RemoveHiddenRuleResponse, error) {
	if req.SessionId == "" {
		return &alertpb.RemoveHiddenRuleResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.RuleId == "" {
		return &alertpb.RemoveHiddenRuleResponse{
			Success: false,
			Message: "Rule ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.RemoveHiddenRuleResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Remove from database
	err = s.db.RemoveUserHiddenRule(user.ID, req.RuleId)
	if err != nil {
		log.Printf("Error removing hidden rule for user %s: %v", user.ID, err)
		return &alertpb.RemoveHiddenRuleResponse{
			Success: false,
			Message: "Failed to remove hidden rule",
		}, nil
	}

	return &alertpb.RemoveHiddenRuleResponse{
		Success: true,
		Message: "Hidden rule removed successfully",
	}, nil
}

// Sentry Integration Methods

// GetUserSentryConfig implements the GetUserSentryConfig RPC method
func (s *AuthServiceGorm) GetUserSentryConfig(ctx context.Context, req *authpb.GetUserSentryConfigRequest) (*authpb.GetUserSentryConfigResponse, error) {
	if req.UserId == "" {
		return &authpb.GetUserSentryConfigResponse{
			Success: false,
			Error:   "User ID is required",
		}, nil
	}

	// Get user Sentry config from database using string user ID
	config, err := s.db.GetUserSentryConfig(req.UserId)
	if err != nil {
		// Config not found is not an error, just return empty config
		return &authpb.GetUserSentryConfigResponse{
			Success: true,
			Config:  nil, // No config found
		}, nil
	}

	// Convert to protobuf format (excluding sensitive token)
	pbConfig := &authpb.UserSentryConfig{
		UserId:    req.UserId,
		BaseUrl:   config.SentryBaseURL,
		CreatedAt: timestamppb.New(config.CreatedAt),
		UpdatedAt: timestamppb.New(config.UpdatedAt),
	}

	return &authpb.GetUserSentryConfigResponse{
		Success: true,
		Config:  pbConfig,
	}, nil
}

// GetUserSentryToken implements the GetUserSentryToken RPC method
// This method returns the user's decrypted Sentry token for API calls
func (s *AuthServiceGorm) GetUserSentryToken(ctx context.Context, req *authpb.GetUserSentryTokenRequest) (*authpb.GetUserSentryTokenResponse, error) {
	if req.UserId == "" {
		return &authpb.GetUserSentryTokenResponse{
			Success: false,
			Error:   "User ID is required",
		}, nil
	}

	if req.SessionId == "" {
		return &authpb.GetUserSentryTokenResponse{
			Success: false,
			Error:   "Session ID is required for authorization",
		}, nil
	}

	// Validate session and get authenticated user
	authenticatedUser, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &authpb.GetUserSentryTokenResponse{
			Success: false,
			Error:   "Invalid session",
		}, nil
	}

	// CRITICAL SECURITY CHECK: Ensure user can only access their own token
	if authenticatedUser.ID != req.UserId {
		return &authpb.GetUserSentryTokenResponse{
			Success: false,
			Error:   "Unauthorized: Cannot access another user's token",
		}, nil
	}

	// Get user Sentry config from database with decrypted token
	config, err := s.db.GetUserSentryConfig(req.UserId)
	if err != nil {
		// Config not found is not an error, just return no token
		return &authpb.GetUserSentryTokenResponse{
			Success:  true,
			HasToken: false,
		}, nil
	}

	// Return the decrypted token
	return &authpb.GetUserSentryTokenResponse{
		Success:       true,
		PersonalToken: config.PersonalToken, // This is already decrypted by the database layer
		HasToken:      config.PersonalToken != "",
	}, nil
}

// SaveUserSentryConfig implements the SaveUserSentryConfig RPC method
func (s *AuthServiceGorm) SaveUserSentryConfig(ctx context.Context, req *authpb.SaveUserSentryConfigRequest) (*authpb.SaveUserSentryConfigResponse, error) {
	if req.UserId == "" {
		return &authpb.SaveUserSentryConfigResponse{
			Success: false,
			Error:   "User ID is required",
		}, nil
	}

	if req.SessionId == "" {
		return &authpb.SaveUserSentryConfigResponse{
			Success: false,
			Error:   "Session ID is required",
		}, nil
	}

	if req.PersonalToken == "" {
		return &authpb.SaveUserSentryConfigResponse{
			Success: false,
			Error:   "Personal token is required",
		}, nil
	}

	// Validate session and get authenticated user
	authenticatedUser, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &authpb.SaveUserSentryConfigResponse{
			Success: false,
			Error:   "Invalid session",
		}, nil
	}

	// CRITICAL SECURITY CHECK: Ensure user can only save their own config
	if authenticatedUser.ID != req.UserId {
		return &authpb.SaveUserSentryConfigResponse{
			Success: false,
			Error:   "Unauthorized: Cannot save another user's Sentry configuration",
		}, nil
	}

	// Use default base URL if not provided
	baseURL := req.BaseUrl
	if baseURL == "" {
		baseURL = "https://sentry.io"
	}

	// Save to database using string user ID
	err = s.db.SaveUserSentryConfig(req.UserId, req.PersonalToken, baseURL)
	if err != nil {
		log.Printf("Error saving user Sentry config for user %s: %v", req.UserId, err)
		return &authpb.SaveUserSentryConfigResponse{
			Success: false,
			Error:   "Failed to save Sentry configuration",
		}, nil
	}

	return &authpb.SaveUserSentryConfigResponse{
		Success: true,
	}, nil
}

// DeleteUserSentryConfig implements the DeleteUserSentryConfig RPC method
func (s *AuthServiceGorm) DeleteUserSentryConfig(ctx context.Context, req *authpb.DeleteUserSentryConfigRequest) (*authpb.DeleteUserSentryConfigResponse, error) {
	if req.UserId == "" {
		return &authpb.DeleteUserSentryConfigResponse{
			Success: false,
			Error:   "User ID is required",
		}, nil
	}

	if req.SessionId == "" {
		return &authpb.DeleteUserSentryConfigResponse{
			Success: false,
			Error:   "Session ID is required",
		}, nil
	}

	// Validate session and get authenticated user
	authenticatedUser, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &authpb.DeleteUserSentryConfigResponse{
			Success: false,
			Error:   "Invalid session",
		}, nil
	}

	// CRITICAL SECURITY CHECK: Ensure user can only delete their own config
	if authenticatedUser.ID != req.UserId {
		return &authpb.DeleteUserSentryConfigResponse{
			Success: false,
			Error:   "Unauthorized: Cannot delete another user's Sentry configuration",
		}, nil
	}

	// Delete from database using string user ID
	err = s.db.DeleteUserSentryConfig(req.UserId)
	if err != nil {
		log.Printf("Error deleting user Sentry config for user %s: %v", req.UserId, err)
		return &authpb.DeleteUserSentryConfigResponse{
			Success: false,
			Error:   "Failed to delete Sentry configuration",
		}, nil
	}

	return &authpb.DeleteUserSentryConfigResponse{
		Success: true,
	}, nil
}

// GetNotificationPreferences implements the GetNotificationPreferences RPC method
func (s *AlertServiceGorm) GetNotificationPreferences(ctx context.Context, req *alertpb.GetNotificationPreferencesRequest) (*alertpb.GetNotificationPreferencesResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetNotificationPreferencesResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetNotificationPreferencesResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get notification preferences from database
	prefs, err := s.db.GetUserNotificationPreference(user.ID)
	if err != nil {
		log.Printf("Failed to get notification preferences for user %s: %v", user.ID, err)
		// Return default preferences if not found
		defaultPrefs := models.DefaultNotificationPreference(user.ID)
		return &alertpb.GetNotificationPreferencesResponse{
			Success: true,
			Preferences: &alertpb.NotificationPreference{
				Id:                          defaultPrefs.ID,
				UserId:                      defaultPrefs.UserID,
				BrowserNotificationsEnabled: defaultPrefs.BrowserNotificationsEnabled,
				EnabledSeverities:           defaultPrefs.EnabledSeverities,
				SoundNotificationsEnabled:   defaultPrefs.SoundNotificationsEnabled,
			},
			Message: "Using default notification preferences",
		}, nil
	}

	// Convert to protobuf format
	pbPrefs := &alertpb.NotificationPreference{
		Id:                          prefs.ID,
		UserId:                      prefs.UserID,
		BrowserNotificationsEnabled: prefs.BrowserNotificationsEnabled,
		EnabledSeverities:           prefs.EnabledSeverities,
		SoundNotificationsEnabled:   prefs.SoundNotificationsEnabled,
		CreatedAt:                   timestamppb.New(prefs.CreatedAt),
		UpdatedAt:                   timestamppb.New(prefs.UpdatedAt),
	}

	return &alertpb.GetNotificationPreferencesResponse{
		Success:     true,
		Preferences: pbPrefs,
		Message:     "Notification preferences retrieved successfully",
	}, nil
}

// SaveNotificationPreferences implements the SaveNotificationPreferences RPC method
func (s *AlertServiceGorm) SaveNotificationPreferences(ctx context.Context, req *alertpb.SaveNotificationPreferencesRequest) (*alertpb.SaveNotificationPreferencesResponse, error) {
	if req.SessionId == "" {
		return &alertpb.SaveNotificationPreferencesResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.SaveNotificationPreferencesResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Validate severities
	validSeverities := []string{}
	validValues := map[string]bool{"critical": true, "warning": true, "info": true, "information": true}
	for _, s := range req.EnabledSeverities {
		if validValues[s] {
			validSeverities = append(validSeverities, s)
		}
	}

	// Create preference object
	pref := &models.NotificationPreference{
		UserID:                      user.ID,
		BrowserNotificationsEnabled: req.BrowserNotificationsEnabled,
		EnabledSeverities:           models.SeverityList(validSeverities),
		SoundNotificationsEnabled:   req.SoundNotificationsEnabled,
	}

	// Save to database
	err = s.db.SaveUserNotificationPreference(pref)
	if err != nil {
		log.Printf("Failed to save notification preferences for user %s: %v", user.ID, err)
		return &alertpb.SaveNotificationPreferencesResponse{
			Success: false,
			Message: "Failed to save notification preferences",
		}, nil
	}

	log.Printf("Notification preferences saved for user %s", user.ID)

	// Get the updated preference from DB to return complete data
	savedPrefs, err := s.db.GetUserNotificationPreference(user.ID)
	if err != nil {
		// Return success but with the data we tried to save
		return &alertpb.SaveNotificationPreferencesResponse{
			Success: true,
			Preferences: &alertpb.NotificationPreference{
				UserId:                      user.ID,
				BrowserNotificationsEnabled: req.BrowserNotificationsEnabled,
				EnabledSeverities:           validSeverities,
				SoundNotificationsEnabled:   req.SoundNotificationsEnabled,
			},
			Message: "Notification preferences saved successfully",
		}, nil
	}

	// Return the complete saved data
	pbPrefs := &alertpb.NotificationPreference{
		Id:                          savedPrefs.ID,
		UserId:                      savedPrefs.UserID,
		BrowserNotificationsEnabled: savedPrefs.BrowserNotificationsEnabled,
		EnabledSeverities:           savedPrefs.EnabledSeverities,
		SoundNotificationsEnabled:   savedPrefs.SoundNotificationsEnabled,
		CreatedAt:                   timestamppb.New(savedPrefs.CreatedAt),
		UpdatedAt:                   timestamppb.New(savedPrefs.UpdatedAt),
	}

	return &alertpb.SaveNotificationPreferencesResponse{
		Success:     true,
		Preferences: pbPrefs,
		Message:     "Notification preferences saved successfully",
	}, nil
}

// GetFilterPresets implements the GetFilterPresets RPC method
func (s *AlertServiceGorm) GetFilterPresets(ctx context.Context, req *alertpb.GetFilterPresetsRequest) (*alertpb.GetFilterPresetsResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetFilterPresetsResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetFilterPresetsResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get filter presets from database
	presets, err := s.db.GetFilterPresets(user.ID, req.IncludeShared)
	if err != nil {
		log.Printf("Failed to get filter presets for user %s: %v", user.ID, err)
		return &alertpb.GetFilterPresetsResponse{
			Success: false,
			Message: "Failed to retrieve filter presets",
		}, nil
	}

	// Convert to protobuf format
	pbPresets := make([]*alertpb.FilterPreset, len(presets))
	for i, preset := range presets {
		pbPresets[i] = &alertpb.FilterPreset{
			Id:          preset.ID,
			UserId:      preset.UserID,
			Name:        preset.Name,
			Description: preset.Description,
			IsShared:    preset.IsShared,
			IsDefault:   preset.IsDefault,
			FilterData:  []byte(preset.FilterData),
			CreatedAt:   timestamppb.New(preset.CreatedAt),
			UpdatedAt:   timestamppb.New(preset.UpdatedAt),
		}
	}

	return &alertpb.GetFilterPresetsResponse{
		Success: true,
		Presets: pbPresets,
		Message: "Filter presets retrieved successfully",
	}, nil
}

// SaveFilterPreset implements the SaveFilterPreset RPC method
func (s *AlertServiceGorm) SaveFilterPreset(ctx context.Context, req *alertpb.SaveFilterPresetRequest) (*alertpb.SaveFilterPresetResponse, error) {
	if req.SessionId == "" {
		return &alertpb.SaveFilterPresetResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.Name == "" {
		return &alertpb.SaveFilterPresetResponse{
			Success: false,
			Message: "Preset name is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.SaveFilterPresetResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Create filter preset
	preset := &models.FilterPreset{
		UserID:      user.ID,
		Name:        req.Name,
		Description: req.Description,
		IsShared:    req.IsShared,
		FilterData:  models.JSONB(req.FilterData),
	}

	// Save to database
	savedPreset, err := s.db.CreateFilterPreset(preset)
	if err != nil {
		log.Printf("Failed to save filter preset for user %s: %v", user.ID, err)
		return &alertpb.SaveFilterPresetResponse{
			Success: false,
			Message: "Failed to save filter preset",
		}, nil
	}

	log.Printf("Filter preset '%s' saved for user %s", preset.Name, user.ID)

	// Convert to protobuf format
	pbPreset := &alertpb.FilterPreset{
		Id:          savedPreset.ID,
		UserId:      savedPreset.UserID,
		Name:        savedPreset.Name,
		Description: savedPreset.Description,
		IsShared:    savedPreset.IsShared,
		IsDefault:   savedPreset.IsDefault,
		FilterData:  []byte(savedPreset.FilterData),
		CreatedAt:   timestamppb.New(savedPreset.CreatedAt),
		UpdatedAt:   timestamppb.New(savedPreset.UpdatedAt),
	}

	return &alertpb.SaveFilterPresetResponse{
		Success: true,
		Preset:  pbPreset,
		Message: "Filter preset saved successfully",
	}, nil
}

// UpdateFilterPreset implements the UpdateFilterPreset RPC method
func (s *AlertServiceGorm) UpdateFilterPreset(ctx context.Context, req *alertpb.UpdateFilterPresetRequest) (*alertpb.UpdateFilterPresetResponse, error) {
	if req.SessionId == "" {
		return &alertpb.UpdateFilterPresetResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.PresetId == "" {
		return &alertpb.UpdateFilterPresetResponse{
			Success: false,
			Message: "Preset ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.UpdateFilterPresetResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get existing preset
	preset, err := s.db.GetFilterPresetByID(req.PresetId)
	if err != nil {
		return &alertpb.UpdateFilterPresetResponse{
			Success: false,
			Message: "Filter preset not found",
		}, nil
	}

	// Check ownership
	if preset.UserID != user.ID {
		return &alertpb.UpdateFilterPresetResponse{
			Success: false,
			Message: "Not authorized to update this filter preset",
		}, nil
	}

	// Update fields
	preset.Name = req.Name
	preset.Description = req.Description
	preset.IsShared = req.IsShared
	preset.FilterData = models.JSONB(req.FilterData)

	// Save to database
	err = s.db.UpdateFilterPreset(preset)
	if err != nil {
		log.Printf("Failed to update filter preset %s for user %s: %v", req.PresetId, user.ID, err)
		return &alertpb.UpdateFilterPresetResponse{
			Success: false,
			Message: "Failed to update filter preset",
		}, nil
	}

	log.Printf("Filter preset '%s' updated for user %s", preset.Name, user.ID)

	// Convert to protobuf format
	pbPreset := &alertpb.FilterPreset{
		Id:          preset.ID,
		UserId:      preset.UserID,
		Name:        preset.Name,
		Description: preset.Description,
		IsShared:    preset.IsShared,
		IsDefault:   preset.IsDefault,
		FilterData:  []byte(preset.FilterData),
		CreatedAt:   timestamppb.New(preset.CreatedAt),
		UpdatedAt:   timestamppb.New(preset.UpdatedAt),
	}

	return &alertpb.UpdateFilterPresetResponse{
		Success: true,
		Preset:  pbPreset,
		Message: "Filter preset updated successfully",
	}, nil
}

// DeleteFilterPreset implements the DeleteFilterPreset RPC method
func (s *AlertServiceGorm) DeleteFilterPreset(ctx context.Context, req *alertpb.DeleteFilterPresetRequest) (*alertpb.DeleteFilterPresetResponse, error) {
	if req.SessionId == "" {
		return &alertpb.DeleteFilterPresetResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.PresetId == "" {
		return &alertpb.DeleteFilterPresetResponse{
			Success: false,
			Message: "Preset ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.DeleteFilterPresetResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Delete with ownership check
	err = s.db.DeleteFilterPreset(req.PresetId, user.ID)
	if err != nil {
		log.Printf("Failed to delete filter preset %s for user %s: %v", req.PresetId, user.ID, err)
		return &alertpb.DeleteFilterPresetResponse{
			Success: false,
			Message: "Failed to delete filter preset or not authorized",
		}, nil
	}

	log.Printf("Filter preset %s deleted for user %s", req.PresetId, user.ID)

	return &alertpb.DeleteFilterPresetResponse{
		Success: true,
		Message: "Filter preset deleted successfully",
	}, nil
}

// SetDefaultFilterPreset implements the SetDefaultFilterPreset RPC method
func (s *AlertServiceGorm) SetDefaultFilterPreset(ctx context.Context, req *alertpb.SetDefaultFilterPresetRequest) (*alertpb.SetDefaultFilterPresetResponse, error) {
	if req.SessionId == "" {
		return &alertpb.SetDefaultFilterPresetResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.PresetId == "" {
		return &alertpb.SetDefaultFilterPresetResponse{
			Success: false,
			Message: "Preset ID is required",
		}, nil
	}

	// Validate session and get user
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.SetDefaultFilterPresetResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Set default (user can set any accessible preset, including shared ones)
	err = s.db.SetDefaultFilterPreset(req.PresetId, user.ID)
	if err != nil {
		log.Printf("Failed to set default filter preset %s for user %s: %v", req.PresetId, user.ID, err)
		return &alertpb.SetDefaultFilterPresetResponse{
			Success: false,
			Message: "Failed to set default filter preset - preset not found or not accessible",
		}, nil
	}

	log.Printf("Filter preset %s set as default for user %s", req.PresetId, user.ID)

	return &alertpb.SetDefaultFilterPresetResponse{
		Success: true,
		Message: "Default filter preset set successfully",
	}, nil
}

// GetAnnotationButtonConfigs implements the GetAnnotationButtonConfigs RPC method
func (s *AlertServiceGorm) GetAnnotationButtonConfigs(ctx context.Context, req *alertpb.GetAnnotationButtonConfigsRequest) (*alertpb.GetAnnotationButtonConfigsResponse, error) {
	if req.SessionId == "" {
		return &alertpb.GetAnnotationButtonConfigsResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.GetAnnotationButtonConfigsResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Get configs from database (creates defaults if none exist)
	configs, err := s.db.GetAnnotationButtonConfigs(user.ID)
	if err != nil {
		log.Printf("Error getting annotation button configs: %v", err)
		return &alertpb.GetAnnotationButtonConfigsResponse{
			Success: false,
			Message: "Failed to get annotation button configs",
		}, nil
	}

	// Convert to protobuf format
	var pbConfigs []*alertpb.AnnotationButtonConfig
	for _, config := range configs {
		pbConfigs = append(pbConfigs, &alertpb.AnnotationButtonConfig{
			Id:             config.ID,
			UserId:         config.UserID,
			Label:          config.Label,
			AnnotationKeys: config.AnnotationKeys,
			Color:          config.Color,
			Icon:           config.Icon,
			DisplayOrder:   int32(config.DisplayOrder),
			Enabled:        config.Enabled,
			ButtonType:     config.ButtonType,
			CreatedAt:      timestamppb.New(config.CreatedAt),
			UpdatedAt:      timestamppb.New(config.UpdatedAt),
		})
	}

	return &alertpb.GetAnnotationButtonConfigsResponse{
		Success: true,
		Configs: pbConfigs,
		Message: "Annotation button configs retrieved successfully",
	}, nil
}

// SaveAnnotationButtonConfigs implements the SaveAnnotationButtonConfigs RPC method
func (s *AlertServiceGorm) SaveAnnotationButtonConfigs(ctx context.Context, req *alertpb.SaveAnnotationButtonConfigsRequest) (*alertpb.SaveAnnotationButtonConfigsResponse, error) {
	if req.SessionId == "" {
		return &alertpb.SaveAnnotationButtonConfigsResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.SaveAnnotationButtonConfigsResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Convert protobuf configs to database models and validate
	var configs []models.AnnotationButtonConfig
	for _, pbConfig := range req.Configs {
		config := models.AnnotationButtonConfig{
			ID:             pbConfig.Id,
			UserID:         user.ID,
			Label:          pbConfig.Label,
			AnnotationKeys: pbConfig.AnnotationKeys,
			Color:          models.SanitizeColor(pbConfig.Color), // Sanitize color to prevent CSS injection
			Icon:           pbConfig.Icon,
			DisplayOrder:   int(pbConfig.DisplayOrder),
			Enabled:        pbConfig.Enabled,
			ButtonType:     pbConfig.ButtonType,
		}

		// Validate configuration
		if err := config.Validate(); err != nil {
			log.Printf("Invalid annotation button config: %v", err)
			return &alertpb.SaveAnnotationButtonConfigsResponse{
				Success: false,
				Message: "Invalid configuration: " + err.Error(),
			}, nil
		}

		configs = append(configs, config)
	}

	// Save to database
	err = s.db.SaveAnnotationButtonConfigs(user.ID, configs)
	if err != nil {
		log.Printf("Error saving annotation button configs: %v", err)
		return &alertpb.SaveAnnotationButtonConfigsResponse{
			Success: false,
			Message: "Failed to save annotation button configs",
		}, nil
	}

	log.Printf("Annotation button configs saved for user %s", user.ID)

	return &alertpb.SaveAnnotationButtonConfigsResponse{
		Success: true,
		Message: "Annotation button configs saved successfully",
	}, nil
}

// DeleteAnnotationButtonConfig implements the DeleteAnnotationButtonConfig RPC method
func (s *AlertServiceGorm) DeleteAnnotationButtonConfig(ctx context.Context, req *alertpb.DeleteAnnotationButtonConfigRequest) (*alertpb.DeleteAnnotationButtonConfigResponse, error) {
	if req.SessionId == "" {
		return &alertpb.DeleteAnnotationButtonConfigResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.ConfigId == "" {
		return &alertpb.DeleteAnnotationButtonConfigResponse{
			Success: false,
			Message: "Config ID is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.DeleteAnnotationButtonConfigResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Delete config
	err = s.db.DeleteAnnotationButtonConfig(user.ID, req.ConfigId)
	if err != nil {
		log.Printf("Failed to delete annotation button config %s for user %s: %v", req.ConfigId, user.ID, err)
		return &alertpb.DeleteAnnotationButtonConfigResponse{
			Success: false,
			Message: "Failed to delete annotation button config or not authorized",
		}, nil
	}

	log.Printf("Annotation button config %s deleted for user %s", req.ConfigId, user.ID)

	return &alertpb.DeleteAnnotationButtonConfigResponse{
		Success: true,
		Message: "Annotation button config deleted successfully",
	}, nil
}

// CreateAnnotationButtonConfig implements the CreateAnnotationButtonConfig RPC method
func (s *AlertServiceGorm) CreateAnnotationButtonConfig(ctx context.Context, req *alertpb.CreateAnnotationButtonConfigRequest) (*alertpb.CreateAnnotationButtonConfigResponse, error) {
	if req.SessionId == "" {
		return &alertpb.CreateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.Config == nil {
		return &alertpb.CreateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Config is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.CreateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Convert protobuf to database model and validate
	config := models.AnnotationButtonConfig{
		UserID:         user.ID,
		Label:          req.Config.Label,
		AnnotationKeys: req.Config.AnnotationKeys,
		Color:          models.SanitizeColor(req.Config.Color),
		Icon:           req.Config.Icon,
		DisplayOrder:   int(req.Config.DisplayOrder),
		Enabled:        req.Config.Enabled,
		ButtonType:     req.Config.ButtonType,
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		log.Printf("Invalid annotation button config: %v", err)
		return &alertpb.CreateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Invalid configuration: " + err.Error(),
		}, nil
	}

	// Create in database
	err = s.db.CreateAnnotationButtonConfig(&config)
	if err != nil {
		log.Printf("Error creating annotation button config: %v", err)
		return &alertpb.CreateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Failed to create annotation button config",
		}, nil
	}

	log.Printf("Annotation button config created for user %s", user.ID)

	// Convert back to protobuf
	pbConfig := &alertpb.AnnotationButtonConfig{
		Id:             config.ID,
		UserId:         config.UserID,
		Label:          config.Label,
		AnnotationKeys: config.AnnotationKeys,
		Color:          config.Color,
		Icon:           config.Icon,
		DisplayOrder:   int32(config.DisplayOrder),
		Enabled:        config.Enabled,
		ButtonType:     config.ButtonType,
		CreatedAt:      timestamppb.New(config.CreatedAt),
		UpdatedAt:      timestamppb.New(config.UpdatedAt),
	}

	return &alertpb.CreateAnnotationButtonConfigResponse{
		Success: true,
		Config:  pbConfig,
		Message: "Annotation button config created successfully",
	}, nil
}

// UpdateAnnotationButtonConfig implements the UpdateAnnotationButtonConfig RPC method
func (s *AlertServiceGorm) UpdateAnnotationButtonConfig(ctx context.Context, req *alertpb.UpdateAnnotationButtonConfigRequest) (*alertpb.UpdateAnnotationButtonConfigResponse, error) {
	if req.SessionId == "" {
		return &alertpb.UpdateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Session ID is required",
		}, nil
	}

	if req.Config == nil {
		return &alertpb.UpdateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Config is required",
		}, nil
	}

	if req.Config.Id == "" {
		return &alertpb.UpdateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Config ID is required",
		}, nil
	}

	// Validate session
	user, err := s.db.GetUserBySession(req.SessionId)
	if err != nil {
		return &alertpb.UpdateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Invalid session",
		}, nil
	}

	// Convert protobuf to database model and validate
	config := models.AnnotationButtonConfig{
		ID:             req.Config.Id,
		UserID:         user.ID,
		Label:          req.Config.Label,
		AnnotationKeys: req.Config.AnnotationKeys,
		Color:          models.SanitizeColor(req.Config.Color),
		Icon:           req.Config.Icon,
		DisplayOrder:   int(req.Config.DisplayOrder),
		Enabled:        req.Config.Enabled,
		ButtonType:     req.Config.ButtonType,
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		log.Printf("Invalid annotation button config: %v", err)
		return &alertpb.UpdateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Invalid configuration: " + err.Error(),
		}, nil
	}

	// Update in database
	err = s.db.UpdateAnnotationButtonConfig(&config)
	if err != nil {
		log.Printf("Error updating annotation button config: %v", err)
		return &alertpb.UpdateAnnotationButtonConfigResponse{
			Success: false,
			Message: "Failed to update annotation button config",
		}, nil
	}

	log.Printf("Annotation button config %s updated for user %s", config.ID, user.ID)

	// Convert back to protobuf
	pbConfig := &alertpb.AnnotationButtonConfig{
		Id:             config.ID,
		UserId:         config.UserID,
		Label:          config.Label,
		AnnotationKeys: config.AnnotationKeys,
		Color:          config.Color,
		Icon:           config.Icon,
		DisplayOrder:   int32(config.DisplayOrder),
		Enabled:        config.Enabled,
		ButtonType:     config.ButtonType,
		CreatedAt:      timestamppb.New(config.CreatedAt),
		UpdatedAt:      timestamppb.New(config.UpdatedAt),
	}

	return &alertpb.UpdateAnnotationButtonConfigResponse{
		Success: true,
		Config:  pbConfig,
		Message: "Annotation button config updated successfully",
	}, nil
}

func generateUUID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
