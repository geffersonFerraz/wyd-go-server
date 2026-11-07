package auth

import "testing"

func TestHashVerify(t *testing.T) {
	h, err := Hash("110389")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := Verify(h, "110389"); !ok || err != nil {
		t.Fatalf("correct secret rejected: %v", err)
	}
	if ok, _ := Verify(h, "110388"); ok {
		t.Fatal("wrong secret accepted")
	}
	h2, _ := Hash("110389")
	if h == h2 {
		t.Fatal("salt not random")
	}
	if _, err := Verify("plain", "x"); err == nil {
		t.Fatal("malformed hash accepted")
	}
}

func TestSplitLegacyPassword(t *testing.T) {
	cases := map[string]struct {
		s  AccountStatus
		pw string
	}{
		"abc":  {StatusNormal, "abc"},
		"@abc": {StatusBlocked, "abc"},
		"_abc": {StatusDefective, "abc"},
		"#abc": {StatusDisabled, "abc"},
		"":     {StatusNormal, ""},
	}
	for in, want := range cases {
		s, pw := SplitLegacyPassword(in)
		if s != want.s || pw != want.pw {
			t.Errorf("%q -> %v %q", in, s, pw)
		}
	}
}
