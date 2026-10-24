package models

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const presetCaptureScript = "../templates/scripts/dashboard_filter_presets.templ"

// TestFilterPresetDataCoversCapturedKeys pins the browser-to-server contract for
// saved filters. captureCurrentFilterState() is the only producer of filter_data
// and ShouldBindJSON silently drops every key FilterPresetData does not declare,
// so a filter added to the dashboard without a matching field here is persisted
// as if it never existed - that is how the "Mine" toggle disappeared on preset
// save/restore. This turns that silent drop into a test failure.
func TestFilterPresetDataCoversCapturedKeys(t *testing.T) {
	src, err := os.ReadFile(presetCaptureScript)
	if err != nil {
		t.Fatalf("read %s: %v", presetCaptureScript, err)
	}

	captured := capturedFilterKeys(t, string(src))
	if len(captured) < 10 {
		t.Fatalf("only %d keys parsed out of captureCurrentFilterState() (%v) - the parser drifted from the script", len(captured), captured)
	}

	tags := jsonTagNames(FilterPresetData{})
	for _, key := range captured {
		if !tags[key] {
			t.Errorf("captureCurrentFilterState() sends %q but FilterPresetData has no field with json tag %q - the value is dropped on save", key, key)
		}
	}
}

// capturedFilterKeys returns the top-level keys of the object literal returned by
// captureCurrentFilterState().
func capturedFilterKeys(t *testing.T, src string) []string {
	t.Helper()

	fn := strings.Index(src, "captureCurrentFilterState()")
	if fn < 0 {
		t.Fatalf("captureCurrentFilterState() not found in %s", presetCaptureScript)
	}
	open := strings.Index(src[fn:], "return {")
	if open < 0 {
		t.Fatalf("captureCurrentFilterState() has no object literal")
	}
	body := src[fn+open+len("return {"):]

	depth := 1
	end := -1
	for i, c := range body {
		switch c {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
		if depth == 0 {
			end = i
			break
		}
	}
	if end < 0 {
		t.Fatalf("unbalanced object literal in captureCurrentFilterState()")
	}

	keyLine := regexp.MustCompile(`(?m)^\s*([a-z_][a-z0-9_]*)\s*:`)
	var keys []string
	for _, m := range keyLine.FindAllStringSubmatch(body[:end], -1) {
		keys = append(keys, m[1])
	}
	return keys
}

func jsonTagNames(v any) map[string]bool {
	typ := reflect.TypeOf(v)
	tags := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			tags[name] = true
		}
	}
	return tags
}
