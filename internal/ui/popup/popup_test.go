package popup_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tkilb/lazyprint/internal/ui/popup"
)

func TestModel_QueueOperations(t *testing.T) {
	m := popup.Model{}
	assert.False(t, m.Active())

	m = m.Push(popup.Message{Severity: popup.Warning, Text: "Test warning"})
	assert.True(t, m.Active())

	msg, ok := m.Current()
	assert.True(t, ok)
	assert.Equal(t, "Test warning", msg.Text)
	assert.Equal(t, popup.Warning, msg.Severity)

	m = m.Dismiss()
	assert.False(t, m.Active())
}

func TestBoxes(t *testing.T) {
	confirm := popup.ConfirmBox("Delete queue?", 80)
	assert.Contains(t, confirm, "Confirm")
	assert.Contains(t, confirm, "Delete queue?")
	assert.Contains(t, confirm, "(y/enter) confirm")

	danger := popup.DangerConfirmBox("Purge all jobs?", 80)
	assert.Contains(t, danger, "Confirm")
	assert.Contains(t, danger, "Purge all jobs?")

	prompt := popup.PromptBox("Rename Printer", "Enter new name:", "> new_name", 80)
	assert.Contains(t, prompt, "Rename Printer")
	assert.Contains(t, prompt, "Enter new name:")
}

func TestOverlay(t *testing.T) {
	bg := strings.Repeat("xxxxxxxxxxxxxxxxxxxx\n", 10)
	box := popup.ConfirmBox("Are you sure?", 40)
	overlaid := popup.Overlay(bg, box, 60, 15)

	assert.Contains(t, overlaid, "Are you sure?")
	assert.Contains(t, overlaid, "xxxx")
}

func TestConfirmChoiceAt(t *testing.T) {
	confirm, ok := popup.ConfirmChoiceAt(2) // beginning of "(y/enter) confirm"
	assert.True(t, ok)
	assert.True(t, confirm)

	confirm, ok = popup.ConfirmChoiceAt(25) // inside "(n/esc) cancel"
	assert.True(t, ok)
	assert.False(t, confirm)
}
