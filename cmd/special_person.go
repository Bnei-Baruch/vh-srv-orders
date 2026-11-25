package cmd

import (
	"strings"

	"github.com/Bnei-Baruch/vh-srv-orders/repo"
)

// identifiersOf returns the identifiers a special names a person by: keycloak
// id, lower-cased email, either, or neither.
func identifiersOf(special *repo.Special) []string {
	var ids []string
	if keycloakID := strings.TrimSpace(special.KeycloakId.String); keycloakID != "" {
		ids = append(ids, "kc\x00"+keycloakID)
	}
	if email := strings.TrimSpace(strings.ToLower(special.Email.String)); email != "" {
		ids = append(ids, "email\x00"+email)
	}
	return ids
}

// groupByPerson folds specials into one group per person, in first-seen order.
// Two specials are the same person when they share any non-empty identifier;
// older rows disagree about which column is filled, so neither column alone
// works.
func groupByPerson(specials []*repo.Special) [][]*repo.Special {
	byIdentifier := make(map[string]int)
	var groups [][]*repo.Special

	merge := func(dst, src int) {
		groups[dst] = append(groups[dst], groups[src]...)
		groups[src] = nil
		for identifier, group := range byIdentifier {
			if group == src {
				byIdentifier[identifier] = dst
			}
		}
	}

	for _, special := range specials {
		identifiers := identifiersOf(special)
		if len(identifiers) == 0 {
			continue
		}

		group := -1
		for _, identifier := range identifiers {
			existing, ok := byIdentifier[identifier]
			if !ok {
				continue
			}
			switch {
			case group == -1:
				group = existing
			case existing != group:
				merge(group, existing)
			}
		}
		if group == -1 {
			group = len(groups)
			groups = append(groups, nil)
		}

		groups[group] = append(groups[group], special)
		for _, identifier := range identifiers {
			byIdentifier[identifier] = group
		}
	}

	kept := groups[:0]
	for _, group := range groups {
		if len(group) > 0 {
			kept = append(kept, group)
		}
	}
	return kept
}
