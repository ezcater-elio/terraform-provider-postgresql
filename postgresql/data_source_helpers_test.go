package postgresql

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
)

func TestGeneratePatternArrayStringQuotesLiterals(t *testing.T) {
	got := generatePatternArrayString([]any{"it's%", "plain"}, queryArrayKeywordAny)
	assert.Equal(t, `ANY (array['it''s%','plain'])`, got)
}

// Backslashes switch to an E-prefixed literal; the matched string is unchanged.
func TestGeneratePatternArrayStringBackslash(t *testing.T) {
	got := generatePatternArrayString([]any{`a\b`}, queryArrayKeywordAll)
	assert.Equal(t, `ALL (array[ E'a\\b'])`, got)
}

func TestApplyPatternMatchingToQueryQuotesRegex(t *testing.T) {
	d := schema.TestResourceDataRaw(t, dataSourcePostgreSQLDatabaseSchemas().Schema, map[string]any{
		"database":      "postgres",
		"regex_pattern": "^o'k$",
	})
	assert.Equal(t,
		[]string{`schema_name ~ '^o''k$'`},
		applyPatternMatchingToQuery(schemaPatternMatchingTarget, d),
	)
}
