package statusbar_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tkilb/lazyprint/internal/ui/statusbar"
)

func TestStatusBar_View(t *testing.T) {
	sb := statusbar.New()
	sb.Width = 100

	viewPrinters := sb.View(statusbar.PanelPrinters)
	assert.Contains(t, viewPrinters, "default")
	assert.Contains(t, viewPrinters, "quit")

	viewJobs := sb.View(statusbar.PanelJobs)
	assert.Contains(t, viewJobs, "cancel")

	viewScanners := sb.View(statusbar.PanelScanners)
	assert.Contains(t, viewScanners, "scan")

	sb.SetMessage("Refreshed successfully", false)
	viewMsg := sb.View(statusbar.PanelPrinters)
	assert.Contains(t, viewMsg, "Refreshed successfully")
}
