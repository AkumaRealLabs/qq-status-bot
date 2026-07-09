package domain

import "strings"

// GroupsForPrice returns managed scheduler groups whose price range includes price.
func GroupsForPrice(tiers []SchedulerTier, price float64) []string {
	groups := []string{}
	seen := map[string]bool{}
	for _, tier := range tiers {
		group := strings.TrimSpace(tier.Group)
		if group == "" || price < tier.PriceMin || price > tier.PriceMax || seen[group] {
			continue
		}
		seen[group] = true
		groups = append(groups, group)
	}
	return groups
}

// ManagedGroups is the set of non-empty groups defined in tiers.
func ManagedGroups(tiers []SchedulerTier) map[string]bool {
	out := map[string]bool{}
	for _, tier := range tiers {
		if group := strings.TrimSpace(tier.Group); group != "" {
			out[group] = true
		}
	}
	return out
}

// SplitGroups parses a comma-separated group list, trimming and de-duplicating.
func SplitGroups(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		group := strings.TrimSpace(item)
		if group != "" && !seen[group] {
			seen[group] = true
			out = append(out, group)
		}
	}
	return out
}

// SameGroups reports whether two group lists contain the same set of names (order ignored).
func SameGroups(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, group := range a {
		seen[group] = true
	}
	for _, group := range b {
		if !seen[group] {
			return false
		}
	}
	return true
}

// TargetGroups keeps unmanaged groups from current membership and adds price-matched managed groups.
func TargetGroups(tiers []SchedulerTier, managed map[string]bool, price float64, current string) []string {
	if managed == nil {
		managed = ManagedGroups(tiers)
	}
	out := []string{}
	seen := map[string]bool{}
	for _, group := range SplitGroups(current) {
		if !managed[group] {
			seen[group] = true
			out = append(out, group)
		}
	}
	for _, group := range GroupsForPrice(tiers, price) {
		if !seen[group] {
			seen[group] = true
			out = append(out, group)
		}
	}
	return out
}

// JoinGroups joins group names with commas (stable order of input).
func JoinGroups(groups []string) string {
	return strings.Join(groups, ",")
}
