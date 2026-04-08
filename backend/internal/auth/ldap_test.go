package auth

import (
	"reflect"
	"testing"
)

func TestReplaceUsernameToken(t *testing.T) {
	filter := "(&(objectClass=person)(uid={username})(mail={username}))"
	got := replaceUsernameToken(filter, "alice")
	want := "(&(objectClass=person)(uid=alice)(mail=alice))"
	if got != want {
		t.Fatalf("unexpected filter, got=%q want=%q", got, want)
	}
}

func TestResolveRolesMergesAndDeduplicates(t *testing.T) {
	p := &LDAPProvider{
		roles: []RoleConfig{
			{Name: "viewer", Users: []string{"alice"}},
			{Name: "admin", Users: []string{"alice"}},
		},
		groupRoleMapping: map[string][]string{
			"cn=sre,ou=groups,dc=example,dc=com": []string{"admin", "ops"},
			"cn=dev,ou=groups,dc=example,dc=com": []string{"dev"},
		},
	}

	got := p.resolveRoles("alice", []string{
		"CN=SRE,OU=groups,DC=example,DC=com",
		"cn=dev,ou=groups,dc=example,dc=com",
	})
	want := []string{"viewer", "admin", "ops", "dev"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected roles, got=%v want=%v", got, want)
	}
}
