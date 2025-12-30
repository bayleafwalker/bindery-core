package semver

import (
	"fmt"

	mm "github.com/Masterminds/semver/v3"
)

// Version is a semantic version.
//
// This is a thin wrapper around github.com/Masterminds/semver/v3.
type Version struct {
	v *mm.Version
}

// Constraint is a semantic version constraint.
//
// Examples:
// - ">=1.2.0 <2.0.0"
// - "^1.0.0"
// - "~1.4"
type Constraint struct {
	c *mm.Constraints
}

func ParseVersion(raw string) (Version, error) {
	v, err := mm.NewVersion(raw)
	if err != nil {
		return Version{}, fmt.Errorf("semver: parse version %q: %w", raw, err)
	}
	return Version{v: v}, nil
}

func MustParseVersion(raw string) Version {
	v, err := ParseVersion(raw)
	if err != nil {
		panic(err)
	}
	return v
}

func ParseConstraint(raw string) (Constraint, error) {
	c, err := mm.NewConstraint(raw)
	if err != nil {
		return Constraint{}, fmt.Errorf("semver: parse constraint %q: %w", raw, err)
	}
	return Constraint{c: c}, nil
}

func MustParseConstraint(raw string) Constraint {
	c, err := ParseConstraint(raw)
	if err != nil {
		panic(err)
	}
	return c
}

func Satisfies(v Version, c Constraint) bool {
	if v.v == nil || c.c == nil {
		return false
	}
	return c.c.Check(v.v)
}

// Compare compares a and b, returning:
// -1 if a < b
//
//	0 if a == b
//	1 if a > b
func Compare(a, b Version) int {
	if a.v == nil && b.v == nil {
		return 0
	}
	if a.v == nil {
		return -1
	}
	if b.v == nil {
		return 1
	}
	return a.v.Compare(b.v)
}

// MaxSatisfying returns the highest version in candidates that satisfies c.
//
// If multiple versions are equal, the first encountered wins.
func MaxSatisfying(c Constraint, candidates []Version) (Version, bool) {
	var best Version
	found := false
	for _, candidate := range candidates {
		if !Satisfies(candidate, c) {
			continue
		}
		if !found || Compare(candidate, best) > 0 {
			best = candidate
			found = true
		}
	}
	return best, found
}
