package relayv1

// PeekMustUUID is a small validation helper for control-plane code that needs
// to reject non-UUID allocation and participant IDs before entering the hot
// path. It returns the parsed bytes without exposing the parser implementation.
func PeekMustUUID(value string) ([16]byte, error) { return parseUUID(value) }
