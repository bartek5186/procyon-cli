package modulegen

import "testing"

func TestPolicyRoleReferences(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantUser  string
		wantAdmin string
	}{
		{name: "application roles", source: `{Role: RoleUser}`, wantUser: "RoleUser", wantAdmin: "RoleAdmin"},
		{name: "aliased core roles", source: `{Role: coreauthz.RoleUser}`, wantUser: "coreauthz.RoleUser", wantAdmin: "coreauthz.RoleAdmin"},
		{name: "legacy root policies", source: `{Role: authz.RoleUser}`, wantUser: "authz.RoleUser", wantAdmin: "authz.RoleAdmin"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user, admin := policyRoleReferences(test.source)
			if user != test.wantUser || admin != test.wantAdmin {
				t.Fatalf("policyRoleReferences() = %q, %q; want %q, %q", user, admin, test.wantUser, test.wantAdmin)
			}
		})
	}
}
