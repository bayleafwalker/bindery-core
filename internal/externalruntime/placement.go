package externalruntime

// resolvePlacement is intentionally a coordinator-frozen seam. The worker
// packet may only fill in the mechanical allocator result validation and
// publication beneath the tests; it may not redesign this boundary.
func resolvePlacement(_ PlacementAllocator, _ PlacementIntent) (*PublicPlacement, error) {
	return nil, nil
}
