package common

// AddUnique appends items from 'toAdd' to 'existing' only if they're not already present.
// Returns the updated slice with unique items.
func AddUnique[T comparable](existing []T, toAdd []T) []T {
	// seen map uses struct{} as the value for memory efficiency
	seen := make(map[T]struct{}, len(existing))

	// Populate the set with existing items
	for _, item := range existing {
		seen[item] = struct{}{}
	}

	for _, item := range toAdd {
		// check if item not yet in the set
		if _, found := seen[item]; !found {
			existing = append(existing, item)
			// Add the new unique item to the set
			seen[item] = struct{}{}
		}
	}
	return existing
}
