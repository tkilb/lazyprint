package sane_test

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tkilb/lazyprint/internal/runner"
	"github.com/tkilb/lazyprint/internal/sane"
)

func TestCalculateCropBounds(t *testing.T) {
	t.Run("dark card at top-left origin", func(t *testing.T) {
		img := image.NewRGBA(image.Rect(0, 0, 500, 500))
		draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

		card := image.Rect(0, 0, 200, 300)
		draw.Draw(img, card, &image.Uniform{C: color.Black}, image.Point{}, draw.Src)

		crop := sane.CalculateCropBounds(img)
		assert.Equal(t, 0, crop.Min.X)
		assert.Equal(t, 0, crop.Min.Y)
		assert.True(t, crop.Dx() >= 196 && crop.Dx() <= 202, "unexpected width: %d", crop.Dx())
		assert.True(t, crop.Dy() >= 296 && crop.Dy() <= 302, "unexpected height: %d", crop.Dy())
	})

	t.Run("white greeting card back on off-white lid with shadow line, bezel, and dust", func(t *testing.T) {
		// Simulate flatbed glass (1000x1300) with slightly warm/gray lid (lum ~213, < 225)
		bed := image.NewRGBA(image.Rect(0, 0, 1000, 1300))
		lidColor := color.RGBA{R: 215, G: 213, B: 208, A: 255}
		draw.Draw(bed, bed.Bounds(), &image.Uniform{C: lidColor}, image.Point{}, draw.Src)

		// Outer glass bezel reflections along edges
		bezelColor := color.RGBA{R: 60, G: 60, B: 60, A: 255}
		draw.Draw(bed, image.Rect(0, 0, 1000, 8), &image.Uniform{C: bezelColor}, image.Point{}, draw.Src)
		draw.Draw(bed, image.Rect(0, 0, 8, 1300), &image.Uniform{C: bezelColor}, image.Point{}, draw.Src)
		draw.Draw(bed, image.Rect(990, 0, 1000, 1300), &image.Uniform{C: bezelColor}, image.Point{}, draw.Src)
		draw.Draw(bed, image.Rect(0, 1290, 1000, 1300), &image.Uniform{C: bezelColor}, image.Point{}, draw.Src)

		// White greeting card placed at top-left origin: 0,0 to 500,700
		cardPaper := color.RGBA{R: 220, G: 219, B: 216, A: 255}
		draw.Draw(bed, image.Rect(0, 0, 500, 700), &image.Uniform{C: cardPaper}, image.Point{}, draw.Src)

		// Small Hallmark barcode / licensing text near bottom-center of the card
		draw.Draw(bed, image.Rect(200, 560, 300, 630), &image.Uniform{C: color.RGBA{R: 40, G: 40, B: 40, A: 255}}, image.Point{}, draw.Src)

		// Physical paper cut shadow line along bottom `________|` (y=700, x=0..500) and right `|` (x=500, y=0..700)
		shadowLine := color.RGBA{R: 135, G: 135, B: 135, A: 255}
		draw.Draw(bed, image.Rect(0, 698, 500, 701), &image.Uniform{C: shadowLine}, image.Point{}, draw.Src)
		draw.Draw(bed, image.Rect(499, 0, 501, 701), &image.Uniform{C: shadowLine}, image.Point{}, draw.Src)

		// Random dust specs on empty scanner bed
		draw.Draw(bed, image.Rect(750, 900, 753, 903), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)
		draw.Draw(bed, image.Rect(820, 1100, 822, 1102), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)

		crop := sane.CalculateCropBounds(bed)
		assert.Equal(t, 0, crop.Min.X)
		assert.Equal(t, 0, crop.Min.Y)
		assert.True(t, crop.Dx() >= 496 && crop.Dx() <= 502, "expected flush width ~500, got %d", crop.Dx())
		assert.True(t, crop.Dy() >= 696 && crop.Dy() <= 702, "expected flush height ~700, got %d", crop.Dy())
	})

	t.Run("floating rectangle away from origin", func(t *testing.T) {
		bed := image.NewRGBA(image.Rect(0, 0, 600, 600))
		draw.Draw(bed, bed.Bounds(), &image.Uniform{C: color.RGBA{R: 230, G: 230, B: 230, A: 255}}, image.Point{}, draw.Src)

		card := image.Rect(120, 140, 360, 420)
		draw.Draw(bed, card, &image.Uniform{C: color.RGBA{R: 30, G: 80, B: 160, A: 255}}, image.Point{}, draw.Src)

		crop := sane.CalculateCropBounds(bed)
		assert.True(t, crop.Min.X >= 118 && crop.Min.X <= 124, "unexpected Min.X: %d", crop.Min.X)
		assert.True(t, crop.Min.Y >= 138 && crop.Min.Y <= 144, "unexpected Min.Y: %d", crop.Min.Y)
		assert.True(t, crop.Max.X >= 356 && crop.Max.X <= 362, "unexpected Max.X: %d", crop.Max.X)
		assert.True(t, crop.Max.Y >= 416 && crop.Max.Y <= 422, "unexpected Max.Y: %d", crop.Max.Y)
	})

	t.Run("card with wide right-edge penumbra ramp and slight skew", func(t *testing.T) {
		bed := image.NewRGBA(image.Rect(0, 0, 1000, 1200))
		draw.Draw(bed, bed.Bounds(), &image.Uniform{C: color.RGBA{R: 251, G: 251, B: 251, A: 255}}, image.Point{}, draw.Src)

		// Card body from x=0..500 (top half) and x=0..506 (bottom half), y=0..800
		cardColor := color.RGBA{R: 185, G: 180, B: 160, A: 255}
		draw.Draw(bed, image.Rect(0, 0, 500, 400), &image.Uniform{C: cardColor}, image.Point{}, draw.Src)
		draw.Draw(bed, image.Rect(0, 400, 506, 800), &image.Uniform{C: cardColor}, image.Point{}, draw.Src)

		// 24px soft penumbra ramp outside the right edge (fading from 205 up to 250)
		for dx := 0; dx < 24; dx++ {
			lum := uint8(205 + dx*2)
			penColor := color.RGBA{R: lum, G: lum, B: lum, A: 255}
			draw.Draw(bed, image.Rect(500+dx, 0, 501+dx, 400), &image.Uniform{C: penColor}, image.Point{}, draw.Src)
			draw.Draw(bed, image.Rect(506+dx, 400, 507+dx, 800), &image.Uniform{C: penColor}, image.Point{}, draw.Src)
		}

		crop := sane.CalculateCropBounds(bed)
		assert.Equal(t, 0, crop.Min.X)
		assert.Equal(t, 0, crop.Min.Y)
		assert.True(t, crop.Dx() >= 500 && crop.Dx() <= 516, "expected card body preserved and penumbra trimmed, got %d", crop.Dx())
		assert.True(t, crop.Dy() >= 795 && crop.Dy() <= 804, "expected height ~800, got %d", crop.Dy())
	})

	t.Run("full page document spans bed and trims outer whitespace", func(t *testing.T) {
		bed := image.NewRGBA(image.Rect(0, 0, 1000, 1300))
		draw.Draw(bed, bed.Bounds(), &image.Uniform{C: color.RGBA{R: 240, G: 240, B: 240, A: 255}}, image.Point{}, draw.Src)

		// Document covering 92% of bed
		doc := image.Rect(0, 0, 920, 1200)
		draw.Draw(bed, doc, &image.Uniform{C: color.White}, image.Point{}, draw.Src)

		crop := sane.CalculateCropBounds(bed)
		assert.True(t, crop.Dx() >= 918 && crop.Dx() <= 924)
		assert.True(t, crop.Dy() >= 1198 && crop.Dy() <= 1204)
	})

	t.Run("handwritten letter with sparse edge words and blank paper margin on scanner base", func(t *testing.T) {
		// Simulate flatbed 1000x1300 with gray/off-white scanner lid (lum ~218)
		bed := image.NewRGBA(image.Rect(0, 0, 1000, 1300))
		lidColor := color.RGBA{R: 220, G: 218, B: 214, A: 255}
		draw.Draw(bed, bed.Bounds(), &image.Uniform{C: lidColor}, image.Point{}, draw.Src)

		// White letter sheet placed at (0,0) to (820, 1080)
		paperColor := color.RGBA{R: 250, G: 249, B: 247, A: 255}
		draw.Draw(bed, image.Rect(0, 0, 820, 1080), &image.Uniform{C: paperColor}, image.Point{}, draw.Src)

		ink := color.RGBA{R: 45, G: 45, B: 50, A: 255}
		// Salutation near top-left ("Hi" / "Happy Birthday") starting at y=95 (past 8% top strip)
		draw.Draw(bed, image.Rect(40, 95, 65, 130), &image.Uniform{C: ink}, image.Point{}, draw.Src)
		draw.Draw(bed, image.Rect(40, 145, 68, 280), &image.Uniform{C: ink}, image.Point{}, draw.Src)

		// Vertical lines of cursive writing with word spaces across the letter body
		for colX := 110; colX <= 760; colX += 55 {
			for wordY := 90; wordY < 980; wordY += 75 {
				// Each word is a short 35px stroke separated by a 40px gap (fails old 4% straight-run filter)
				draw.Draw(bed, image.Rect(colX, wordY, colX+4, wordY+35), &image.Uniform{C: ink}, image.Point{}, draw.Src)
			}
		}

		// Rightmost line of handwriting at x=775..785 (near right edge of 820px paper)
		for wordY := 120; wordY < 1010; wordY += 80 {
			draw.Draw(bed, image.Rect(775, wordY, 780, wordY+32), &image.Uniform{C: ink}, image.Point{}, draw.Src)
		}

		crop := sane.CalculateCropBounds(bed)
		// Top-left origin must stay anchored at (0,0) — never slicing off top salutation at y=95
		assert.Equal(t, 0, crop.Min.X)
		assert.Equal(t, 0, crop.Min.Y)
		// Must preserve full paper width (~820) past the rightmost handwriting at x=780, while cropping scanner base (820..1000)
		assert.True(t, crop.Max.X >= 815 && crop.Max.X <= 826, "expected full letter width ~820 preserved, got %d", crop.Max.X)
		assert.True(t, crop.Max.Y >= 1075 && crop.Max.Y <= 1086, "expected full letter height ~1080 preserved, got %d", crop.Max.Y)
	})

	t.Run("US letter covering 94% of A4 bed with 1px ballpoint handwriting near edges", func(t *testing.T) {
		// US Letter (8.5x11") on an A4 flatbed covers 100% width and 94% height (y=0..1222 of 1300)
		bed := image.NewRGBA(image.Rect(0, 0, 1000, 1300))
		lidColor := color.RGBA{R: 226, G: 224, B: 220, A: 255}
		draw.Draw(bed, bed.Bounds(), &image.Uniform{C: lidColor}, image.Point{}, draw.Src)

		paperColor := color.RGBA{R: 251, G: 250, B: 248, A: 255}
		draw.Draw(bed, image.Rect(0, 0, 985, 1220), &image.Uniform{C: paperColor}, image.Point{}, draw.Src)

		// 1-pixel ballpoint pen strokes near top (y=35) and right (x=955) margins
		ink := color.RGBA{R: 50, G: 55, B: 70, A: 255}
		for x := 60; x < 955; x += 40 {
			bed.Set(x, 35, ink)
			bed.Set(x, 36, ink)
		}
		for y := 40; y < 1160; y += 45 {
			bed.Set(955, y, ink)
			bed.Set(956, y, ink)
		}

		for _, agg := range []sane.CropAggression{sane.Crop10Pct, sane.Crop20Pct, sane.Crop30Pct} {
			crop := sane.CalculateCropBoundsWithAggression(bed, agg)
			final := sane.ApplyCropMarginWithAggression(bed.Bounds(), crop, 300, agg)
			assert.Equal(t, 0, final.Min.X, "aggression %s clipped left edge", agg)
			assert.Equal(t, 0, final.Min.Y, "aggression %s clipped top edge", agg)
			assert.True(t, final.Max.X >= 980, "aggression %s clipped right paper edge (got %d)", agg, final.Max.X)
			assert.True(t, final.Max.Y >= 1215 && final.Max.Y <= 1285, "aggression %s unexpected bottom edge %d", agg, final.Max.Y)
		}
	})
}

func TestCropAggressionPresets(t *testing.T) {
	bounds := image.Rect(0, 0, 1000, 1000)
	crop := image.Rect(100, 100, 600, 700)

	off := sane.ApplyCropMarginWithAggression(bounds, crop, 300, sane.CropOff)
	cons := sane.ApplyCropMarginWithAggression(bounds, crop, 300, sane.CropConservative)
	bal := sane.ApplyCropMarginWithAggression(bounds, crop, 300, sane.CropBalanced)
	agg := sane.ApplyCropMarginWithAggression(bounds, crop, 300, sane.CropAggressive)

	assert.Equal(t, bounds, off)
	// Conservative leaves the widest margin; Aggressive leaves the tightest margin.
	assert.True(t, cons.Dx() > bal.Dx(), "conservative width (%d) should exceed balanced (%d)", cons.Dx(), bal.Dx())
	assert.True(t, bal.Dx() > agg.Dx(), "balanced width (%d) should exceed aggressive (%d)", bal.Dx(), agg.Dx())
	assert.Equal(t, sane.CropOff, sane.ParseCropAggression("off"))
	assert.Equal(t, sane.Crop20Pct, sane.ParseCropAggression("low"))
	assert.Equal(t, sane.Crop30Pct, sane.ParseCropAggression("medium"))
	assert.Equal(t, sane.Crop80Pct, sane.ParseCropAggression("high"))
	assert.Equal(t, sane.Crop50Pct, sane.ParseCropAggression("50%"))
	assert.Equal(t, sane.Crop100Pct, sane.ParseCropAggression("100%"))
}

func TestApplyCropMargin(t *testing.T) {
	bounds := image.Rect(0, 0, 1000, 1000)

	t.Run("origin anchored adds margin on max edges", func(t *testing.T) {
		crop := image.Rect(0, 0, 400, 500)
		padded := sane.ApplyCropMargin(bounds, crop, 300)
		assert.Equal(t, 0, padded.Min.X)
		assert.Equal(t, 0, padded.Min.Y)
		assert.True(t, padded.Max.X > 400)
		assert.True(t, padded.Max.Y > 500)
	})

	t.Run("floating item adds margin on all edges", func(t *testing.T) {
		crop := image.Rect(200, 200, 600, 600)
		padded := sane.ApplyCropMargin(bounds, crop, 300)
		assert.True(t, padded.Min.X < 200)
		assert.True(t, padded.Min.Y < 200)
		assert.True(t, padded.Max.X > 600)
		assert.True(t, padded.Max.Y > 600)
	})

	t.Run("already full bounds remains bounds", func(t *testing.T) {
		padded := sane.ApplyCropMargin(bounds, bounds, 300)
		assert.Equal(t, bounds, padded)
	})
}

func TestAutoCropFile_Image(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "test.png")

	// Create and save test image
	img := image.NewRGBA(image.Rect(0, 0, 400, 400))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, 150, 200), &image.Uniform{C: color.Black}, image.Point{}, draw.Src)

	f, err := os.Create(imgPath)
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, img))
	f.Close()

	mock := runner.NewMockRunner()
	client := sane.NewClient(mock)

	err = client.AutoCrop(context.Background(), imgPath, 300)
	require.NoError(t, err)

	// Verify cropped file on disk
	rf, err := os.Open(imgPath)
	require.NoError(t, err)
	defer rf.Close()

	cropped, err := png.Decode(rf)
	require.NoError(t, err)
	assert.True(t, cropped.Bounds().Dx() < 200)
	assert.True(t, cropped.Bounds().Dy() < 250)
}
