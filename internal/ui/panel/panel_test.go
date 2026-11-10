package panel_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tkilb/lazyprint/internal/ui/panel"
)

func TestInnerSize(t *testing.T) {
	w, h := panel.InnerSize(40, 20)
	assert.Equal(t, 38, w)
	assert.Equal(t, 18, h)

	// Clamp to minWidth / minHeight
	wMin, hMin := panel.InnerSize(4, 2)
	assert.Equal(t, 10, wMin)
	assert.Equal(t, 1, hMin)
}

func TestScrollWindow(t *testing.T) {
	tests := []struct {
		name        string
		cursor      int
		total       int
		visible     int
		expectStart int
		expectEnd   int
	}{
		{"fits entirely", 0, 5, 10, 0, 5},
		{"cursor at top", 0, 20, 5, 0, 5},
		{"cursor within window", 3, 20, 5, 0, 5},
		{"cursor scrolls window", 7, 20, 5, 3, 8},
		{"cursor at bottom", 19, 20, 5, 15, 20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, e := panel.ScrollWindow(tc.cursor, tc.total, tc.visible)
			assert.Equal(t, tc.expectStart, s)
			assert.Equal(t, tc.expectEnd, e)
		})
	}
}

func TestFrame_Rendering(t *testing.T) {
	rendered := panel.Frame("[1] Printers", "Printer Body", 30, 4, true, "1/3")
	lines := strings.Split(strings.TrimSpace(rendered), "\n")
	assert.Equal(t, 6, len(lines)) // top + 4 body lines + bottom

	// Verify embedded top border title with [1] Printers convention
	assert.Contains(t, lines[0], "[1] Printers")
	assert.Contains(t, lines[0], "╭")
	assert.Contains(t, lines[0], "╮")

	// Verify bottom border with footer
	assert.Contains(t, lines[5], "1/3")
	assert.Contains(t, lines[5], "╰")
	assert.Contains(t, lines[5], "╯")
}
