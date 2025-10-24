package models

import "time"

// Helper functions for common operations on model types

// IntPtr returns a pointer to the given int value
// Useful for setting optional int fields in models
func IntPtr(i int) *int {
	return &i
}

// TimePtr returns a pointer to the given time.Time value
// Useful for setting optional time fields in models
func TimePtr(t time.Time) *time.Time {
	return &t
}

// StringPtr returns a pointer to the given string value
// Useful for setting optional string fields in models
func StringPtr(s string) *string {
	return &s
}

// BoolPtr returns a pointer to the given bool value
// Useful for setting optional bool fields in models
func BoolPtr(b bool) *bool {
	return &b
}

// Int64Ptr returns a pointer to the given int64 value
// Useful for setting optional int64 fields in models
func Int64Ptr(i int64) *int64 {
	return &i
}

// Float64Ptr returns a pointer to the given float64 value
// Useful for setting optional float64 fields in models
func Float64Ptr(f float64) *float64 {
	return &f
}

// Dereferencing helpers with default values

// IntValue returns the value pointed to by ptr, or the default value if ptr is nil
func IntValue(ptr *int, defaultValue int) int {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}

// TimeValue returns the value pointed to by ptr, or the default value if ptr is nil
func TimeValue(ptr *time.Time, defaultValue time.Time) time.Time {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}

// StringValue returns the value pointed to by ptr, or the default value if ptr is nil
func StringValue(ptr *string, defaultValue string) string {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}

// BoolValue returns the value pointed to by ptr, or the default value if ptr is nil
func BoolValue(ptr *bool, defaultValue bool) bool {
	if ptr == nil {
		return defaultValue
	}
	return *ptr
}
