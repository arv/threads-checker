package diagnostics

import (
	"testing"

	"github.com/microsoft/typescript-go/internal/locale"
	"golang.org/x/text/language"
	"gotest.tools/v3/assert"
)

func TestOwnershipMessagesLocalizeByKey(t *testing.T) {
	t.Parallel()
	m := X_0_cannot_be_used_here_Colon_it_was_sent_to_1_on_line_2
	assert.Equal(t, keyToMessage(m.Key()), (*Message)(nil))
	got := Localize(locale.Locale(language.English), nil, m.Key(), "mine", "spawn", "12")
	assert.Equal(t, got, "'mine' cannot be used here: it was sent to 'spawn' on line 12.")
	assert.Equal(t, m.Code(), int32(96001))
	assert.Equal(t, m.Category(), CategoryError)
}
