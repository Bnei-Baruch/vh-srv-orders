package importers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volatiletech/null/v9"
)

func TestCreateSpecial_BlankEmailNamesTheCause(t *testing.T) {
	im := NewSpecialsImporter()
	for _, email := range []null.String{null.String{}, null.StringFrom(""), null.StringFrom("  ")} {
		err := im.createSpecial(&SpecialRecord{Email: email, KeycloakID: null.StringFrom("kc-1")})
		assert.ErrorContains(t, err, "email is required", "email %q", email.String)
	}
}
