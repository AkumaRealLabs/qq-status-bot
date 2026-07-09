package domain

import "strings"

// GroupsForPrice 返回价格区间包含 price 的托管调度分组。
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

// ManagedGroups 是档位中定义的非空分组集合。
func ManagedGroups(tiers []SchedulerTier) map[string]bool {
	out := map[string]bool{}
	for _, tier := range tiers {
		if group := strings.TrimSpace(tier.Group); group != "" {
			out[group] = true
		}
	}
	return out
}

// SplitGroups 解析逗号分隔的分组列表，去空白并去重。
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

// SameGroups 判断两个分组列表是否同名集合（忽略顺序）。
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

// TargetGroups 保留当前成员中的非托管分组，并加入价格匹配的托管分组。
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

// AssignedTargetGroups 计算应写入调度器的分组列表。
// unassigned 视为「停靠位」：会从当前分组中剥离，且当价格未命中任何托管档位、也无其它手动分组时落到 unassigned。
// 调用方须保证 unassigned 已通过 ValidateSchedulerUnassignedGroup。
func AssignedTargetGroups(tiers []SchedulerTier, managed map[string]bool, price float64, current, unassigned string) []string {
	unassigned = strings.TrimSpace(unassigned)
	if managed == nil {
		managed = ManagedGroups(tiers)
	}
	// 复制 managed，避免修改调用方 map；把 unassigned 当托管剥离，防止粘在渠道上。
	strip := make(map[string]bool, len(managed)+1)
	for k, v := range managed {
		strip[k] = v
	}
	if unassigned != "" {
		strip[unassigned] = true
	}
	out := TargetGroups(tiers, strip, price, current)
	if len(out) == 0 && unassigned != "" {
		return []string{unassigned}
	}
	return out
}

// JoinGroups 用逗号拼接分组名（保持输入顺序）。
func JoinGroups(groups []string) string {
	return strings.Join(groups, ",")
}
