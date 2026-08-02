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

func TestValidateColumnConfigs(t *testing.T) {
	ackAge := ColumnConfig{ID: "col_ack_age", FieldType: "system", FieldPath: "acknowledgedAt", Formatter: "ackage", Width: 130, Order: 0}

	if err := ValidateColumnConfigs([]ColumnConfig{ackAge}); err != nil {
		t.Fatalf("ack age column must be saveable: %v", err)
	}

	bogus := ackAge
	bogus.Formatter = "definitely-not-a-formatter"
	if err := ValidateColumnConfigs([]ColumnConfig{bogus}); err == nil {
		t.Fatal("unknown formatter must be rejected")
	}

	dupe := ackAge
	dupe.ID = "col_other"
	if err := ValidateColumnConfigs([]ColumnConfig{ackAge, dupe}); err == nil {
		t.Fatal("duplicate order must be rejected")
	}
}
