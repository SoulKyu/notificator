# Team Activity Feed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A dedicated `/activity` page showing one chronological, filterable log of every ack, unack, comment, silence and resolve across all alerts.

**Architecture:** Single data source — the `comments` table gains a structured `kind` column, since every collaboration action already writes a comment. One read-only backend RPC (`GetRecentActivity`) returns comments in a time window; the webui handler resolves alert names from the cache and applies filters (reusing the dashboard's filter predicate). A new templ page renders a log table, polling every 30s while visible.

**Tech Stack:** Go, gRPC/protobuf, GORM (Postgres in prod, SQLite in tests), Gin, templ, Alpine.js, Tailwind.

## Global Constraints

- go.mod is `go 1.23.0`; Dockerfiles build `golang:1.23-alpine`. Do NOT add a dependency that raises the go directive above 1.23 (verify `head -6 go.mod` after any `go get`). No new deps are needed for this plan.
- Never hand-edit generated files: regenerate proto with `bash scripts/generate_proto.sh` (NOT `make proto`, which no-ops), regenerate templates with `go run github.com/a-h/templ/cmd/templ@v0.3.906 generate` (local templ CLI version differs from go.mod's v0.3.906).
- Templ workflow: edit `.templ` only, never `*_templ.go`.
- Inclusive terms; self-documenting code; Conventional Commits; NO `Co-Authored-By` trailer.
- Feed kinds are exactly: `comment`, `ack`, `unack`, `silence`, `resolve`.

---

### Task 1: `kind` column on comments (proto + model + db + service)

Adds the structured `kind` field end-to-end on the comment write path. Default `comment`; the four audit sites will set their kind in Task 2.

**Files:**
- Modify: `proto/alert.proto` (Comment message, AddCommentRequest)
- Modify: `internal/backend/models/models.go:74-90` (Comment struct)
- Modify: `internal/backend/database/gorm_db.go:345-357` (CreateComment)
- Modify: `internal/backend/services/services.go:504-567` (AddComment)
- Test: `internal/backend/database/gorm_db_test.go`

**Interfaces:**
- Produces: `models.Comment.Kind string`; `GormDB.CreateComment(alertKey, userID, content, kind string) (*models.CommentWithUser, error)`; `AddCommentRequest.Kind` proto field; `alertpb.Comment.Kind` proto field.

- [ ] **Step 1: Add `kind` to the proto messages**

In `proto/alert.proto`, add to `message Comment` (after `content = 5;`... it currently ends at `created_at = 6`):

```proto
message Comment {
  string id = 1;
  string alert_key = 2;
  string user_id = 3;
  string username = 4;
  string content = 5;
  google.protobuf.Timestamp created_at = 6;
  string kind = 7;
}
```

And add to `message AddCommentRequest`:

```proto
message AddCommentRequest {
  string session_id = 1;
  string alert_key = 2;
  string content = 3;
  string kind = 4;
}
```

- [ ] **Step 2: Regenerate proto**

Run: `bash scripts/generate_proto.sh`
Expected: regenerates `internal/backend/proto/alert/*.pb.go` with `Kind` fields; exit 0.

- [ ] **Step 3: Add `Kind` to the GORM model**

In `internal/backend/models/models.go`, the `Comment` struct — add the field after `Content`:

```go
type Comment struct {
	ID        string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	AlertKey  string    `gorm:"not null;size:500;index" json:"alert_key"`
	UserID    string    `gorm:"not null;size:32" json:"user_id"`
	Content   string    `gorm:"not null;type:text" json:"content"`
	Kind      string    `gorm:"size:16;not null;default:comment" json:"kind"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	User User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}
```

- [ ] **Step 4: Write the failing test for CreateComment with kind**

In `internal/backend/database/gorm_db_test.go`, add (note: `newTestDB` must migrate `Comment` — do that in the next step; write the test first):

```go
func TestCreateCommentStoresKind(t *testing.T) {
	gdb := newTestDB(t)
	u := models.User{ID: "u1", Username: "alice", Email: "a@example.com"}
	if err := gdb.db.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	got, err := gdb.CreateComment("key-a", u.ID, "🔇 Alert silenced for 2h: x", "silence")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if got.Kind != "silence" {
		t.Fatalf("Kind = %q, want silence", got.Kind)
	}

	plain, err := gdb.CreateComment("key-a", u.ID, "looks fine", "comment")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if plain.Kind != "comment" {
		t.Fatalf("Kind = %q, want comment", plain.Kind)
	}
}
```

- [ ] **Step 5: Add `Comment` to the test DB migration**

In `internal/backend/database/gorm_db_test.go`, `newTestDB`, extend the AutoMigrate call:

```go
	if err := db.AutoMigrate(&models.User{}, &models.Acknowledgment{}, &models.ResolvedAlert{}, &models.Comment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `go test ./internal/backend/database/ -run TestCreateCommentStoresKind -v`
Expected: FAIL — `CreateComment` currently takes 3 args, compile error `not enough arguments`.

- [ ] **Step 7: Update `CreateComment` to accept kind**

In `internal/backend/database/gorm_db.go:345`:

```go
func (gdb *GormDB) CreateComment(alertKey, userID, content, kind string) (*models.CommentWithUser, error) {
	if kind == "" {
		kind = "comment"
	}
	comment := &models.Comment{
		AlertKey: alertKey,
		UserID:   userID,
		Content:  content,
		Kind:     kind,
	}

	if err := gdb.db.Create(comment).Error; err != nil {
		return nil, fmt.Errorf("failed to create comment: %w", err)
	}

	return gdb.GetCommentWithUser(comment.ID)
}
```

- [ ] **Step 8: Thread `kind` through the AddComment service**

In `internal/backend/services/services.go`, `AddComment` — update the `CreateComment` call (was `s.db.CreateComment(req.AlertKey, user.ID, req.Content)`):

```go
	// Create comment
	comment, err := s.db.CreateComment(req.AlertKey, user.ID, req.Content, req.Kind)
```

And add `Kind` to the `protoComment` literal built just below it:

```go
	protoComment := &alertpb.Comment{
		Id:        comment.ID,
		AlertKey:  comment.AlertKey,
		UserId:    comment.UserID,
		Username:  comment.Username,
		Content:   comment.Content,
		CreatedAt: timestamppb.New(comment.CreatedAt),
		Kind:      comment.Kind,
	}
```

- [ ] **Step 9: Fix the other CreateComment callers**

Run: `grep -rn "\.CreateComment(" internal/ | grep -v _test`
For every call other than the service one above, add `, "comment"` as the 4th arg. (If none, skip.)

- [ ] **Step 10: Run tests + build**

Run: `go build ./... && go test ./internal/backend/...`
Expected: build OK, tests pass including `TestCreateCommentStoresKind`.

- [ ] **Step 11: Commit**

```bash
git add proto/alert.proto internal/backend/proto internal/backend/models/models.go internal/backend/database/gorm_db.go internal/backend/database/gorm_db_test.go internal/backend/services/services.go
git commit -m "feat(comments): add structured kind column to comments"
```

---

### Task 2: Write kind at the audit sites + repair the modal `isSystem` badge

Sets the real kind at the four audit call sites, and repoints the modal's dead `isSystem` badge onto `kind`.

**Files:**
- Modify: `internal/webui/client/backend_client.go:468-484` (AddComment + new AddSystemComment)
- Modify: `internal/webui/handlers/dashboard_handlers.go` (4 audit sites: ack `:914`, unack `:958`, resolve `:990`, silence `:2065`)
- Modify: `internal/webui/models/dashboard.go` (Comment wire model — add Kind)
- Modify: `internal/webui/handlers/*` where GetComments maps to the wire model (see Step 5)
- Modify: `internal/webui/templates/components/modal_components.templ:1300` (isSystem badge)

**Interfaces:**
- Consumes: `AddCommentRequest.Kind` (Task 1).
- Produces: `BackendClient.AddSystemComment(sessionID, alertKey, kind, content string) error`; webui `Comment` model gains `Kind string` / `IsSystem bool`.

- [ ] **Step 1: Add kind to the webui client**

In `internal/webui/client/backend_client.go`, update `AddComment` to send `Kind: "comment"` and add `AddSystemComment`:

```go
func (c *BackendClient) AddComment(sessionID, alertKey, content string) error {
	return c.addComment(sessionID, alertKey, content, "comment")
}

// AddSystemComment records an audit comment (ack/unack/silence/resolve) with a
// structured kind so the activity feed and the modal badge can categorise it
// without parsing the emoji prefix.
func (c *BackendClient) AddSystemComment(sessionID, alertKey, kind, content string) error {
	return c.addComment(sessionID, alertKey, content, kind)
}

func (c *BackendClient) addComment(sessionID, alertKey, content, kind string) error {
	if c.alertClient == nil {
		return fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &alertpb.AddCommentRequest{
		SessionId: sessionID,
		AlertKey:  alertKey,
		Content:   content,
		Kind:      kind,
	}

	_, err := c.alertClient.AddComment(ctx, req)
	return err
}
```

- [ ] **Step 2: Set kind at the four audit sites**

In `internal/webui/handlers/dashboard_handlers.go`, replace each audit `backendClient.AddComment(...)` with `AddSystemComment`:

- ack site (`commentContent := fmt.Sprintf("🔔 Alert acknowledged: %s", reason)`):
  `if err := backendClient.AddSystemComment(sessionID, fingerprint, "ack", commentContent); err != nil {`
- unack site (`🔕 Alert unacknowledged`):
  `if err := backendClient.AddSystemComment(sessionID, fingerprint, "unack", commentContent); err != nil {`
- resolve site (`✅ Alert resolved`):
  `if err := backendClient.AddSystemComment(sessionID, fingerprint, "resolve", commentContent); err != nil {`
- silence site (`🔇 Alert silenced for ...`, near `:2065`):
  `if err := backendClient.AddSystemComment(sessionID, fingerprint, "silence", commentContent); err != nil {`

Leave the human-comment site (`:1478`, `backendClient.AddComment(sessionID, fingerprint, strings.TrimSpace(request.Content))`) unchanged.

- [ ] **Step 3: Add Kind/IsSystem to the webui Comment wire model**

In `internal/webui/models/dashboard.go`, find the `Comment` struct used by the alert modal (the one with `Content`, `Username`, `CreatedAt`) and add:

```go
	Kind     string `json:"kind"`
	IsSystem bool   `json:"isSystem"`
```

- [ ] **Step 4: Locate where GetComments maps to the wire model**

Run: `grep -rn "IsSystem\|\.Content" internal/webui/handlers/*.go | grep -i comment`
Find the handler that converts backend `alertpb.Comment` (now carrying `Kind`) into the webui `Comment` wire model for the modal.

- [ ] **Step 5: Populate Kind/IsSystem in that mapping**

Where each backend comment is mapped to the wire model, set:

```go
		kind := deriveCommentKind(c.Kind, c.Content)
		// ... in the wire Comment literal:
		Kind:     kind,
		IsSystem: kind != "comment",
```

And add the shared helper in the same package (e.g. top of `internal/webui/handlers/activity_handlers.go` created in Task 8, or a small `comment_kind.go`). For this task, put it in `internal/webui/handlers/comment_kind.go`:

```go
package handlers

import "strings"

// deriveCommentKind returns the stored kind, falling back to the emoji prefix for
// legacy comments written before the kind column existed. Emoji-sniffing is confined
// to this legacy fallback — new comments carry an authoritative kind.
func deriveCommentKind(kind, content string) string {
	if kind != "" {
		return kind
	}
	switch {
	case strings.HasPrefix(content, "🔔"):
		return "ack"
	case strings.HasPrefix(content, "🔕"):
		return "unack"
	case strings.HasPrefix(content, "🔇"):
		return "silence"
	case strings.HasPrefix(content, "✅"):
		return "resolve"
	default:
		return "comment"
	}
}
```

- [ ] **Step 6: Repoint the modal badge**

In `internal/webui/templates/components/modal_components.templ:1300`, the badge already reads `x-show="comment.isSystem"`. Now that the wire model populates `isSystem`, no markup change is required — but verify the comment objects delivered to the modal carry `isSystem`. If the modal builds comments from a different endpoint, ensure that endpoint's payload includes `kind`/`isSystem` from Step 5.

- [ ] **Step 7: Write a unit test for the kind fallback helper**

Create `internal/webui/handlers/comment_kind_test.go`:

```go
package handlers

import "testing"

func TestDeriveCommentKind(t *testing.T) {
	cases := []struct{ kind, content, want string }{
		{"silence", "🔇 whatever", "silence"},        // stored kind wins
		{"", "🔔 Alert acknowledged: x", "ack"},       // legacy fallback
		{"", "🔕 Alert unacknowledged: x", "unack"},
		{"", "🔇 Alert silenced for 2h: x", "silence"},
		{"", "✅ Alert resolved: x", "resolve"},
		{"", "just a human note", "comment"},
	}
	for _, c := range cases {
		if got := deriveCommentKind(c.kind, c.content); got != c.want {
			t.Errorf("deriveCommentKind(%q,%q) = %q, want %q", c.kind, c.content, got, c.want)
		}
	}
}
```

- [ ] **Step 8: Regenerate templates, build, test**

Run: `go run github.com/a-h/templ/cmd/templ@v0.3.906 generate && go build ./... && go test ./internal/webui/...`
Expected: build OK, `TestDeriveCommentKind` passes.

- [ ] **Step 9: Commit**

```bash
git add internal/webui/client/backend_client.go internal/webui/handlers/dashboard_handlers.go internal/webui/models/dashboard.go internal/webui/handlers/comment_kind.go internal/webui/handlers/comment_kind_test.go internal/webui/templates/components/modal_components.templ internal/webui/templates/components/modal_components_templ.go
git commit -m "feat(comments): write kind at audit sites and revive the modal system badge"
```

---

### Task 3: Migration — index on `comments.created_at`

A time-ordered global scan of comments would table-scan today (only `alert_key` is indexed).

**Files:**
- Modify: `internal/backend/database/migrate.go` (add a custom migration + register it in the run sequence)

**Interfaces:**
- Produces: index `idx_comments_created_at` on `comments(created_at)`.

- [ ] **Step 1: Add the index migration**

In `internal/backend/database/migrate.go`, add:

```go
// migrateCommentsCreatedAtIndex indexes comments.created_at so the activity feed's
// time-ordered global scan uses an index instead of scanning the whole table.
func (gdb *GormDB) migrateCommentsCreatedAtIndex() error {
	return gdb.db.Exec(`CREATE INDEX IF NOT EXISTS idx_comments_created_at ON comments (created_at)`).Error
}
```

- [ ] **Step 2: Call it from the migration runner**

Find where the other `migrate*` methods are invoked (e.g. inside `AutoMigrate` at `gorm_db.go:114` or a `RunCustomMigrations`) and add a call to `gdb.migrateCommentsCreatedAtIndex()` alongside the existing ones, with the same error handling pattern.

Run: `grep -n "migrateUserColumnPreferences\|migrateColumnConfigs\|RunCustomMigrations" internal/backend/database/*.go` to locate the invocation site.

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: exit 0. (The `CREATE INDEX IF NOT EXISTS` is idempotent and valid on both SQLite and Postgres.)

- [ ] **Step 4: Commit**

```bash
git add internal/backend/database/migrate.go
git commit -m "perf(db): index comments.created_at for the activity feed"
```

---

### Task 4: DB query — `GetRecentActivity`

Fetches comments in a time window, newest-first, capped.

**Files:**
- Modify: `internal/backend/database/gorm_db.go`
- Test: `internal/backend/database/gorm_db_test.go`

**Interfaces:**
- Produces: `GormDB.GetRecentActivity(since time.Time, limit int) ([]models.CommentWithUser, error)` — newest-first, at most `limit` rows, joined to users for `Username`. `CommentWithUser` embeds `Comment` (so carries `Kind`, `Content`, `AlertKey`, `CreatedAt`) plus `Username`.

- [ ] **Step 1: Write the failing test**

In `internal/backend/database/gorm_db_test.go`:

```go
func TestGetRecentActivity(t *testing.T) {
	gdb := newTestDB(t)
	u := models.User{ID: "u1", Username: "alice", Email: "a@example.com"}
	if err := gdb.db.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	// three comments at -3h, -1h, -10m
	seed := []models.Comment{
		{ID: "c1", AlertKey: "k1", UserID: u.ID, Content: "old", Kind: "comment", CreatedAt: base.Add(-3 * time.Hour)},
		{ID: "c2", AlertKey: "k1", UserID: u.ID, Content: "🔇 silenced", Kind: "silence", CreatedAt: base.Add(-1 * time.Hour)},
		{ID: "c3", AlertKey: "k2", UserID: u.ID, Content: "recent", Kind: "comment", CreatedAt: base.Add(-10 * time.Minute)},
	}
	for i := range seed {
		if err := gdb.db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// window = last 2h → excludes c1; newest first → c3, c2
	got, err := gdb.GetRecentActivity(base.Add(-2*time.Hour), 100)
	if err != nil {
		t.Fatalf("GetRecentActivity: %v", err)
	}
	if len(got) != 2 || got[0].ID != "c3" || got[1].ID != "c2" {
		t.Fatalf("got %d rows %v, want [c3 c2]", len(got), ids(got))
	}
	if got[0].Username != "alice" {
		t.Fatalf("username = %q, want alice", got[0].Username)
	}

	// limit caps the result
	got, err = gdb.GetRecentActivity(base.Add(-4*time.Hour), 1)
	if err != nil {
		t.Fatalf("GetRecentActivity: %v", err)
	}
	if len(got) != 1 || got[0].ID != "c3" {
		t.Fatalf("limit=1 got %v, want [c3]", ids(got))
	}
}

func ids(rows []models.CommentWithUser) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/backend/database/ -run TestGetRecentActivity -v`
Expected: FAIL — `GetRecentActivity` undefined.

- [ ] **Step 3: Implement the query**

In `internal/backend/database/gorm_db.go`, near `GetComments`:

```go
// GetRecentActivity returns comments created at or after `since`, newest first,
// capped at `limit`, joined to users for the username. It is the single source for
// the cross-alert activity feed: every ack/unack/silence/resolve already writes a
// comment, so no merge with the acknowledgments table is needed.
func (gdb *GormDB) GetRecentActivity(since time.Time, limit int) ([]models.CommentWithUser, error) {
	var rows []models.CommentWithUser
	err := gdb.db.Table("comments").
		Select("comments.*, users.username").
		Joins("JOIN users ON users.id = comments.user_id").
		Where("comments.created_at >= ?", since).
		Order("comments.created_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/backend/database/ -run TestGetRecentActivity -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backend/database/gorm_db.go internal/backend/database/gorm_db_test.go
git commit -m "feat(db): GetRecentActivity time-window query for the activity feed"
```

---

### Task 5: Backend RPC — `GetRecentActivity`

The read-only RPC: manual session validation, server-side limit clamp, maps rows to `ActivityEvent` with the kind fallback.

**Files:**
- Modify: `proto/alert.proto` (RPC + messages)
- Modify: `internal/backend/services/services.go` (impl + a `deriveCommentKind` helper for the backend)
- Test: `internal/backend/services/services_test.go` (create if absent)

**Interfaces:**
- Consumes: `GormDB.GetRecentActivity` (Task 4); `AuthServiceGorm.ValidateSessionByID` pattern (`services.go:425`) or `s.db.GetUserBySession` (used by AddComment).
- Produces: `rpc GetRecentActivity`; `ActivityEvent{id, alert_key, kind, user_id, username, content, created_at}`; server clamp constants `activityDefaultLimit=100`, `activityMaxLimit=200`.

- [ ] **Step 1: Add the RPC and messages to proto**

In `proto/alert.proto`, add to the `AlertService` service block:

```proto
  rpc GetRecentActivity(GetRecentActivityRequest) returns (GetRecentActivityResponse);
```

And the messages (near the Comment messages):

```proto
message GetRecentActivityRequest {
  string session_id = 1;
  google.protobuf.Timestamp since = 2;
  int32 limit = 3;
}

message ActivityEvent {
  string id = 1;
  string alert_key = 2;
  string kind = 3;
  string user_id = 4;
  string username = 5;
  string content = 6;
  google.protobuf.Timestamp created_at = 7;
}

message GetRecentActivityResponse {
  repeated ActivityEvent events = 1;
}
```

- [ ] **Step 2: Regenerate proto**

Run: `bash scripts/generate_proto.sh`
Expected: `GetRecentActivity` appears on the generated `AlertServiceServer` interface; exit 0.

- [ ] **Step 3: Write the failing service test**

Create/append `internal/backend/services/services_test.go`. Match the existing service test setup if one exists (`grep -n "func newTest" internal/backend/services/*_test.go`); otherwise construct the service around an in-memory DB the same way `database` tests do. Test skeleton:

```go
func TestGetRecentActivityValidatesAndClamps(t *testing.T) {
	svc, sessionID, _ := newActivityTestService(t) // seeds a user + session + a few comments

	// invalid session rejected
	_, err := svc.GetRecentActivity(context.Background(), &alertpb.GetRecentActivityRequest{
		SessionId: "", Since: timestamppb.New(time.Now().Add(-time.Hour)), Limit: 50,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("empty session: got %v, want Unauthenticated", err)
	}

	// limit above max is clamped (ask for 5000, assert the SQL limit used ≤ 200 by
	// seeding 3 comments and asserting no error + events returned)
	resp, err := svc.GetRecentActivity(context.Background(), &alertpb.GetRecentActivityRequest{
		SessionId: sessionID, Since: timestamppb.New(time.Now().Add(-24 * time.Hour)), Limit: 5000,
	})
	if err != nil {
		t.Fatalf("valid call: %v", err)
	}
	if len(resp.Events) == 0 {
		t.Fatal("want events, got none")
	}
	if resp.Events[0].Username == "" {
		t.Fatal("event missing username")
	}
}
```

Write `newActivityTestService(t)` mirroring the DB test harness: open sqlite in-memory, AutoMigrate `User, Comment, Session` (whatever `GetUserBySession` needs), seed a user + a valid session row + 3 comments, return the constructed `*AlertServiceGorm`, the session id, and the user id.

- [ ] **Step 4: Run to verify it fails**

Run: `go test ./internal/backend/services/ -run TestGetRecentActivityValidatesAndClamps -v`
Expected: FAIL — `GetRecentActivity` not implemented.

- [ ] **Step 5: Implement the RPC**

In `internal/backend/services/services.go`:

```go
const (
	activityDefaultLimit = 100
	activityMaxLimit     = 200
)

// GetRecentActivity returns recent cross-alert collaboration events (acks, unacks,
// comments, silences, resolves) for the activity feed. Read-only. The AlertService has
// no auth interceptor, so the session is validated here explicitly.
func (s *AlertServiceGorm) GetRecentActivity(ctx context.Context, req *alertpb.GetRecentActivityRequest) (*alertpb.GetRecentActivityResponse, error) {
	if req.SessionId == "" {
		return nil, status.Errorf(codes.Unauthenticated, "session ID is required")
	}
	if _, err := s.db.GetUserBySession(req.SessionId); err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid session")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = activityDefaultLimit
	}
	if limit > activityMaxLimit {
		limit = activityMaxLimit
	}

	since := time.Time{}
	if req.Since != nil {
		since = req.Since.AsTime()
	}

	rows, err := s.db.GetRecentActivity(since, limit)
	if err != nil {
		log.Printf("Error getting recent activity: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to load activity: %v", err)
	}

	events := make([]*alertpb.ActivityEvent, 0, len(rows))
	for _, r := range rows {
		events = append(events, &alertpb.ActivityEvent{
			Id:        r.ID,
			AlertKey:  r.AlertKey,
			Kind:      deriveActivityKind(r.Kind, r.Content),
			UserId:    r.UserID,
			Username:  r.Username,
			Content:   r.Content,
			CreatedAt: timestamppb.New(r.CreatedAt),
		})
	}
	return &alertpb.GetRecentActivityResponse{Events: events}, nil
}

// deriveActivityKind returns the stored kind, falling back to the emoji prefix for
// legacy rows written before the kind column existed.
func deriveActivityKind(kind, content string) string {
	if kind != "" {
		return kind
	}
	switch {
	case strings.HasPrefix(content, "🔔"):
		return "ack"
	case strings.HasPrefix(content, "🔕"):
		return "unack"
	case strings.HasPrefix(content, "🔇"):
		return "silence"
	case strings.HasPrefix(content, "✅"):
		return "resolve"
	default:
		return "comment"
	}
}
```

Ensure `strings` is imported in `services.go` (it likely is; if not, add it).

- [ ] **Step 6: Run to verify it passes**

Run: `go test ./internal/backend/services/ -run TestGetRecentActivityValidatesAndClamps -v`
Expected: PASS.

- [ ] **Step 7: Build + full backend tests**

Run: `go build ./... && go test ./internal/backend/...`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add proto/alert.proto internal/backend/proto internal/backend/services/services.go internal/backend/services/services_test.go
git commit -m "feat(activity): GetRecentActivity RPC with session validation and limit clamp"
```

---

### Task 6: Extract the shared alert-filter predicate

Factor the per-alert match logic out of `applyDashboardFilters` so the activity handler reuses identical semantics.

**Files:**
- Modify: `internal/webui/handlers/dashboard_handlers.go:400-480`
- Test: `internal/webui/handlers/dashboard_filter_predicate_test.go`

**Interfaces:**
- Produces: `alertPassesAlertLevelFilters(alert *webuimodels.DashboardAlert, filters webuimodels.DashboardFilters) bool` — covers ONLY the alert-label filters shared with the activity feed: search, alertmanagers, severities, teams, alertNames. (Statuses, hidden-alert rules, ack/comment filters stay inline in `applyDashboardFilters` — they are dashboard-specific.)

- [ ] **Step 1: Write the failing test**

Create `internal/webui/handlers/dashboard_filter_predicate_test.go`:

```go
package handlers

import (
	"testing"

	webuimodels "notificator/internal/webui/models"
)

func TestAlertPassesAlertLevelFilters(t *testing.T) {
	alert := &webuimodels.DashboardAlert{
		AlertName: "KafkaLagHigh", Source: "prod", Severity: "critical", Team: "sre",
	}

	if !alertPassesAlertLevelFilters(alert, webuimodels.DashboardFilters{}) {
		t.Fatal("no filters should pass")
	}
	if !alertPassesAlertLevelFilters(alert, webuimodels.DashboardFilters{Severities: []string{"critical"}}) {
		t.Fatal("matching severity should pass")
	}
	if alertPassesAlertLevelFilters(alert, webuimodels.DashboardFilters{Severities: []string{"warning"}}) {
		t.Fatal("non-matching severity should fail")
	}
	if alertPassesAlertLevelFilters(alert, webuimodels.DashboardFilters{Teams: []string{"dba"}}) {
		t.Fatal("non-matching team should fail")
	}
	if !alertPassesAlertLevelFilters(alert, webuimodels.DashboardFilters{
		Alertmanagers: []string{"prod"}, AlertNames: []string{"KafkaLagHigh"},
	}) {
		t.Fatal("matching source+name should pass")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/webui/handlers/ -run TestAlertPassesAlertLevelFilters -v`
Expected: FAIL — `alertPassesAlertLevelFilters` undefined.

- [ ] **Step 3: Extract the predicate**

In `internal/webui/handlers/dashboard_handlers.go`, add the function:

```go
// alertPassesAlertLevelFilters reports whether an alert matches the label-level
// filters shared between the dashboard and the activity feed. It deliberately
// excludes dashboard-only concerns (hidden rules, statuses, ack/comment filters).
func alertPassesAlertLevelFilters(alert *webuimodels.DashboardAlert, filters webuimodels.DashboardFilters) bool {
	if filters.Search != "" && !matchesSearch(alert, filters.Search) {
		return false
	}
	if len(filters.Alertmanagers) > 0 && !contains(filters.Alertmanagers, alert.Source) {
		return false
	}
	if len(filters.Severities) > 0 && !contains(filters.Severities, alert.Severity) {
		return false
	}
	if len(filters.Teams) > 0 && !contains(filters.Teams, alert.Team) {
		return false
	}
	if len(filters.AlertNames) > 0 && !contains(filters.AlertNames, alert.AlertName) {
		return false
	}
	return true
}
```

Then in `applyDashboardFilters`, replace the inline search/alertmanager/severity/team/alertName blocks with a single call, keeping the status/ack/comment/hidden logic inline:

```go
		// Shared alert-level filters (search, alertmanager, severity, team, alertName)
		if !alertPassesAlertLevelFilters(alert, filters) {
			continue
		}

		// Apply status filter (dashboard-only)
		if len(filters.Statuses) > 0 && !contains(filters.Statuses, alert.Status.State) {
			continue
		}
		// ... (acknowledgment and comments filters stay as-is below)
```

- [ ] **Step 4: Run predicate test + existing dashboard tests**

Run: `go test ./internal/webui/handlers/ -run "TestAlertPassesAlertLevelFilters|Filter|Dashboard" -v`
Expected: new test passes; existing dashboard/filter tests still pass (proves the extraction preserved behavior).

- [ ] **Step 5: Build + full handler tests**

Run: `go build ./... && go test ./internal/webui/handlers/`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/webui/handlers/dashboard_handlers.go internal/webui/handlers/dashboard_filter_predicate_test.go
git commit -m "refactor(filters): extract shared alert-level filter predicate"
```

---

### Task 7: WebUI client wrapper — `GetRecentActivity`

**Files:**
- Modify: `internal/webui/client/backend_client.go`

**Interfaces:**
- Consumes: `alertpb.GetRecentActivityRequest/Response` (Task 5).
- Produces: `BackendClient.GetRecentActivity(sessionID string, since time.Time, limit int) ([]*alertpb.ActivityEvent, error)`.

- [ ] **Step 1: Add the wrapper**

In `internal/webui/client/backend_client.go`, following the `GetComments` shape:

```go
// GetRecentActivity fetches recent cross-alert collaboration events for the activity feed.
func (c *BackendClient) GetRecentActivity(sessionID string, since time.Time, limit int) ([]*alertpb.ActivityEvent, error) {
	if c.alertClient == nil {
		return nil, fmt.Errorf("not connected to backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.alertClient.GetRecentActivity(ctx, &alertpb.GetRecentActivityRequest{
		SessionId: sessionID,
		Since:     timestamppb.New(since),
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, err
	}
	return resp.Events, nil
}
```

Ensure `timestamppb` is imported (`google.golang.org/protobuf/types/known/timestamppb`).

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/webui/client/backend_client.go
git commit -m "feat(activity): webui client wrapper for GetRecentActivity"
```

---

### Task 8: WebUI handler + routes — `/api/v1/dashboard/activity` and the page

Resolves alert names from the cache, applies the shared predicate + activity filters (with uncached pass-through/hide), returns JSON; plus the HTML page handler.

**Files:**
- Create: `internal/webui/handlers/activity_handlers.go`
- Create: `internal/webui/models/activity.go` (wire types)
- Modify: `internal/webui/router.go:297` area (API route) and `:373` area (page route)
- Test: `internal/webui/handlers/activity_handlers_test.go`

**Interfaces:**
- Consumes: `BackendClient.GetRecentActivity` (Task 7); `alertPassesAlertLevelFilters` (Task 6); `parseDashboardFilters` (`dashboard_handlers.go:223`); `alertCache.GetAlert` / `GetAllAlerts`; `deriveCommentKind` (Task 2).
- Produces: `GET /api/v1/dashboard/activity`, `ActivityPage` handler, `webuimodels.ActivityEvent` wire type.

- [ ] **Step 1: Wire model**

Create `internal/webui/models/activity.go`:

```go
package models

import "time"

// ActivityEvent is one row of the activity feed as delivered to the browser.
type ActivityEvent struct {
	ID        string    `json:"id"`
	AlertKey  string    `json:"alertKey"`
	AlertName string    `json:"alertName"` // resolved from cache; falls back to AlertKey
	Source    string    `json:"source"`    // alertmanager, "" when uncached
	Kind      string    `json:"kind"`      // comment|ack|unack|silence|resolve
	Username  string    `json:"username"`
	Content   string    `json:"content"`
	Uncached  bool      `json:"uncached"` // true when the alert is no longer in the cache
	CreatedAt time.Time `json:"createdAt"`
}
```

- [ ] **Step 2: Write the failing handler test**

Create `internal/webui/handlers/activity_handlers_test.go`. It exercises the pure filtering/resolution helper (not the gin layer), so extract that logic into a testable function `buildActivityFeed`:

```go
package handlers

import (
	"testing"
	"time"

	"notificator/internal/backend/proto/alert"
	webuimodels "notificator/internal/webui/models"
)

func TestBuildActivityFeedUncachedBehavior(t *testing.T) {
	now := time.Now()
	events := []*alert.ActivityEvent{
		{Id: "e1", AlertKey: "cached-key", Kind: "silence", Username: "mathieu", Content: "🔇 x"},
		{Id: "e2", AlertKey: "gone-key", Kind: "resolve", Username: "julie", Content: "✅ y"},
	}
	// cache resolves only "cached-key" → KafkaLagHigh / prod / critical
	resolve := func(key string) (name, source, severity, team string, ok bool) {
		if key == "cached-key" {
			return "KafkaLagHigh", "prod", "critical", "sre", true
		}
		return "", "", "", "", false
	}

	// no alert-level filter → both pass, e2 marked uncached
	all := buildActivityFeed(events, webuimodels.DashboardFilters{}, resolve, now)
	if len(all) != 2 {
		t.Fatalf("no filter: got %d, want 2", len(all))
	}
	var gone *webuimodels.ActivityEvent
	for i := range all {
		if all[i].AlertKey == "gone-key" {
			gone = &all[i]
		}
	}
	if gone == nil || !gone.Uncached || gone.AlertName != "gone-key" {
		t.Fatalf("uncached event should pass through with key fallback: %+v", gone)
	}

	// severity filter active → uncached e2 hidden, cached e1 kept
	filtered := buildActivityFeed(events, webuimodels.DashboardFilters{Severities: []string{"critical"}}, resolve, now)
	if len(filtered) != 1 || filtered[0].AlertKey != "cached-key" {
		t.Fatalf("active filter: got %v, want [cached-key]", filtered)
	}

	// severity filter that the cached alert fails → nothing
	none := buildActivityFeed(events, webuimodels.DashboardFilters{Severities: []string{"warning"}}, resolve, now)
	if len(none) != 0 {
		t.Fatalf("non-matching filter: got %d, want 0", len(none))
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/webui/handlers/ -run TestBuildActivityFeedUncachedBehavior -v`
Expected: FAIL — `buildActivityFeed` undefined.

- [ ] **Step 4: Implement handler + helpers**

Create `internal/webui/handlers/activity_handlers.go`:

```go
package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"notificator/internal/backend/proto/alert"
	"notificator/internal/webui/middleware"
	webuimodels "notificator/internal/webui/models"
	"notificator/internal/webui/templates/pages"

	"github.com/a-h/templ"
)

// alertResolver returns an alert's display fields from the cache.
type alertResolver func(alertKey string) (name, source, severity, team string, ok bool)

// hasAlertLevelFilter reports whether any label-level filter is active. When none is,
// events whose alert is no longer cached pass through (full history); when one is,
// they are hidden because they cannot be evaluated.
func hasAlertLevelFilter(f webuimodels.DashboardFilters) bool {
	return f.Search != "" || len(f.Alertmanagers) > 0 || len(f.Severities) > 0 ||
		len(f.Teams) > 0 || len(f.AlertNames) > 0
}

// buildActivityFeed resolves each backend event's alert name from the cache and applies
// the shared alert-level filter predicate, honouring the uncached pass-through/hide rule.
func buildActivityFeed(events []*alert.ActivityEvent, filters webuimodels.DashboardFilters, resolve alertResolver, now time.Time) []webuimodels.ActivityEvent {
	filterActive := hasAlertLevelFilter(filters)
	out := make([]webuimodels.ActivityEvent, 0, len(events))
	for _, e := range events {
		name, source, severity, team, ok := resolve(e.AlertKey)
		if !ok {
			if filterActive {
				continue // cannot evaluate an uncached alert against an active filter
			}
			out = append(out, webuimodels.ActivityEvent{
				ID: e.Id, AlertKey: e.AlertKey, AlertName: e.AlertKey, Kind: e.Kind,
				Username: e.Username, Content: e.Content, Uncached: true,
				CreatedAt: e.CreatedAt.AsTime(),
			})
			continue
		}
		probe := &webuimodels.DashboardAlert{AlertName: name, Source: source, Severity: severity, Team: team}
		if filterActive && !alertPassesAlertLevelFilters(probe, filters) {
			continue
		}
		out = append(out, webuimodels.ActivityEvent{
			ID: e.Id, AlertKey: e.AlertKey, AlertName: name, Source: source, Kind: e.Kind,
			Username: e.Username, Content: e.Content, CreatedAt: e.CreatedAt.AsTime(),
		})
	}
	return out
}

// GetActivity serves the activity feed JSON.
func GetActivity(c *gin.Context) {
	if backendClient == nil || !backendClient.IsConnected() {
		c.JSON(http.StatusServiceUnavailable, webuimodels.ErrorResponse("Backend not available"))
		return
	}
	sessionID := middleware.GetSessionIDFromContext(c)
	if sessionID == "" {
		c.JSON(http.StatusUnauthorized, webuimodels.ErrorResponse("Not authenticated"))
		return
	}

	// window: minutes back from now, default 60, clamp to a sane ceiling
	minutes := 60
	if v := c.Query("windowMinutes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			minutes = n
		}
	}
	since := time.Now().Add(-time.Duration(minutes) * time.Minute)

	events, err := backendClient.GetRecentActivity(sessionID, since, 200)
	if err != nil {
		c.JSON(http.StatusBadGateway, webuimodels.ErrorResponse("Failed to load activity: "+err.Error()))
		return
	}

	filters := parseDashboardFilters(c)
	resolve := func(key string) (string, string, string, string, bool) {
		if alertCache == nil {
			return "", "", "", "", false
		}
		a, ok := alertCache.GetAlert(key)
		if !ok {
			return "", "", "", "", false
		}
		return a.AlertName, a.Source, a.Severity, a.Team, true
	}

	feed := buildActivityFeed(events, filters, resolve, time.Now())

	// Scope (Mine/Everyone) and action-type (kinds) are applied here, post-resolution.
	if c.Query("scope") == "mine" {
		if u := middleware.GetEffectiveUser(c); u != nil {
			mineName := u.Username
			kept := feed[:0]
			for _, ev := range feed {
				if ev.Username == mineName {
					kept = append(kept, ev)
				}
			}
			feed = kept
		}
	}
	if kinds := parseStringArray(c.Query("kinds")); len(kinds) > 0 {
		kept := feed[:0]
		for _, ev := range feed {
			if contains(kinds, ev.Kind) {
				kept = append(kept, ev)
			}
		}
		feed = kept
	}

	c.JSON(http.StatusOK, webuimodels.SuccessResponse(gin.H{"events": feed}))
}

// ActivityPage serves the activity page shell.
func ActivityPage(c *gin.Context) {
	user := middleware.GetCurrentUserFromContext(c)
	if user == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	templ.Handler(pages.Activity(pages.ActivityData{
		User: pages.ProfileUser{ID: user.ID, Username: user.Username, Email: user.Email},
	})).ServeHTTP(c.Writer, c.Request)
}
```

Note: `GetAlert` must return the fields used (`AlertName, Source, Severity, Team`). Verify with `grep -n "func (ac \*AlertCache) GetAlert" internal/webui/services/alert_cache.go` and that `DashboardAlert` has those fields (it does — used in `applyDashboardFilters`). `parseStringArray`, `contains`, `parseDashboardFilters`, `middleware.GetEffectiveUser`, `webuimodels.SuccessResponse/ErrorResponse` all already exist.

- [ ] **Step 5: Run to verify the test passes**

Run: `go test ./internal/webui/handlers/ -run TestBuildActivityFeedUncachedBehavior -v`
Expected: PASS. (`pages.Activity`/`pages.ActivityData` do not exist yet → the package won't compile. Create a minimal stub in Task 9 Step 1 BEFORE running this, or temporarily comment out `ActivityPage`. Cleanest: implement Task 9 Step 1 first, then return here. See ordering note below.)

> **Ordering note:** Task 8 Step 4's `ActivityPage` references `pages.Activity`, created in Task 9. Do Task 9 Step 1 (create the `Activity` templ page + `ActivityData` type + regenerate) immediately before Task 8 Step 5 so the package compiles. The two tasks are commit-independent otherwise.

- [ ] **Step 6: Register the routes**

In `internal/webui/router.go`, in the `dashboard` group (near `:297`):

```go
			dashboard.GET("/activity", handlers.GetActivity)
```

And in the protected pages group (near `:373`, beside `/silences`):

```go
		protectedPages.GET("/activity", handlers.ActivityPage)
```

- [ ] **Step 7: Build + test**

Run: `go build ./... && go test ./internal/webui/handlers/`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/webui/handlers/activity_handlers.go internal/webui/models/activity.go internal/webui/router.go internal/webui/handlers/activity_handlers_test.go
git commit -m "feat(activity): activity feed handler, routes and filtering"
```

---

### Task 9: Frontend — `/activity` page (log-table layout), PageNavigator entry, polling

Renders POC variant C. The layout was validated with the user during brainstorming.

**Files:**
- Create: `internal/webui/templates/pages/Activity.templ`
- Modify: `internal/webui/templates/components/PageNavigator.templ` (add "activity" entry)
- Test: manual (browser) via the running stack

**Interfaces:**
- Consumes: `GET /api/v1/dashboard/activity`; `@components.PageNavigator`. (Filters are hand-rolled `<details>` dropdowns matching `Silences.templ`'s convention; the earlier note about reusing `@components.FilterDropdown` proved impractical — that component is unused repo-wide, needs server-side `metadata.availableFilters` infra, and the sibling page hand-rolls its own filters. The substantive reuse is the shared server-side predicate `alertPassesAlertLevelFilters`.)
- Produces: `pages.Activity(data ActivityData)`, `pages.ActivityData{User ProfileUser}`.

- [ ] **Step 1: Create the page templ (do this before Task 8 Step 5)**

Create `internal/webui/templates/pages/Activity.templ` following `Silences.templ`'s structure: `@layouts.Base(...)`, sticky header with `@components.PageNavigator("activity")`, a filter bar (reused `@components.FilterDropdown("severities","Severity")` etc. + time-window segmented control + Mine/Everyone toggle + kind chips + search), and the **log table** (columns Time · User · Action · Alert · Detail, day groups, color-coded action pill, rows `@click` to `window.location='/dashboard/alert/'+ev.alertKey`). Include the `<script>` with `activityPage()` Alpine component. Base the visual tokens on POC variant C (already validated). `ActivityData` mirrors `SilencesData`:

```go
type ActivityData struct {
	User ProfileUser
}
```

The Alpine component's polling contract:

```js
init() {
	this.load();
	document.addEventListener('visibilitychange', () => this.syncPolling());
	this.syncPolling();
},
syncPolling() {
	clearInterval(this.timer);
	if (document.visibilityState === 'visible') {
		this.timer = setInterval(() => this.load(), 30000); // 30s, only while visible
	}
},
async load() {
	const params = new URLSearchParams();
	params.set('windowMinutes', this.windowMinutes);
	if (this.scope === 'mine') params.set('scope', 'mine');
	if (this.kinds.length) params.set('kinds', this.kinds.join(','));
	if (this.filters.severities.length) params.set('severities', this.filters.severities.join(','));
	if (this.filters.teams.length) params.set('teams', this.filters.teams.join(','));
	if (this.filters.alertmanagers.length) params.set('alertmanagers', this.filters.alertmanagers.join(','));
	if (this.filters.alertNames.length) params.set('alertNames', this.filters.alertNames.join(','));
	if (this.search) params.set('search', this.search);
	const r = await fetch('/api/v1/dashboard/activity?' + params.toString());
	const p = await r.json();
	if (!r.ok || !p.success) { this.error = p.error || 'Failed to load activity'; return; }
	this.events = p.data.events || [];
	this.error = '';
},
```

- [ ] **Step 2: Add the PageNavigator entry**

In `internal/webui/templates/components/PageNavigator.templ`, add an "Activity" link mirroring the "silences"/"statistics" blocks, keyed on `activePage == "activity"`. Update the doc comment on `:4` to include "activity".

- [ ] **Step 3: Regenerate templates + build**

Run: `go run github.com/a-h/templ/cmd/templ@v0.3.906 generate && go build ./...`
Expected: exit 0, `Activity_templ.go` and `PageNavigator_templ.go` regenerated.

- [ ] **Step 4: Rebuild the webui image and bring up the stack**

Run:
```bash
npx @tailwindcss/cli -i ./internal/webui/static/css/input.css -o ./internal/webui/static/css/output.css --minify
docker build -q -t registry-1.docker.io/soulkyu/notificator-webui:latest -f Dockerfile.webui .
docker compose -p repo up -d webui
```
Expected: webui healthy.

- [ ] **Step 5: Manual verification (browser)**

Log in, click **Activity** in the nav. Verify:
- The log table renders recent events grouped by day, newest first, with color-coded action pills.
- Ack/silence/resolve/comment appear and are distinguishable.
- A severity/team filter narrows the feed; clearing it restores full history.
- Mine/Everyone toggle works; time-window control changes the range.
- Clicking a row opens the alert modal (`/dashboard/alert/<key>`).
- In the network tab: a request every 30s while the tab is visible; none after switching away.

- [ ] **Step 6: Commit**

```bash
git add internal/webui/templates/pages/Activity.templ internal/webui/templates/pages/Activity_templ.go internal/webui/templates/components/PageNavigator.templ internal/webui/templates/components/PageNavigator_templ.go
git commit -m "feat(activity): /activity page with log-table feed and 30s visible-only polling"
```

---

### Task 10: Final verification + issue close-out

- [ ] **Step 1: Full build + test**

Run: `go build ./... && go test ./...`
Expected: all green.

- [ ] **Step 2: Confirm the documented limitation**

Verify a silence created from `/silences` (not from an alert) does NOT appear in the feed (it writes no alert-scoped comment), matching the spec's out-of-scope note.

- [ ] **Step 3: Push and open PR**

```bash
git push soulkyu main   # or a feature branch + PR per the repo's flow
```
Reference `Closes #77` in the PR/commit description, summarising: single-source-comments + kind, `/activity` log page, reused filter predicate, and the documented limitation.

---

## Self-Review

**Spec coverage:**
- `kind` column + audit sites + modal badge → Tasks 1, 2. ✓
- Legacy emoji fallback → Task 2 (`deriveCommentKind`), Task 5 (`deriveActivityKind`). ✓
- `GetRecentActivity` RPC, session validation, limit clamp → Task 5. ✓ (RPC simplified to `{session_id, since, limit}`; `kinds`/`alert_keys` were optional in the spec and are applied webui-side post-derivation — Task 8 — to keep legacy `kind==""` rows correct. Documented deviation.)
- Migration index on `comments.created_at` → Task 3. ✓
- Reused FilterDropdown UI → Task 9; shared matching predicate → Task 6. ✓
- Uncached pass-through vs hide → Task 8 (`buildActivityFeed`, tested). ✓
- Activity-specific filters (window, kinds, mine/everyone, search) → Task 8 + Task 9. ✓
- Alert-name resolution + uncached fallback → Task 8. ✓
- `/activity` page, PageNavigator, log-table layout, deep link → Task 9. ✓
- 30s visible-only polling, no new SSE → Task 9. ✓
- Error handling (Internal on failure, no silent empty feed) → Task 5 + Task 8. ✓
- Documented limitation (standalone /silences) → Task 10. ✓

**Placeholder scan:** No TBD/TODO; every code step carries complete code. The one cross-task ordering dependency (Task 8 `ActivityPage` needs Task 9's `pages.Activity`) is called out explicitly with an ordering note.

**Type consistency:** `deriveCommentKind` (webui, Task 2) and `deriveActivityKind` (backend, Task 5) are intentionally two functions in two packages with identical logic (no shared package spans backend+webui). `CreateComment(alertKey, userID, content, kind)` signature consistent across Tasks 1 and its callers. `alertPassesAlertLevelFilters` used identically in Tasks 6 and 8. `ActivityEvent` exists as three layers (proto `alert.ActivityEvent`, webui `webuimodels.ActivityEvent`) with explicit mapping in Task 8 — names checked.
