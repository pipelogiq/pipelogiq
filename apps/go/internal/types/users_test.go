package types

import "testing"

func TestUserRoles(t *testing.T) {
	cases := []struct {
		input string
		want  string
		valid bool
	}{
		{"admin", "admin", true},
		{" Administrator ", "admin", true},
		{"SuperAdmin", "admin", true},
		{"operator", "operator", true},
		{" User ", "operator", true},
		{" VIEWER ", "viewer", true},
		{"owner", "owner", false},
		{"", "", false},
		{"RegularUser", "regularuser", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := NormalizeUserRole(tc.input); got != tc.want {
				t.Errorf("NormalizeUserRole = %q, want %q", got, tc.want)
			}
			if got := IsValidUserRole(tc.input); got != tc.valid {
				t.Errorf("IsValidUserRole = %t, want %t", got, tc.valid)
			}
		})
	}
}
