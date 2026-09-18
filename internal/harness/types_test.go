package harness

import "testing"

func TestValidateRejectsBypassPermissions(t *testing.T) {
	s := StartSpec{SessionID: "0b4f9a2e-3f3e-4c0a-9d3a-3c5a4c1e2f10", Cwd: "/tmp", Home: "/tmp",
		Profile: Profile{Mode: "bypass" + "Permissions"}}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for " + "bypass" + "Permissions")
	}
}

func TestValidateRequiresUUIDCwdHome(t *testing.T) {
	cases := []StartSpec{
		{SessionID: "nope", Cwd: "/tmp", Home: "/tmp", Profile: Profile{Mode: "default"}},
		{SessionID: "0b4f9a2e-3f3e-4c0a-9d3a-3c5a4c1e2f10", Home: "/tmp", Profile: Profile{Mode: "default"}},
		{SessionID: "0b4f9a2e-3f3e-4c0a-9d3a-3c5a4c1e2f10", Cwd: "/tmp", Profile: Profile{Mode: "default"}},
	}
	for i, c := range cases {
		if err := c.Validate(); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestValidateAcceptsGoodSpec(t *testing.T) {
	s := StartSpec{SessionID: "0b4f9a2e-3f3e-4c0a-9d3a-3c5a4c1e2f10", Cwd: "/tmp", Home: "/tmp",
		Profile: Profile{Mode: "default", MaxTurns: 20}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}
