package cmd

import (
	"strings"

	"gitlab.bbdev.team/vh/pay/orders/repo"
)

// identifiersOf returns the identifiers a special names a person by. Either
// column may be absent, so a row can carry one, both, or — for a row that names
// nobody — neither.
//
// The email is lower-cased because that is how the rest of the system compares
// it: GetAllSpecialsByEmail matches with `ilike`.
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

// groupByPerson folds specials into one group per person, preserving the order
// in which people were first seen.
//
// Neither column identifies a person on its own. Keying on the email alone
// merges every keycloak-only row, since those carry no email at all. Keying on
// the keycloak id first splits one person across two groups whenever two of
// their rows disagree about whether the id is populated — which is ordinary,
// because the importer writes the row before the account exists and
// handleCreateSpecial requires no id either.
//
// So two specials are the same person when they share *any* non-empty
// identifier, and a row that joins two groups merges them.
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

	// merge empties the groups it absorbs rather than reindexing every entry.
	kept := groups[:0]
	for _, group := range groups {
		if len(group) > 0 {
			kept = append(kept, group)
		}
	}
	return kept
}
