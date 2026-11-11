package sane

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

// CalculateCropBounds detects the rectangular document boundary on a flatbed scan
// using downsampled Sobel edge/line gradients and adaptive scanner-lid background modeling.
func CalculateCropBounds(img image.Image) image.Rectangle {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 30 || h <= 30 {
		return bounds
	}

	const maxGridDim = 1000
	step := maxInt(1, maxInt(w, h)/maxGridDim)
	gw := w / step
	gh := h / step
	if gw < 20 || gh < 20 {
		return bounds
	}

	// 1. Build downsampled RGB + luminance grid using box averaging to suppress pixel noise.
	rGrid := make([]uint8, gw*gh)
	gGrid := make([]uint8, gw*gh)
	bGrid := make([]uint8, gw*gh)
	lGrid := make([]uint8, gw*gh)

	sampleStep := maxInt(1, step/3)
	for gy := 0; gy < gh; gy++ {
		y0 := bounds.Min.Y + gy*step
		y1 := minInt(bounds.Max.Y, y0+step)
		for gx := 0; gx < gw; gx++ {
			x0 := bounds.Min.X + gx*step
			x1 := minInt(bounds.Max.X, x0+step)
			var sumR, sumG, sumB, count uint32
			for y := y0; y < y1; y += sampleStep {
				for x := x0; x < x1; x += sampleStep {
					r, g, b, _ := img.At(x, y).RGBA()
					sumR += r >> 8
					sumG += g >> 8
					sumB += b >> 8
					count++
				}
			}
			if count == 0 {
				count = 1
			}
			r8 := uint8(sumR / count)
			g8 := uint8(sumG / count)
			b8 := uint8(sumB / count)
			idx := gy*gw + gx
			rGrid[idx] = r8
			gGrid[idx] = g8
			bGrid[idx] = b8
			lGrid[idx] = uint8((299*uint32(r8) + 587*uint32(g8) + 114*uint32(b8)) / 1000)
		}
	}

	// If step == 1, apply a 3x3 smoothing pass on luminance to suppress single-pixel dust specs.
	if step == 1 {
		smoothed := make([]uint8, gw*gh)
		copy(smoothed, lGrid)
		for y := 1; y < gh-1; y++ {
			for x := 1; x < gw-1; x++ {
				sum := uint32(lGrid[(y-1)*gw+(x-1)]) + 2*uint32(lGrid[(y-1)*gw+x]) + uint32(lGrid[(y-1)*gw+(x+1)]) +
					2*uint32(lGrid[y*gw+(x-1)]) + 4*uint32(lGrid[y*gw+x]) + 2*uint32(lGrid[y*gw+(x+1)]) +
					uint32(lGrid[(y+1)*gw+(x-1)]) + 2*uint32(lGrid[(y+1)*gw+x]) + uint32(lGrid[(y+1)*gw+(x+1)])
				smoothed[y*gw+x] = uint8(sum / 16)
			}
		}
		lGrid = smoothed
	}

	// 2. Compute directional Sobel gradients and thin-line responses across the grid.
	// gxGrid responds to vertical edges/lines (|), gyGrid responds to horizontal edges/lines (___).
	gxGrid := make([]uint8, gw*gh)
	gyGrid := make([]uint8, gw*gh)

	for y := 1; y < gh-1; y++ {
		for x := 1; x < gw-1; x++ {
			idx := y*gw + x
			gxVal := channelGradientX(lGrid, gw, x, y)
			if v := channelGradientX(rGrid, gw, x, y); v > gxVal {
				gxVal = v
			}
			if v := channelGradientX(gGrid, gw, x, y); v > gxVal {
				gxVal = v
			}
			if v := channelGradientX(bGrid, gw, x, y); v > gxVal {
				gxVal = v
			}

			gyVal := channelGradientY(lGrid, gw, x, y)
			if v := channelGradientY(rGrid, gw, x, y); v > gyVal {
				gyVal = v
			}
			if v := channelGradientY(gGrid, gw, x, y); v > gyVal {
				gyVal = v
			}
			if v := channelGradientY(bGrid, gw, x, y); v > gyVal {
				gyVal = v
			}

			gxGrid[idx] = uint8(minInt(255, gxVal))
			gyGrid[idx] = uint8(minInt(255, gyVal))
		}
	}

	// 3. Estimate empty scanner lid gradient noise floor from smooth interior regions.
	bezelX := maxInt(2, gw*25/1000)
	bezelY := maxInt(2, gh*25/1000)

	lidGradNoise := estimateLidGradNoise(gxGrid, gyGrid, gw, gh, bezelX, bezelY)
	edgeThresh := uint8(maxInt(7, int(lidGradNoise)*3+5))

	// 4. Identify verified rectangular edge and content segments.
	// A scanned item is always a rectangle: its horizontal boundaries/features form horizontal runs
	// of vertical gradient (gyGrid), and its vertical boundaries/features form vertical runs
	// of horizontal gradient (gxGrid). Smooth lid shading and isolated dust specs fail run checks.
	verified := make([]bool, gw*gh)
	minRunX := maxInt(6, gw*4/100)
	minRunY := maxInt(6, gh*4/100)
	gapX := maxInt(2, gw/100)
	gapY := maxInt(2, gh/100)

	// Horizontal runs (captures horizontal cut lines `________|` and horizontal content edges)
	rowActive := make([]bool, gw)
	for y := bezelY; y < gh-bezelY; y++ {
		for x := 0; x < gw; x++ {
			rowActive[x] = false
			if x >= bezelX && x < gw-bezelX {
				idx := y*gw + x
				gyBand := maxUint8(gyGrid[idx], maxUint8(gyGrid[(y-1)*gw+x], gyGrid[minInt(gh-1, y+1)*gw+x]))
				if gyBand >= edgeThresh {
					rowActive[x] = true
				}
			}
		}
		markBridgedRuns(rowActive, gapX, minRunX, func(x int) {
			verified[y*gw+x] = true
		})
	}

	// Vertical runs (captures vertical cut lines `|` and vertical content edges)
	colActive := make([]bool, gh)
	for x := bezelX; x < gw-bezelX; x++ {
		for y := 0; y < gh; y++ {
			colActive[y] = false
			if y >= bezelY && y < gh-bezelY {
				idx := y*gw + x
				gxBand := maxUint8(gxGrid[idx], maxUint8(gxGrid[y*gw+(x-1)], gxGrid[y*gw+minInt(gw-1, x+1)]))
				if gxBand >= edgeThresh {
					colActive[y] = true
				}
			}
		}
		markBridgedRuns(colActive, gapY, minRunY, func(y int) {
			verified[y*gw+x] = true
		})
	}

	// 5. Project verified cells onto rows and columns while excluding top/left ruler bezel strips
	// from inflating the orthogonal axis.
	colCounts := make([]int, gw)
	rowCounts := make([]int, gh)
	topInset := maxInt(bezelY, gh*5/100)
	leftInset := maxInt(bezelX, gw*5/100)

	for y := topInset; y < gh-bezelY; y++ {
		for x := bezelX; x < gw-bezelX; x++ {
			if verified[y*gw+x] {
				colCounts[x]++
			}
		}
	}
	for y := bezelY; y < gh-bezelY; y++ {
		for x := leftInset; x < gw-bezelX; x++ {
			if verified[y*gw+x] {
				rowCounts[y]++
			}
		}
	}

	trimOuterBezel(colCounts, gw)
	trimOuterBezel(rowCounts, gh)

	minX, maxX, foundX := findActiveSpan(colCounts)
	minY, maxY, foundY := findActiveSpan(rowCounts)
	if !foundX || !foundY {
		return bounds
	}

	// 6. Origin corner anchoring:
	// Documents placed at the top-left scanner guide (0, 0) may have blank white cardstock
	// in their upper/left regions and no top/left shadow line. Unless a real top/left boundary
	// spans >= 40% of the rectangle's width/height at minY/minX, anchor to the origin (0, 0).
	rectW := maxX - minX + 1
	rectH := maxY - minY + 1

	if minY <= gh*8/100 || !hasBoundaryLineH(verified, gw, gh, minY, minX, maxX, maxInt(4, rectW*40/100)) {
		minY = 0
	}
	if minX <= gw*8/100 || !hasBoundaryLineV(verified, gw, gh, minX, minY, maxY, maxInt(4, rectH*40/100)) {
		minX = 0
	}

	// 7. Penumbra & skew peak snapping:
	// A small local window (at most 4-5 grid cells) snaps outer LED penumbra / Sobel blur
	// to the boundary without reaching inward into document content.
	winX := minInt(12, maxInt(4, gw*12/1000))
	winY := minInt(12, maxInt(4, gh*12/1000))

	maxX = refineMaxBoundaryX(gxGrid, gw, gh, minX, maxX, minY, maxY, winX)
	maxY = refineMaxBoundaryY(gyGrid, gw, gh, minX, maxX, minY, maxY, winY)
	if minX > 0 {
		minX = refineMinBoundaryX(gxGrid, gw, gh, minX, maxX, minY, maxY, winX)
	}
	if minY > 0 {
		minY = refineMinBoundaryY(gyGrid, gw, gh, minX, maxX, minY, maxY, winY)
	}

	pxMinX := 0
	if minX > 0 {
		pxMinX = maxInt(0, minX*step)
	}
	pxMinY := 0
	if minY > 0 {
		pxMinY = maxInt(0, minY*step)
	}
	pxMaxX := minInt(w-1, maxInt(pxMinX+1, maxX*step))
	pxMaxY := minInt(h-1, maxInt(pxMinY+1, maxY*step))

	cropW := pxMaxX - pxMinX + 1
	cropH := pxMaxY - pxMinY + 1

	// If detected crop spans virtually the entire scanner bed on both axes, keep full bounds.
	if cropW >= w*98/100 && cropH >= h*98/100 {
		return bounds
	}

	return image.Rect(bounds.Min.X+pxMinX, bounds.Min.Y+pxMinY, bounds.Min.X+pxMaxX+1, bounds.Min.Y+pxMaxY+1)
}

func channelGradientX(grid []uint8, gw, x, y int) int {
	idx := y*gw + x
	sx := (int(grid[(y-1)*gw+(x+1)]) + 2*int(grid[y*gw+(x+1)]) + int(grid[(y+1)*gw+(x+1)])) -
		(int(grid[(y-1)*gw+(x-1)]) + 2*int(grid[y*gw+(x-1)]) + int(grid[(y+1)*gw+(x-1)]))
	gxVal := absInt(sx) / 4
	lx := absInt(2*int(grid[idx])-int(grid[y*gw+(x-1)])-int(grid[y*gw+(x+1)])) / 2
	if lx > gxVal {
		gxVal = lx
	}
	return gxVal
}

func channelGradientY(grid []uint8, gw, x, y int) int {
	idx := y*gw + x
	sy := (int(grid[(y+1)*gw+(x-1)]) + 2*int(grid[(y+1)*gw+x]) + int(grid[(y+1)*gw+(x+1)])) -
		(int(grid[(y-1)*gw+(x-1)]) + 2*int(grid[(y-1)*gw+x]) + int(grid[(y-1)*gw+(x+1)]))
	gyVal := absInt(sy) / 4
	ly := absInt(2*int(grid[idx])-int(grid[(y-1)*gw+x])-int(grid[(y+1)*gw+x])) / 2
	if ly > gyVal {
		gyVal = ly
	}
	return gyVal
}

// estimateLidGradNoise samples smooth cells in the far-right and far-bottom interior of the bed
// to determine the empty scanner lid's baseline gradient noise floor.
func estimateLidGradNoise(gxGrid, gyGrid []uint8, gw, gh, bezelX, bezelY int) uint8 {
	var gradSamples []uint8

	stripW := maxInt(4, gw*15/100)
	stripH := maxInt(4, gh*15/100)

	xStart := maxInt(bezelX, gw-bezelX-stripW)
	yStart := maxInt(bezelY, gh-bezelY-stripH)

	for y := bezelY; y < gh-bezelY; y++ {
		for x := bezelX; x < gw-bezelX; x++ {
			if x < xStart && y < yStart {
				continue
			}
			idx := y*gw + x
			grad := maxUint8(gxGrid[idx], gyGrid[idx])
			if grad <= 6 {
				gradSamples = append(gradSamples, grad)
			}
		}
	}

	if len(gradSamples) == 0 {
		return 1
	}

	return medianUint8(gradSamples)
}

func medianUint8(vals []uint8) uint8 {
	var counts [256]int
	for _, v := range vals {
		counts[v]++
	}
	target := len(vals) / 2
	acc := 0
	for i, c := range counts {
		acc += c
		if acc > target {
			return uint8(i)
		}
	}
	return 1
}

func markBridgedRuns(active []bool, maxGap, minLen int, mark func(idx int)) {
	n := len(active)
	bridged := make([]bool, n)
	copy(bridged, active)

	// Bridge small gaps up to maxGap
	lastActive := -1
	for i := 0; i < n; i++ {
		if active[i] {
			if lastActive >= 0 && i-lastActive-1 <= maxGap {
				for k := lastActive + 1; k < i; k++ {
					bridged[k] = true
				}
			}
			lastActive = i
		}
	}

	// Emit contiguous runs of length >= minLen
	runStart := -1
	for i := 0; i <= n; i++ {
		if i < n && bridged[i] {
			if runStart < 0 {
				runStart = i
			}
		} else if runStart >= 0 {
			if i-runStart >= minLen {
				for k := runStart; k < i; k++ {
					if active[k] {
						mark(k)
					}
				}
			}
			runStart = -1
		}
	}
}

// trimOuterBezel zeroes out isolated activity sitting in the outer 8% of the bed
// when separated from the main document by an empty scanner-lid corridor.
func trimOuterBezel(counts []int, n int) {
	outerLimit := n * 92 / 100
	minEmptyGap := maxInt(4, n*4/100)

	emptyRun := 0
	for i := n - 1; i >= 0; i-- {
		if counts[i] == 0 {
			emptyRun++
		} else {
			if i < outerLimit {
				break
			}
			emptyRun = 0
		}
		if emptyRun >= minEmptyGap && i < outerLimit {
			for k := i + emptyRun; k < n; k++ {
				counts[k] = 0
			}
			break
		}
	}
}

func findActiveSpan(counts []int) (int, int, bool) {
	minIdx, maxIdx := -1, -1
	for i, c := range counts {
		if c > 0 {
			if minIdx < 0 {
				minIdx = i
			}
			maxIdx = i
		}
	}
	if minIdx < 0 {
		return 0, 0, false
	}
	return minIdx, maxIdx, true
}

func hasBoundaryLineH(verified []bool, gw, gh, y, minX, maxX, minCount int) bool {
	count := 0
	y0 := maxInt(0, y-1)
	y1 := minInt(gh-1, y+2)
	for x := minX; x <= maxX; x++ {
		for yy := y0; yy <= y1; yy++ {
			if verified[yy*gw+x] {
				count++
				break
			}
		}
	}
	return count >= minCount
}

func hasBoundaryLineV(verified []bool, gw, gh, x, minY, maxY, minCount int) bool {
	count := 0
	x0 := maxInt(0, x-1)
	x1 := minInt(gw-1, x+2)
	for y := minY; y <= maxY; y++ {
		for xx := x0; xx <= x1; xx++ {
			if verified[y*gw+xx] {
				count++
				break
			}
		}
	}
	return count >= minCount
}

func refineMaxBoundaryX(gxGrid []uint8, gw, gh, minX, maxX, minY, maxY, win int) int {
	midY := (minY + maxY) / 2
	topScores := sumColsRange(gxGrid, gw, gh, minY, midY)
	botScores := sumColsRange(gxGrid, gw, gh, midY, maxY)
	allScores := sumColsRange(gxGrid, gw, gh, minY, maxY)

	snappedAll, okAll := snapMaxEdgePeak(allScores, maxX, minX, win)
	snappedTop, okTop := snapMaxEdgePeak(topScores, maxX, minX, win)
	snappedBot, okBot := snapMaxEdgePeak(botScores, maxX, minX, win)

	if okTop && okBot && absInt(snappedTop-snappedBot) <= win {
		return maxInt(snappedTop, snappedBot)
	}
	if okAll {
		return snappedAll
	}
	return maxX
}

func refineMinBoundaryX(gxGrid []uint8, gw, gh, minX, maxX, minY, maxY, win int) int {
	midY := (minY + maxY) / 2
	topScores := sumColsRange(gxGrid, gw, gh, minY, midY)
	botScores := sumColsRange(gxGrid, gw, gh, midY, maxY)
	allScores := sumColsRange(gxGrid, gw, gh, minY, maxY)

	snappedAll, okAll := snapMinEdgePeak(allScores, minX, maxX, win)
	snappedTop, okTop := snapMinEdgePeak(topScores, minX, maxX, win)
	snappedBot, okBot := snapMinEdgePeak(botScores, minX, maxX, win)

	if okTop && okBot && absInt(snappedTop-snappedBot) <= win {
		return minInt(snappedTop, snappedBot)
	}
	if okAll {
		return snappedAll
	}
	return minX
}

func refineMaxBoundaryY(gyGrid []uint8, gw, gh, minX, maxX, minY, maxY, win int) int {
	midX := (minX + maxX) / 2
	leftScores := sumRowsRange(gyGrid, gw, gh, minX, midX)
	rightScores := sumRowsRange(gyGrid, gw, gh, midX, maxX)
	allScores := sumRowsRange(gyGrid, gw, gh, minX, maxX)

	snappedAll, okAll := snapMaxEdgePeak(allScores, maxY, minY, win)
	snappedLeft, okLeft := snapMaxEdgePeak(leftScores, maxY, minY, win)
	snappedRight, okRight := snapMaxEdgePeak(rightScores, maxY, minY, win)

	if okLeft && okRight && absInt(snappedLeft-snappedRight) <= win {
		return maxInt(snappedLeft, snappedRight)
	}
	if okAll {
		return snappedAll
	}
	return maxY
}

func refineMinBoundaryY(gyGrid []uint8, gw, gh, minX, maxX, minY, maxY, win int) int {
	midX := (minX + maxX) / 2
	leftScores := sumRowsRange(gyGrid, gw, gh, minX, midX)
	rightScores := sumRowsRange(gyGrid, gw, gh, midX, maxX)
	allScores := sumRowsRange(gyGrid, gw, gh, minX, maxX)

	snappedAll, okAll := snapMinEdgePeak(allScores, minY, maxY, win)
	snappedLeft, okLeft := snapMinEdgePeak(leftScores, minY, maxY, win)
	snappedRight, okRight := snapMinEdgePeak(rightScores, minY, maxY, win)

	if okLeft && okRight && absInt(snappedLeft-snappedRight) <= win {
		return minInt(snappedLeft, snappedRight)
	}
	if okAll {
		return snappedAll
	}
	return minY
}

func sumColsRange(grid []uint8, gw, gh, y0, y1 int) []int {
	scores := make([]int, gw)
	y0 = maxInt(0, y0)
	y1 = minInt(gh-1, y1)
	for y := y0; y <= y1; y++ {
		rowOff := y * gw
		for x := 0; x < gw; x++ {
			scores[x] += int(grid[rowOff+x])
		}
	}
	return scores
}

func sumRowsRange(grid []uint8, gw, gh, x0, x1 int) []int {
	scores := make([]int, gh)
	x0 = maxInt(0, x0)
	x1 = minInt(gw-1, x1)
	for y := 0; y < gh; y++ {
		rowOff := y * gw
		sum := 0
		for x := x0; x <= x1; x++ {
			sum += int(grid[rowOff+x])
		}
		scores[y] = sum
	}
	return scores
}

// snapMaxEdgePeak walks inward from maxIdx across the outer penumbra tail until it crosses
// the outermost boundary gradient peak and lands on its inner shoulder.
func snapMaxEdgePeak(scores []int, maxIdx, minIdx, win int) (int, bool) {
	limit := maxInt(minIdx, maxIdx-win)
	windowMax := 0
	for i := limit; i <= maxIdx; i++ {
		if scores[i] > windowMax {
			windowMax = scores[i]
		}
	}
	if windowMax == 0 {
		return maxInt(minIdx, maxIdx-2), false
	}

	peak := 0
	best := maxIdx
	for i := maxIdx; i >= limit; i-- {
		s := scores[i]
		if s > peak {
			peak = s
			best = i
		} else if peak >= windowMax/2 {
			if s >= peak*60/100 {
				best = i
			} else if i < best {
				return maxInt(minIdx, best), true
			}
		}
	}
	return maxInt(minIdx, best), true
}

// snapMinEdgePeak walks outward from minIdx across the outer penumbra tail until it crosses
// the outermost boundary gradient peak and lands on its boundary.
func snapMinEdgePeak(scores []int, minIdx, maxIdx, win int) (int, bool) {
	limit := minInt(maxIdx, minIdx+win)
	windowMax := 0
	for i := minIdx; i <= limit; i++ {
		if scores[i] > windowMax {
			windowMax = scores[i]
		}
	}
	if windowMax == 0 {
		return minInt(maxIdx, minIdx), false
	}

	peak := 0
	best := minIdx
	for i := minIdx; i <= limit; i++ {
		s := scores[i]
		if s > peak {
			peak = s
			best = i
		} else if peak >= windowMax/2 {
			if s >= peak*60/100 {
				best = i
			} else if i > best {
				return minInt(maxIdx, best), true
			}
		}
	}
	return minInt(maxIdx, best), true
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func minUint8(a, b uint8) uint8 {
	if a < b {
		return a
	}
	return b
}

func maxUint8(a, b uint8) uint8 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ApplyCropMargin adds a conservative safety padding margin around detected crop bounds
// to prevent clipping text, signatures, or margins near the document edge.
func ApplyCropMargin(bounds, crop image.Rectangle, resolution int) image.Rectangle {
	if crop.Dx() >= bounds.Dx() && crop.Dy() >= bounds.Dy() {
		return bounds
	}

	margin := 35
	if resolution > 0 {
		margin = maxInt(20, resolution*12/100) // ~0.12 inch margin (~36px at 300 DPI, ~18px at 150 DPI)
	} else {
		margin = maxInt(20, int(float64(maxInt(bounds.Dx(), bounds.Dy()))*0.012))
	}

	minX := crop.Min.X
	if minX > bounds.Min.X {
		minX = maxInt(bounds.Min.X, minX-margin)
	}
	minY := crop.Min.Y
	if minY > bounds.Min.Y {
		minY = maxInt(bounds.Min.Y, minY-margin)
	}
	maxX := minInt(bounds.Max.X, crop.Max.X+margin)
	maxY := minInt(bounds.Max.Y, crop.Max.Y+margin)

	padded := image.Rect(minX, minY, maxX, maxY)
	if padded.Dx() >= bounds.Dx() && padded.Dy() >= bounds.Dy() {
		return bounds
	}
	return padded
}

// AutoCropFile detects the content boundaries and crops the file, trimming empty scanner bed margins.
func (c *Client) AutoCropFile(ctx context.Context, filePath string, resolution int) error {
	ext := strings.ToLower(filepath.Ext(filePath))

	if ext == ".png" || ext == ".jpg" || ext == ".jpeg" {
		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open file for crop: %w", err)
		}
		img, _, decErr := image.Decode(f)
		f.Close()
		if decErr != nil {
			return fmt.Errorf("decode image: %w", decErr)
		}

		crop := CalculateCropBounds(img)
		crop = ApplyCropMargin(img.Bounds(), crop, resolution)
		if crop.Dx() >= img.Bounds().Dx() && crop.Dy() >= img.Bounds().Dy() {
			return nil
		}

		// Try native Go crop first
		var croppedImg image.Image
		if si, ok := img.(subImager); ok {
			croppedImg = si.SubImage(crop)
		} else {
			rgba := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
			draw.Draw(rgba, rgba.Bounds(), img, crop.Min, draw.Src)
			croppedImg = rgba
		}

		outF, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("create cropped file: %w", err)
		}
		defer outF.Close()

		if ext == ".png" {
			return png.Encode(outF, croppedImg)
		}
		return jpeg.Encode(outF, croppedImg, &jpeg.Options{Quality: 95})
	}

	if ext == ".pdf" {
		tool := "magick"
		if _, err := c.r.LookPath("magick"); err != nil {
			if _, err := c.r.LookPath("convert"); err == nil {
				tool = "convert"
			} else {
				return fmt.Errorf("magick or convert required to auto-crop PDF: %w", err)
			}
		}

		// Rasterize first page at 72 DPI to detect bounding box
		out, err := c.r.Run(ctx, tool, "-density", "72", filePath+"[0]", "png:-")
		if err != nil {
			return fmt.Errorf("rasterize pdf for crop: %w", err)
		}

		img, _, decErr := image.Decode(bytes.NewReader(out))
		if decErr != nil {
			return fmt.Errorf("decode pdf preview: %w", decErr)
		}

		crop := CalculateCropBounds(img)
		crop = ApplyCropMargin(img.Bounds(), crop, 72)
		if crop.Dx() >= img.Bounds().Dx() && crop.Dy() >= img.Bounds().Dy() {
			return nil
		}

		scale := 1.0
		if resolution > 0 {
			scale = float64(resolution) / 72.0
		}
		scaledW := int(float64(crop.Dx()) * scale)
		scaledH := int(float64(crop.Dy()) * scale)
		scaledX := int(float64(crop.Min.X) * scale)
		scaledY := int(float64(crop.Min.Y) * scale)
		cropSpec := fmt.Sprintf("%dx%d+%d+%d", scaledW, scaledH, scaledX, scaledY)

		var args []string
		if resolution > 0 {
			args = append(args, "-density", strconv.Itoa(resolution))
		}
		args = append(args, filePath, "-crop", cropSpec, "+repage", filePath)

		_, err = c.r.Run(ctx, tool, args...)
		if err != nil {
			return fmt.Errorf("pdf crop command failed: %w", err)
		}
		return nil
	}

	return nil
}
