package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notificator/internal/backend/database"
	alertpb "notificator/internal/backend/proto/alert"
	authpb "notificator/internal/backend/proto/auth"
	mainmodels "notificator/internal/models"
)

// AuthServiceGorm implements the AuthService gRPC service
type AuthServiceGorm struct {
	authpb.UnimplementedAuthServiceServer
	db *database.GormDB
}

func NewAuthServiceGorm(db *database.GormDB) *AuthServiceGorm {
	return &AuthServiceGorm{db: db}
}

// Register implements the Register RPC method
func (s *AuthServiceGorm) Register(ctx context.Context, req *authpb.RegisterRequest) (*authpb.RegisterResponse, error) {
	// Basic validation
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

	// Verify password
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

	// Create session (expires in 24 hours)
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
			Id:               pref.ID,
			UserId:           pref.UserID,
			LabelConditions:  conditions,
			Color:            pref.Color,
			ColorType:        pref.ColorType,
			Priority:         int32(pref.Priority),
			CreatedAt:        timestamppb.New(pref.CreatedAt),
			UpdatedAt:        timestamppb.New(pref.UpdatedAt),
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
			ID:        pbPref.Id,
			UserID:    user.ID,
			Color:     pbPref.Color,
			ColorType: pbPref.ColorType,
			Priority:  int(pbPref.Priority),
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

// DeleteUserColorPreference implements the DeleteUserColorPreference RPC method
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
		Fingerprint:  req.Fingerprint,
		UpdateType:   alertpb.ResolvedAlertUpdateType_RESOLVED_ALERT_CREATED,
		ResolvedAlert: pbResolvedAlert,
		Timestamp:    timestamppb.Now(),
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

// Helper function to generate UUID
func generateUUID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
