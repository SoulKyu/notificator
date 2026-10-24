package models

import (
	"os"
	"regexp"
	"strings"
	"testing"

	backendmodels "notificator/internal/backend/models"
)

// The dashboard rejected every column-preference and filter-preset save for
// weeks because renderCell() learned the "ackage" formatter and the server-side
// allowlist did not. Read the formatters renderCell actually dispatches on and
// require the allowlist to cover them.
func TestValidColumnFormattersCoverRenderCell(t *testing.T) {
	const source = "../templates/scripts/dashboard_utilities.templ"

	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}

	body := string(data)
	start := strings.Index(body, "renderCell(alert, column) {")
	if start < 0 {
		t.Fatalf("renderCell() not found in %s - update this test's anchor", source)
	}
	end := strings.Index(body[start:], "getFieldValue(alert, fieldPath) {")
	if end < 0 {
		t.Fatalf("end of renderCell() not found in %s - update this test's anchor", source)
	}

	cases := regexp.MustCompile(`case '([a-zA-Z]+)':`).FindAllStringSubmatch(body[start:start+end], -1)
	if len(cases) == 0 {
		t.Fatalf("no formatter cases found in renderCell()")
	}

	for _, match := range cases {
		if !backendmodels.ValidColumnFormatters[match[1]] {
			t.Errorf("renderCell handles formatter %q but ValidColumnFormatters rejects it: saves carrying that column would 400", match[1])
		}
	}
}

func TestNormalizeColumnConfigs(t *testing.T) {
	ackAge := ColumnConfig{ID: "col_ack_age", FieldType: "system", FieldPath: "acknowledgedAt", Formatter: "ackage", Width: 130, Order: 0}

	if _, err := NormalizeColumnConfigs([]ColumnConfig{ackAge}); err != nil {
		t.Fatalf("ack age column must be saveable: %v", err)
	}

	bogus := ackAge
	bogus.Formatter = "definitely-not-a-formatter"
	if _, err := NormalizeColumnConfigs([]ColumnConfig{bogus}); err == nil {
		t.Fatal("unknown formatter must be rejected")
	}

	sameID := ackAge
	sameID.Order = 7
	if _, err := NormalizeColumnConfigs([]ColumnConfig{ackAge, sameID}); err == nil {
		t.Fatal("duplicate column ID must be rejected")
	}

	// A layout carrying two columns at the same order used to fail every save
	// with no way out of the Column Config modal. It is a position, so it is
	// repaired rather than rejected - and the input slice is left untouched.
	custom := ackAge
	custom.ID = "col_custom_label_env"
	custom.Formatter = "text"
	custom.FieldType = "label"
	later := custom
	later.ID = "col_owner"
	later.Order = 5

	input := []ColumnConfig{later, ackAge, custom}
	normalized, err := NormalizeColumnConfigs(input)
	if err != nil {
		t.Fatalf("duplicate order must be repaired, not rejected: %v", err)
	}
	for i, col := range normalized {
		if col.Order != i {
			t.Fatalf("column %q: want order %d, got %d", col.ID, i, col.Order)
		}
	}
	if normalized[2].ID != "col_owner" {
		t.Fatalf("higher incoming order must sort last, got %q", normalized[2].ID)
	}
	if input[0].Order != 5 {
		t.Fatalf("input slice must not be mutated, got order %d", input[0].Order)
	}
}
