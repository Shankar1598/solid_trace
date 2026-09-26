package config

import (
	"strings"
	"testing"
)

func TestValidateInternalToken(t *testing.T) {
	cases := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"unset", "", true},
		{"too short", strings.Repeat("a", minInternalTokenLength-1), true},
		{"minimum length", strings.Repeat("a", minInternalTokenLength), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateInternalToken(tc.token)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateInternalToken(%d bytes) error = %v, wantErr %v", len(tc.token), err, tc.wantErr)
			}
		})
	}
}
