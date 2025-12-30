package semver

import "testing"

func TestSatisfies(t *testing.T) {
	c := MustParseConstraint("^1.2.0")

	if !Satisfies(MustParseVersion("1.2.0"), c) {
		t.Fatalf("expected 1.2.0 to satisfy ^1.2.0")
	}
	if !Satisfies(MustParseVersion("1.9.9"), c) {
		t.Fatalf("expected 1.9.9 to satisfy ^1.2.0")
	}
	if Satisfies(MustParseVersion("2.0.0"), c) {
		t.Fatalf("expected 2.0.0 to NOT satisfy ^1.2.0")
	}
}

func TestMaxSatisfying(t *testing.T) {
	c := MustParseConstraint(">=1.0.0 <2.0.0")
	candidates := []Version{
		MustParseVersion("0.9.0"),
		MustParseVersion("1.0.0"),
		MustParseVersion("1.5.0"),
		MustParseVersion("2.0.0"),
	}

	best, ok := MaxSatisfying(c, candidates)
	if !ok {
		t.Fatalf("expected to find a satisfying version")
	}
	if Compare(best, MustParseVersion("1.5.0")) != 0 {
		t.Fatalf("expected best=1.5.0")
	}
}
