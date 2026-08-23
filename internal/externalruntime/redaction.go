package externalruntime

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

var publicSecretFieldPattern = regexp.MustCompile(`(?i)"(?:[a-z0-9]+[_-])*(?:authorization|bearer|token|credential|secret|password|url|ip|port|endpoint)"\s*:`)

// ScanPublicOutput is the release-blocking redaction oracle for serialized
// public DTOs and structured-log payloads. It is intentionally byte-oriented:
// the caller supplies the exact bytes that would cross the public boundary.
func ScanPublicOutput(serialized []byte, forbiddenValues ...string) error {
	if publicSecretFieldPattern.Match(serialized) {
		return fmt.Errorf("serialized public output contains a credential or operational secret field")
	}
	for _, value := range forbiddenValues {
		if value != "" && bytes.Contains(serialized, []byte(value)) {
			return fmt.Errorf("serialized public output contains a forbidden fixture value")
		}
	}
	return nil
}

// ScanStructuredLog applies the same oracle to JSON logs while also rejecting
// raw authorization headers. Logging is expected to use request IDs and
// stable error codes, never request headers or endpoint mappings.
func ScanStructuredLog(serialized []byte, forbiddenValues ...string) error {
	if strings.Contains(strings.ToLower(string(serialized)), "authorization: bearer") {
		return fmt.Errorf("structured log contains an authorization header")
	}
	return ScanPublicOutput(serialized, forbiddenValues...)
}
