package sane_test

import (
	"image"
	_ "image/png"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tkilb/lazyprint/internal/sane"
)

func TestUserScan(t *testing.T) {
	f, err := os.Open("/home/tylerkilburn/Scans/scan_20261111_144032.png")
	if err != nil {
		t.Skip("no scan")
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	crop := sane.CalculateCropBounds(img)
	padded := sane.ApplyCropMargin(b, crop, 300)

	t.Logf("User scan crop: %v (w=%d, h=%d)", crop, crop.Dx(), crop.Dy())
	t.Logf("User scan padded: %v (w=%d, h=%d)", padded, padded.Dx(), padded.Dy())
	t.Logf("Bottom whitespace trimmed: %d px", b.Dy()-padded.Max.Y)

	// Must include all handwriting (which ends at Y=3248, X=2460)
	assert.True(t, padded.Max.Y >= 3248, "should include handwriting up to 3248, got %d", padded.Max.Y)
	assert.True(t, padded.Max.X >= 2460, "should include handwriting up to 2460, got %d", padded.Max.X)
	// Must trim the whitespace (bottom whitespace was 211px)
	assert.True(t, padded.Max.Y <= 3300, "should trim whitespace at bottom, got %d", padded.Max.Y)
}
