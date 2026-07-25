package domain

import (
	"errors"
	"sort"
	"strings"
)

const (
	SchedulerProviderGGAPI   = "ggapi"
	SchedulerProviderAxonHub = "axonhub"

	AxonHubControlOff    = "off"
	AxonHubControlActive = "active"

	AxonHubStatusEnabled  = "enabled"
	AxonHubStatusDisabled = "disabled"
	AxonHubStatusArchived = "archived"

	AxonHubTagLow    = "payg_low"
	AxonHubTagStable = "payg_stable"
)

type AxonHubConfig struct {
	BaseURL          string `json:"base_url"`
	AdminEmail       string `json:"admin_email"`
	AdminPassword    string `json:"admin_password,omitempty"`
	AdminPasswordSet bool   `json:"admin_password_set,omitempty"`
	ControlMode      string `json:"control_mode"`
}

func (c AxonHubConfig) Public() AxonHubConfig {
	out := c
	out.AdminPasswordSet = secretSet(c.AdminPassword)
	out.AdminPassword = ""
	return out
}

func NormalizeSchedulerProvider(provider string) string {
	if strings.EqualFold(strings.TrimSpace(provider), SchedulerProviderAxonHub) {
		return SchedulerProviderAxonHub
	}
	return SchedulerProviderGGAPI
}

func NormalizeAxonHubControlMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case AxonHubControlOff, AxonHubControlActive:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return AxonHubControlOff
	}
}

func DefaultAxonHubTiers() []SchedulerTier {
	return []SchedulerTier{
		{Tag: AxonHubTagLow, Group: AxonHubTagLow, PriceMin: 0, PriceMax: 0.099, SalePrice: 0.10},
		{Tag: AxonHubTagStable, Group: AxonHubTagStable, PriceMin: 0.10, PriceMax: 0.20, SalePrice: 0.25},
	}
}

func ValidateNonOverlappingSchedulerTiers(tiers []SchedulerTier) error {
	ordered := append([]SchedulerTier(nil), tiers...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].PriceMin < ordered[j].PriceMin })
	for i := 1; i < len(ordered); i++ {
		if ordered[i].PriceMin <= ordered[i-1].PriceMax {
			return errors.New("调度档位价格区间不能重叠")
		}
	}
	return nil
}

func AxonHubTierForCost(cost float64) (SchedulerTier, bool) {
	for _, tier := range DefaultAxonHubTiers() {
		if cost >= tier.PriceMin && cost <= tier.PriceMax {
			return tier, true
		}
	}
	return SchedulerTier{}, false
}

func IsAxonHubManagedTag(tag string) bool {
	return tag == AxonHubTagLow || tag == AxonHubTagStable
}

func AxonHubManagedTag(tags []string) string {
	for _, tag := range tags {
		if IsAxonHubManagedTag(strings.TrimSpace(tag)) {
			return strings.TrimSpace(tag)
		}
	}
	return ""
}

// AxonHubTargetTags 保留人工标签，并确保最多存在一个 AUM 托管标签。
func AxonHubTargetTags(current []string, managedTag string) []string {
	out := make([]string, 0, len(current)+1)
	seen := map[string]bool{}
	for _, tag := range current {
		tag = strings.TrimSpace(tag)
		if tag == "" || IsAxonHubManagedTag(tag) || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	if IsAxonHubManagedTag(managedTag) {
		out = append(out, managedTag)
	}
	return out
}

type AxonHubCostTarget struct {
	Cost float64
	Tag  string
}

// AxonHubOrderingWeights 在每个托管池内独立按不同成本档递减 10，同成本共享权重。
func AxonHubOrderingWeights(costs map[string]AxonHubCostTarget) map[string]int {
	byTag := map[string][]float64{}
	for _, item := range costs {
		if IsAxonHubManagedTag(item.Tag) {
			byTag[item.Tag] = append(byTag[item.Tag], item.Cost)
		}
	}
	weightsByTag := map[string]map[float64]int{}
	for tag, values := range byTag {
		sort.Float64s(values)
		weightsByTag[tag] = map[float64]int{}
		rank := 0
		for i, value := range values {
			if i > 0 && value != values[i-1] {
				rank++
			}
			weight := 100 - rank*10
			if weight < 10 {
				weight = 10
			}
			weightsByTag[tag][value] = weight
		}
	}
	out := make(map[string]int, len(costs))
	for id, item := range costs {
		if weights, ok := weightsByTag[item.Tag]; ok {
			out[id] = weights[item.Cost]
		}
	}
	return out
}

type AxonHubPreflightCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type AxonHubPreflight struct {
	OK     bool                    `json:"ok"`
	Bound  int                     `json:"bound"`
	Checks []AxonHubPreflightCheck `json:"checks"`
}
