package client

import (
	"context"
	"testing"

	alertpb "notificator/internal/backend/proto/alert"

	"google.golang.org/grpc"
)

// fakeAlertClient embeds the real interface (nil) so it satisfies
// alertpb.AlertServiceClient while only overriding the methods under test.
type fakeAlertClient struct {
	alertpb.AlertServiceClient
	success bool
	message string
}

func (f *fakeAlertClient) AddAcknowledgment(ctx context.Context, in *alertpb.AddAcknowledgmentRequest, opts ...grpc.CallOption) (*alertpb.AddAcknowledgmentResponse, error) {
	return &alertpb.AddAcknowledgmentResponse{Success: f.success, Message: f.message}, nil
}

func (f *fakeAlertClient) DeleteAcknowledgment(ctx context.Context, in *alertpb.DeleteAcknowledgmentRequest, opts ...grpc.CallOption) (*alertpb.DeleteAcknowledgmentResponse, error) {
	return &alertpb.DeleteAcknowledgmentResponse{Success: f.success, Message: f.message}, nil
}

func (f *fakeAlertClient) AddComment(ctx context.Context, in *alertpb.AddCommentRequest, opts ...grpc.CallOption) (*alertpb.AddCommentResponse, error) {
	return &alertpb.AddCommentResponse{Success: f.success, Message: f.message}, nil
}

func (f *fakeAlertClient) DeleteComment(ctx context.Context, in *alertpb.DeleteCommentRequest, opts ...grpc.CallOption) (*alertpb.DeleteCommentResponse, error) {
	return &alertpb.DeleteCommentResponse{Success: f.success, Message: f.message}, nil
}

func TestAddAcknowledgment_BackendRejection(t *testing.T) {
	c := &BackendClient{alertClient: &fakeAlertClient{success: false, message: "invalid session"}}
	err := c.AddAcknowledgment("session", "alert-key", "reason")
	if err == nil || err.Error() != "invalid session" {
		t.Fatalf("expected error %q, got %v", "invalid session", err)
	}
}

func TestAddAcknowledgment_Success(t *testing.T) {
	c := &BackendClient{alertClient: &fakeAlertClient{success: true}}
	if err := c.AddAcknowledgment("session", "alert-key", "reason"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteAcknowledgment_BackendRejection(t *testing.T) {
	c := &BackendClient{alertClient: &fakeAlertClient{success: false, message: "unauthorized"}}
	err := c.DeleteAcknowledgment("session", "alert-key")
	if err == nil || err.Error() != "unauthorized" {
		t.Fatalf("expected error %q, got %v", "unauthorized", err)
	}
}

func TestDeleteAcknowledgment_Success(t *testing.T) {
	c := &BackendClient{alertClient: &fakeAlertClient{success: true}}
	if err := c.DeleteAcknowledgment("session", "alert-key"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddComment_BackendRejection(t *testing.T) {
	c := &BackendClient{alertClient: &fakeAlertClient{success: false, message: "Failed to create comment"}}
	err := c.AddComment("session", "alert-key", "content")
	if err == nil || err.Error() != "Failed to create comment" {
		t.Fatalf("expected error %q, got %v", "Failed to create comment", err)
	}
}

func TestAddComment_Success(t *testing.T) {
	c := &BackendClient{alertClient: &fakeAlertClient{success: true}}
	if err := c.AddComment("session", "alert-key", "content"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteComment_BackendRejection(t *testing.T) {
	c := &BackendClient{alertClient: &fakeAlertClient{success: false, message: "not found"}}
	err := c.DeleteComment("session", "comment-id")
	if err == nil || err.Error() != "not found" {
		t.Fatalf("expected error %q, got %v", "not found", err)
	}
}

func TestDeleteComment_Success(t *testing.T) {
	c := &BackendClient{alertClient: &fakeAlertClient{success: true}}
	if err := c.DeleteComment("session", "comment-id"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
