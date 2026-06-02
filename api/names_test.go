package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseProxyUserName(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		expectedLoginName string
		expectedProxyUser string
		expectedAllowed   bool
		expectedString    string
	}{
		{
			name:              "no proxy user",
			input:             "alice",
			expectedLoginName: "alice",
			expectedProxyUser: "",
			expectedAllowed:   false,
			expectedString:    "alice",
		},
		{
			name:              "with proxy user",
			input:             "Sys As proxy",
			expectedLoginName: "Sys",
			expectedProxyUser: "proxy",
			expectedAllowed:   true,
			expectedString:    "sys as proxy",
		},
		{
			name:              "proxy user same as login",
			input:             "sys as sys",
			expectedLoginName: "sys",
			expectedProxyUser: "",
			expectedAllowed:   false,
			expectedString:    "sys",
		},
		{
			name:              "non-sys login with proxy format",
			input:             "Proxy as other",
			expectedLoginName: "Proxy",
			expectedProxyUser: "other",
			expectedAllowed:   false,
			expectedString:    "proxy as other",
		},
		{
			name:              "invalid format",
			input:             "PROXY other",
			expectedLoginName: "PROXY other",
			expectedProxyUser: "",
			expectedAllowed:   false,
			expectedString:    "proxy other",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			un := &UserName{}
			allowed := un.Parse(tt.input)
			require.Equal(t, tt.expectedLoginName, un.Login)
			require.Equal(t, tt.expectedProxyUser, un.Proxy)
			require.Equal(t, tt.expectedAllowed, allowed)
			require.Equal(t, tt.expectedString, un.String())
		})
	}
}
