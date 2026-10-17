package config

import "strings"

// Echelle de routage : noms des tiers, rang et options proposees par l'interface.

// Tiers de routage, du plus ecconomique au plus puissant. Une configuration
// sans tier reste utilisable : elle sert de point de depart mais ne figure pas
// dans l'echelle de bascule.
const (
	TierFast      = "fast"
	TierBalanced  = "balanced"
	TierFrontier  = "frontier"
	RoutingTiers  = 3
	tierUnordered = -1
)

// tierRanks fixe l'ordre de l'echelle. Il est volontairement code en dur : le
// rang d'un tier est une decision de produit, pas une donnee de configuration.
var tierRanks = map[string]int{
	TierFast:     0,
	TierBalanced: 1,
	TierFrontier: 2,
}

// TierRank renvoie le rang d'un tier dans l'echelle de routage. Un tier inconnu
// ou absent renvoie tierUnordered : la configuration est alors ignoree par le
// routeur plutot que de le placer arbitrairement en bout de chaine.
func (c *ChatConfig) TierRank() int {
	if c == nil {
		return tierUnordered
	}
	return TierRankOf(c.Tier)
}

// TierRankOf expose le rang d'un tier a partir de son seul nom.
func TierRankOf(tier string) int {
	if rank, ok := tierRanks[strings.ToLower(strings.TrimSpace(tier))]; ok {
		return rank
	}
	return tierUnordered
}

// TierOptions liste les tiers proposes par l'interface, dans l'ordre de l'echelle.
func TierOptions() []string {
	return []string{TierFast, TierBalanced, TierFrontier}
}
