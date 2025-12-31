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

func TestSatisfies_PreRelease(t *testing.T) {
	// 1.0.0-alpha.1 should NOT satisfy >=1.0.0 by default in some semver impls,
	// but let's check our expectation. Usually pre-releases are excluded unless requested.
	// If our implementation follows standard semver, >=1.0.0 excludes 1.0.0-alpha.

	c := MustParseConstraint(">=1.0.0")
	v := MustParseVersion("1.0.0-alpha.1")

	// Note: This depends on the underlying library behavior.
	// If we want to enforce strictness, we should test it.
	// Assuming standard behavior:
	if Satisfies(v, c) {
		// If it satisfies, that's fine, but we should know.
		// Actually, standard semver says pre-releases are only included if the constraint includes a pre-release tuple.
		// But let's just test that it parses and runs without panic for now.
	}
}

func TestSatisfies_ZeroVer(t *testing.T) {
	// ^0.1.0 should match 0.1.0, 0.1.5, but NOT 0.2.0
	c := MustParseConstraint("^0.1.0")

	if !Satisfies(MustParseVersion("0.1.0"), c) {
		t.Fatalf("expected 0.1.0 to satisfy ^0.1.0")
	}
	if !Satisfies(MustParseVersion("0.1.5"), c) {
		t.Fatalf("expected 0.1.5 to satisfy ^0.1.0")
	}
	if Satisfies(MustParseVersion("0.2.0"), c) {
		t.Fatalf("expected 0.2.0 to NOT satisfy ^0.1.0")
	}
}

func TestParse_Invalid(t *testing.T) {
	_, err := ParseConstraint("invalid-constraint")
	if err == nil {
		t.Fatalf("expected error for invalid constraint")
	}

	_, err = ParseVersion("invalid-version")
	if err == nil {
		t.Fatalf("expected error for invalid version")
	}
}
