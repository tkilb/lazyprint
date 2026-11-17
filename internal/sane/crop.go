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

// CropAggression controls how aggressively auto-crop trims the scanner base
// and how much safety margin is preserved around the detected document.
type CropAggression string

const (
	CropOff    CropAggression = "off"
	Crop10Pct  CropAggression = "10%"
	Crop20Pct  CropAggression = "20%"
	Crop30Pct  CropAggression = "30%"
	Crop40Pct  CropAggression = "40%"
	Crop50Pct  CropAggression = "50%"
	Crop60Pct  CropAggression = "60%"
	Crop70Pct  CropAggression = "70%"
	Crop80Pct  CropAggression = "80%"
	Crop90Pct  CropAggression = "90%"
	Crop100Pct CropAggression = "100%"

	// Preset aliases mapped to percentage steps.
	CropDefault      CropAggression = Crop30Pct
	CropConservative CropAggression = Crop20Pct
	CropBalanced     CropAggression = Crop30Pct
	CropAggressive   CropAggression = Crop80Pct
)

// CropAggressions returns all supported crop aggression options in chooser order.
func CropAggressions() []CropAggression {
	return []CropAggression{
		CropOff,
		Crop10Pct,
		Crop20Pct,
		Crop30Pct,
		Crop40Pct,
		Crop50Pct,
		Crop60Pct,
		Crop70Pct,
		Crop80Pct,
		Crop90Pct,
		Crop100Pct,
	}
}

// ParseCropAggression normalizes a string into a canonical CropAggression value.
func ParseCropAggression(s string) CropAggression {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "off", "none", "false", "disabled", "0", "0%":
		return CropOff
	case "10%", "10":
		return Crop10Pct
	case "20%", "20", "conservative", "low", "gentle", "safe":
		return Crop20Pct
	case "30%", "30", "balanced", "medium", "normal", "default", "":
		return Crop30Pct
	case "40%", "40":
		return Crop40Pct
	case "50%", "50":
		return Crop50Pct
	case "60%", "60":
		return Crop60Pct
	case "70%", "70":
		return Crop70Pct
	case "80%", "80", "aggressive", "high", "tight", "strict":
		return Crop80Pct
	case "90%", "90":
		return Crop90Pct
	case "100%", "100":
		return Crop100Pct
	default:
		return CropDefault
	}
}

// Percent returns the integer percentage (0 for off, 10..100 for active levels).
func (a CropAggression) Percent() int {
	switch ParseCropAggression(string(a)) {
	case CropOff:
		return 0
	case Crop10Pct:
		return 10
	case Crop20Pct:
		return 20
	case Crop30Pct:
		return 30
	case Crop40Pct:
		return 40
	case Crop50Pct:
		return 50
	case Crop60Pct:
		return 60
	case Crop70Pct:
		return 70
	case Crop80Pct:
		return 80
	case Crop90Pct:
		return 90
	case Crop100Pct:
		return 100
	default:
		return 30
	}
}

// String returns the canonical aggression label.
func (a CropAggression) String() string {
	return string(ParseCropAggression(string(a)))
}

// Meaning returns a short human-readable description for chooser UI display.
func (a CropAggression) Meaning() string {
	switch ParseCropAggression(string(a)) {
	case CropOff:
		return "Keep full scanner bed"
	case Crop10Pct:
		return "Minimal trim; widest safety margin"
	case Crop20Pct:
		return "Conservative trim; wide safety margin"
	case Crop30Pct:
		return "Default trim; safe document margin"
	case Crop40Pct:
		return "Moderate trim; standard margin"
	case Crop50Pct:
		return "Balanced trim; compact margin"
	case Crop60Pct:
		return "Firm trim; narrow margin"
	case Crop70Pct:
		return "Tight trim; minimal margin"
	case Crop80Pct:
		return "Aggressive trim; flush to paper edge"
	case Crop90Pct:
		return "Very aggressive; strip edge shadows"
	case Crop100Pct:
		return "Maximum trim; strip skewed borders"
	default:
		return "Default trim; safe document margin"
	}
}

type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

type aggressionParams struct {
	pct                int
	edgeNoiseMult      int
	edgeNoiseBase      int
	minEdgeThresh      int
	surfaceColorThresh int
	surfaceRatioPct    int // percentage from doc color toward lid background allowed as paper surface
	minClusterSpanProm int // minimum cluster span in per-mille of grid dimension
	minClusterCells    int
	marginDPIHundredth int // outward margin in hundredths of an inch (can be negative at 90-100%)
	minMarginPx        int
	penumbraWinScale   int
}

func paramsForAggression(agg CropAggression) aggressionParams {
	pct := agg.Percent()
	if pct <= 0 {
		pct = 30
	}

	// Interpolate parameters smoothly across 10%..100%.
	// Lower pct = more sensitive to faint paper/ink, wider outward safety margin.
	// Higher pct = stricter against lid shadows/skew wedges, tighter/inset margin.
	edgeMult := 2 + (pct / 35)          // 2 at 10-30%, 3 at 40-60%, 4 at 70-100%
	edgeBase := 3 + (pct / 25)          // 3..7
	minEdge := 5 + (pct / 25)           // 5..9
	surfThresh := 6 + (pct / 10)        // 7..16
	surfRatio := 65 - (pct * 35 / 100) // 62% at 10%, 55% at 30%, 48% at 50%, 37% at 80%, 30% at 100%
	clusterSpan := 10                  // Always preserve short handwritten words near margins
	clusterCells := 12
	winScale := 12 + (pct * 8 / 100) // 12..20

	// Outward margin in hundredths of an inch (at 300 DPI, 1 unit = 3px):
	// 10%: +20 (60px), 20%: +15 (45px), 30%: +10 (30px), 40%: +6 (18px),
	// 50%: +4 (12px), 60%: +2 (6px), 70%: +1 (3px), 80%: 0 (0px),
	// 90%: -2 (-6px inset), 100%: -5 (-15px inset).
	var marginHundredth, minMargin int
	switch {
	case pct <= 10:
		marginHundredth, minMargin = 20, 36
	case pct <= 20:
		marginHundredth, minMargin = 15, 28
	case pct <= 30:
		marginHundredth, minMargin = 10, 18
	case pct <= 40:
		marginHundredth, minMargin = 6, 12
	case pct <= 50:
		marginHundredth, minMargin = 4, 8
	case pct <= 60:
		marginHundredth, minMargin = 2, 4
	case pct <= 70:
		marginHundredth, minMargin = 1, 2
	default:
		marginHundredth, minMargin = 0, 0
	}

	return aggressionParams{
		pct:                pct,
		edgeNoiseMult:      edgeMult,
		edgeNoiseBase:      edgeBase,
		minEdgeThresh:      minEdge,
		surfaceColorThresh: surfThresh,
		surfaceRatioPct:    surfRatio,
		minClusterSpanProm: clusterSpan,
		minClusterCells:    clusterCells,
		marginDPIHundredth: marginHundredth,
		minMarginPx:        minMargin,
		penumbraWinScale:   winScale,
	}
}

// CalculateCropBounds detects the rectangular document boundary on a flatbed scan
// using the default aggression (30%).
func CalculateCropBounds(img image.Image) image.Rectangle {
	return CalculateCropBoundsWithAggression(img, CropDefault)
}

// CalculateCropBoundsWithAggression detects the rectangular document boundary on a flatbed scan
// combining interior paper surface modeling, peak-preserving ink/handwriting detection,
// and multi-scale paper-edge shadow detection.
func CalculateCropBoundsWithAggression(img image.Image, aggression CropAggression) image.Rectangle {
	bounds := img.Bounds()
	if ParseCropAggression(string(aggression)) == CropOff {
		return bounds
	}

	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 30 || h <= 30 {
		return bounds
	}

	params := paramsForAggression(aggression)

	const maxGridDim = 1000
	step := maxInt(1, maxInt(w, h)/maxGridDim)
	gw := w / step
	gh := h / step
	if gw < 20 || gh < 20 {
		return bounds
	}

	// 1. Build downsampled RGB + luminance grid (box average for smooth surface comparison)
	// AND minLGrid / maxLGrid (darkest/brightest pixel in each block so 1-pixel pen strokes
	// and thin paper-edge shadow lines are never washed out at 300-600 DPI).
	rGrid := make([]uint8, gw*gh)
	gGrid := make([]uint8, gw*gh)
	bGrid := make([]uint8, gw*gh)
	lGrid := make([]uint8, gw*gh)
	minLGrid := make([]uint8, gw*gh)
	maxLGrid := make([]uint8, gw*gh)

	sampleStep := maxInt(1, step/3)
	for gy := 0; gy < gh; gy++ {
		y0 := bounds.Min.Y + gy*step
		y1 := minInt(bounds.Max.Y, y0+step)
		for gx := 0; gx < gw; gx++ {
			x0 := bounds.Min.X + gx*step
			x1 := minInt(bounds.Max.X, x0+step)
			var sumR, sumG, sumB, count uint32
			minL := uint8(255)
			maxL := uint8(0)
			for y := y0; y < y1; y += sampleStep {
				for x := x0; x < x1; x += sampleStep {
					r, g, b, _ := img.At(x, y).RGBA()
					r8 := r >> 8
					g8 := g >> 8
					b8 := b >> 8
					sumR += r8
					sumG += g8
					sumB += b8
					lum := uint8((299*r8 + 587*g8 + 114*b8) / 1000)
					if lum < minL {
						minL = lum
					}
					if lum > maxL {
						maxL = lum
					}
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
			minLGrid[idx] = minL
			maxLGrid[idx] = maxL
		}
	}

	// If step == 1, apply a 3x3 smoothing pass on lGrid to suppress single-pixel sensor noise.
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

	// 2. Compute directional Sobel gradients across the grid, plus stroke contrast from minLGrid.
	gxGrid := make([]uint8, gw*gh)
	gyGrid := make([]uint8, gw*gh)
	strokeGrid := make([]uint8, gw*gh)

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
			if step > 1 {
				if v := channelGradientX(minLGrid, gw, x, y) / 2; v > gxVal {
					gxVal = v
				}
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
			if step > 1 {
				if v := channelGradientY(minLGrid, gw, x, y) / 2; v > gyVal {
					gyVal = v
				}
			}

			gxGrid[idx] = uint8(minInt(255, gxVal))
			gyGrid[idx] = uint8(minInt(255, gyVal))

			// Local stroke contrast: captures thin diagonal/cursive ballpoint pen strokes
			// inside a cell even when Sobel X/Y cancels out on a 45-degree stroke.
			bgL := maxUint8(maxLGrid[idx], maxUint8(lGrid[y*gw+(x-1)], lGrid[y*gw+(x+1)]))
			if bgL > minLGrid[idx] {
				strokeGrid[idx] = (bgL - minLGrid[idx]) / 2
			}
		}
	}

	// 3. Exclude outermost ~1.8% hardware bezel strip during initial edge/noise detection.
	bezelX := maxInt(2, gw*18/1000)
	bezelY := maxInt(2, gh*18/1000)

	lidGradNoise := estimatePerimeterGradNoise(gxGrid, gyGrid, gw, gh, bezelX, bezelY)
	edgeThresh := uint8(maxInt(params.minEdgeThresh, int(lidGradNoise)*params.edgeNoiseMult+params.edgeNoiseBase))

	// 4. Detect directional boundary runs (captures subtle white-on-white paper shadow lines)
	// AND 2D content clusters (captures cursive handwriting, sparse text, and salutations
	// near page margins without requiring straight 4% single-column runs).
	horizEdge := make([]bool, gw*gh)
	vertEdge := make([]bool, gw*gh)
	verified := make([]bool, gw*gh)

	minRunX := maxInt(6, gw*4/100)
	minRunY := maxInt(6, gh*4/100)
	gapX := maxInt(2, gw/100)
	gapY := maxInt(2, gh/100)

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
			horizEdge[y*gw+x] = true
			verified[y*gw+x] = true
		})
	}

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
			vertEdge[y*gw+x] = true
			verified[y*gw+x] = true
		})
	}

	// 4b. 2D connected content clustering: combines Sobel gradients and peak stroke contrast
	// so thin ballpoint cursive loops and words with spaces near margins are always captured,
	// while excluding pure 1D paper-edge shadow seams so border shadows don't bridge to glass specks.
	contentCluster := detectContentClusters(gxGrid, gyGrid, strokeGrid, minLGrid, vertEdge, horizEdge, gw, gh, bezelX, bezelY, edgeThresh, params)
	for i, ok := range contentCluster {
		if ok {
			verified[i] = true
		}
	}

	// 4c. Model the document's interior paper surface AND the exposed scanner lid background
	// (filtering out paper cells and handwriting halo cells from the perimeter so an 8.5"x11" letter
	// covering 94%+ of the bed never pollutes the lid color with paper or ink).
	docSurf, lid := estimateDocAndLid(rGrid, gGrid, bGrid, lGrid, minLGrid, gxGrid, gyGrid, verified, gw, gh, bezelX, bezelY, lidGradNoise)
	surfVerified := make([]bool, gw*gh)
	if lid.distinctFromDoc {
		markDocumentSurface(rGrid, gGrid, bGrid, lGrid, surfVerified, gw, gh, bezelX, bezelY, docSurf, lid, params)
		for i, ok := range surfVerified {
			if ok {
				verified[i] = true
			}
		}
	}

	// Compute the bounding box of actual ink/content clusters (including ink strokes that reach
	// into the outer bezel strip when the border is paper rather than a dark glass bezel reflection)
	// so no aggression level ever cuts into handwriting or printed text.
	cMinX, cMaxX, cMinY, cMaxY, hasContent := contentBounds(contentCluster, lGrid, minLGrid, strokeGrid, gw, gh, bezelX, bezelY, docSurf)
	for y := 0; y < gh; y++ {
		for x := 0; x < gw; x++ {
			if contentCluster[y*gw+x] {
				verified[y*gw+x] = true
			}
		}
	}

	// 5. Project verified foreground onto rows and columns, keeping directional edges separate
	// so a vertical edge seam (vertEdge) doesn't extend rowCounts (maxY) into blank bottom space,
	// and a horizontal edge seam (horizEdge) doesn't extend colCounts (maxX) into blank right space.
	colCounts := make([]int, gw)
	rowCounts := make([]int, gh)
	topInset := maxInt(bezelY, gh*15/1000)
	leftInset := maxInt(bezelX, gw*15/1000)

	for y := topInset; y < gh-bezelY; y++ {
		for x := bezelX; x < gw-bezelX; x++ {
			idx := y*gw + x
			if vertEdge[idx] || contentCluster[idx] || surfVerified[idx] {
				colCounts[x]++
			}
		}
	}
	for y := bezelY; y < gh-bezelY; y++ {
		for x := leftInset; x < gw-bezelX; x++ {
			idx := y*gw + x
			if horizEdge[idx] || contentCluster[idx] || surfVerified[idx] {
				rowCounts[y]++
			}
		}
	}

	minX, maxX, foundX := findActiveSpan(colCounts)
	minY, maxY, foundY := findActiveSpan(rowCounts)
	if !foundX || !foundY {
		return bounds
	}

	// 6. Origin corner anchoring & unseparated white-on-white margin protection:
	// If the scanner lid and the paper surface have virtually identical color (!lid.distinctFromDoc),
	// check for either a strong Sobel edge line (horizEdge/vertEdge) OR a faint white-on-white
	// paper-edge shadow seam in minLGrid beyond the content bounding box. If neither exists on an axis,
	// extend that axis to the full bed border so interior handwriting and margins are never cut off.
	rectW := maxX - minX + 1
	rectH := maxY - minY + 1
	minEdgeSpanX := maxInt(4, rectW*35/100)
	minEdgeSpanY := maxInt(4, rectH*35/100)

	seamMinX, seamMaxX, seamMinY, seamMaxY := false, false, false, false

	if !lid.distinctFromDoc {
		if minY > gh*8/100 && hasPureEdgeLineH(horizEdge, gw, gh, minY, minX, maxX, minEdgeSpanX) {
			// floating top edge
		} else if seamY, ok := findFaintHorizPaperSeamBefore(minLGrid, gw, gh, minX, maxX, gh*25/100, docSurf); ok && (!hasContent || seamY <= cMinY) {
			minY = seamY
			seamMinY = true
		} else {
			minY = 0
		}

		if minX > gw*8/100 && hasPureEdgeLineV(vertEdge, gw, gh, minX, minY, maxY, minEdgeSpanY) {
			// floating left edge
		} else if seamX, ok := findFaintVertPaperSeamBefore(minLGrid, gw, gh, minY, maxY, gw*25/100, docSurf); ok && (!hasContent || seamX <= cMinX) {
			minX = seamX
			seamMinX = true
		} else {
			minX = 0
		}

		hasVertEdgeMaxX := false
		if edgeX, ok := findPureEdgeLineVNearMax(vertEdge, gw, gh, maxInt(minX, maxX-12), maxX, minY, maxY, minEdgeSpanY); ok {
			maxX = edgeX
			hasVertEdgeMaxX = true
		} else if seamX, ok := findFaintVertPaperSeamAfter(minLGrid, lGrid, gw, gh, minY, maxY, maxX, docSurf); ok && (!hasContent || seamX >= cMaxX) {
			maxX = seamX
			seamMaxX = true
		} else if maxX >= gw*75/100 && maxX <= gw-bezelX-8 && isOuterBlankMinL(minLGrid, gw, gh, maxX+3, gw-bezelX-1, minY, maxY, docSurf) {
			seamMaxX = true
		} else {
			maxX = gw - 1
		}

		if maxY < gh*65/100 {
			if endY, ok := findCardBottomBoundaryY(vertEdge, minLGrid, gw, gh, bezelX, bezelY, minX, maxX, minY, maxY, hasVertEdgeMaxX, docSurf); ok {
				maxY = endY
				seamMaxY = true
			}
		}

		if seamMaxY {
			// already resolved by card bottom boundary
		} else if params.pct >= 80 && maxY >= gh*65/100 && maxY <= gh-bezelY-8 && isOuterBlankMinL(minLGrid, gw, gh, minX, maxX, maxY+3, gh-bezelY-1, docSurf) {
			seamMaxY = true
		} else if edgeY, ok := findPureEdgeLineHNearMax(horizEdge, gw, gh, maxInt(minY, maxY-12), maxY, minX, maxX, minEdgeSpanX); ok {
			maxY = edgeY
		} else if seamY, ok := findFaintHorizPaperSeamAfter(minLGrid, lGrid, gw, gh, minX, maxX, maxY, docSurf); ok {
			maxY = seamY
			seamMaxY = true
		} else if endY, ok := findCardBottomBoundaryY(vertEdge, minLGrid, gw, gh, bezelX, bezelY, minX, maxX, minY, maxY, hasVertEdgeMaxX, docSurf); ok {
			maxY = endY
			seamMaxY = true
		} else if maxY >= gh*75/100 && maxY <= gh-bezelY-8 && isOuterBlankMinL(minLGrid, gw, gh, minX, maxX, maxY+3, gh-bezelY-1, docSurf) {
			seamMaxY = true
		} else {
			maxY = gh - 1
		}
	} else {
		floatingTop := minY > gh*8/100 &&
			hasBoundaryLineH(horizEdge, verified, gw, gh, minY, minX, maxX, maxInt(4, rectW*40/100)) &&
			isStripLidBackground(rGrid, gGrid, bGrid, lGrid, gw, gh, minX, maxX, bezelY, minY-2, lid, params)
		if !floatingTop {
			minY = 0
		}

		floatingLeft := minX > gw*8/100 &&
			hasBoundaryLineV(vertEdge, verified, gw, gh, minX, minY, maxY, maxInt(4, rectH*40/100)) &&
			isStripLidBackground(rGrid, gGrid, bGrid, lGrid, gw, gh, bezelX, minX-2, minY, maxY, lid, params)
		if !floatingLeft {
			minX = 0
		}
	}

	// If the detected paper surface reaches right up to the inner edge of the bezel strip,
	// walk outward column-by-column / row-by-row through any remaining paper surface inside the bezel zone
	// (e.g. US Letter covering 98.5% of the bed width) while stopping before dark bezel reflections or lid.
	if maxX >= gw-bezelX-2 {
		for maxX+1 < gw && isStripPaperSurface(rGrid, gGrid, bGrid, lGrid, gw, gh, maxX+1, maxX+1, minY, maxY, docSurf) {
			maxX++
		}
	}
	if maxY >= gh-bezelY-2 {
		for maxY+1 < gh && isStripPaperSurface(rGrid, gGrid, bGrid, lGrid, gw, gh, minX, maxX, maxY+1, maxY+1, docSurf) {
			maxY++
		}
	}

	// 7. Penumbra & skew peak snapping:
	// Snaps outer LED penumbra / Sobel blur to the physical edge peak when a boundary lies inside the bed.
	winX := minInt(16, maxInt(8, gw*params.penumbraWinScale/1000))
	winY := minInt(16, maxInt(8, gh*params.penumbraWinScale/1000))

	if maxX < gw-bezelX && !seamMaxX {
		maxX = refineMaxBoundaryX(gxGrid, gw, gh, minX, maxX, minY, maxY, winX)
		if params.pct >= 60 {
			for maxX > minX+10 && (!hasContent || maxX > cMaxX+1) && hasPureEdgeLineV(vertEdge, gw, gh, maxX, minY, maxY, minEdgeSpanY/2) {
				maxX--
			}
			inset := (params.pct - 70) / 10
			for i := 0; i < inset && maxX > minX+10 && (!hasContent || maxX > cMaxX+1); i++ {
				maxX--
			}
		}
	}
	if maxY < gh-bezelY && !seamMaxY {
		maxY = refineMaxBoundaryY(gyGrid, gw, gh, minX, maxX, minY, maxY, winY)
		if params.pct >= 60 {
			for maxY > minY+10 && (!hasContent || maxY > cMaxY+1) && hasPureEdgeLineH(horizEdge, gw, gh, maxY, minX, maxX, minEdgeSpanX/2) {
				maxY--
			}
			inset := (params.pct - 70) / 10
			for i := 0; i < inset && maxY > minY+10 && (!hasContent || maxY > cMaxY+1); i++ {
				maxY--
			}
		}
	}
	if minX > 0 && !seamMinX {
		minX = refineMinBoundaryX(gxGrid, gw, gh, minX, maxX, minY, maxY, winX)
	}
	if minY > 0 && !seamMinY {
		minY = refineMinBoundaryY(gyGrid, gw, gh, minX, maxX, minY, maxY, winY)
	}

	// If ink strokes extend into the outer bezel strip (e.g. handwriting reaching the edge of the scan),
	// ensure the crop boundary never cuts off that outer strip.
	if hasContent {
		if cMinX < bezelX {
			minX = minInt(minX, cMinX)
		}
		if cMaxX >= gw-bezelX {
			maxX = maxInt(maxX, cMaxX)
		}
		if cMinY < bezelY {
			minY = minInt(minY, cMinY)
		}
		if cMaxY >= gh-bezelY {
			maxY = maxInt(maxY, cMaxY)
		}
	}

	pxMinX := 0
	if minX > 0 {
		pxMinX = maxInt(0, minX*step)
	}
	pxMinY := 0
	if minY > 0 {
		pxMinY = maxInt(0, minY*step)
	}
	pxMaxX := w - 1
	if maxX < gw-1 {
		pxMaxX = minInt(w-1, maxInt(pxMinX+1, maxX*step))
	}
	pxMaxY := h - 1
	if maxY < gh-1 {
		pxMaxY = minInt(h-1, maxInt(pxMinY+1, maxY*step))
	}

	cropW := pxMaxX - pxMinX + 1
	cropH := pxMaxY - pxMinY + 1

	// If detected crop spans virtually the entire scanner bed on both axes, keep full bounds.
	if cropW >= w*98/100 && cropH >= h*98/100 {
		return bounds
	}

	return image.Rect(bounds.Min.X+pxMinX, bounds.Min.Y+pxMinY, bounds.Min.X+pxMaxX+1, bounds.Min.Y+pxMaxY+1)
}

type surfaceColor struct {
	r, g, b uint8
	lum     uint8
	mad     uint8
	valid   bool
}

type lidBackground struct {
	r, g, b         uint8
	lum             uint8
	gradNoise       uint8
	colorMad        uint8
	distinctFromDoc bool
}

func estimatePerimeterGradNoise(gxGrid, gyGrid []uint8, gw, gh, bezelX, bezelY int) uint8 {
	var gradSamples []uint8
	stripW := maxInt(4, gw*10/100)
	stripH := maxInt(4, gh*10/100)
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

// estimateDocAndLid models the interior paper surface color first, and then models the
// exposed scanner base from smooth perimeter cells that differ from the paper surface.
func estimateDocAndLid(rGrid, gGrid, bGrid, lGrid, minLGrid, gxGrid, gyGrid []uint8, verified []bool, gw, gh, bezelX, bezelY int, gradNoise uint8) (surfaceColor, lidBackground) {
	minX, maxX, minY, maxY := gw, 0, gh, 0
	for y := bezelY; y < gh-bezelY; y++ {
		for x := bezelX; x < gw-bezelX; x++ {
			if verified[y*gw+x] {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if minX >= maxX {
		minX, maxX = bezelX, gw/2
	} else if maxX-minX < gw/10 {
		minX = bezelX
		maxX = maxInt(maxX, gw/2)
	}
	if minY >= maxY {
		minY, maxY = bezelY, gh/2
	} else if maxY-minY < gh/10 {
		minY = bezelY
		maxY = maxInt(maxY, gh/2)
	}

	// Sample smooth interior cells inside the verified span to get the document's paper color.
	var docR, docG, docB, docL []uint8
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			idx := y*gw + x
			if maxUint8(gxGrid[idx], gyGrid[idx]) <= 5 && int(lGrid[idx])-int(minLGrid[idx]) <= 4 {
				docR = append(docR, rGrid[idx])
				docG = append(docG, gGrid[idx])
				docB = append(docB, bGrid[idx])
				docL = append(docL, lGrid[idx])
			}
		}
	}
	if len(docL) < 16 {
		return surfaceColor{}, lidBackground{gradNoise: gradNoise}
	}

	medDocR := medianUint8(docR)
	medDocG := medianUint8(docG)
	medDocB := medianUint8(docB)
	medDocL := medianUint8(docL)
	var docDevs []uint8
	for i := range docL {
		docDevs = append(docDevs, cellColorDist(docR[i], docG[i], docB[i], docL[i], medDocR, medDocG, medDocB, medDocL))
	}
	docMad := medianUint8(docDevs)
	doc := surfaceColor{r: medDocR, g: medDocG, b: medDocB, lum: medDocL, mad: docMad, valid: true}

	// Collect smooth cells in the outermost 2.5% right strip and outermost 2.5% bottom strip
	// (where flatbed scanner base is exposed even when a US Letter page covers 94% of the bed,
	// without reaching inward into a two-tone document like a greeting card).
	stripW := maxInt(3, gw*25/1000)
	stripH := maxInt(3, gh*25/1000)
	xStart := maxInt(bezelX, gw-bezelX-stripW)
	yStart := maxInt(bezelY, gh-bezelY-stripH)

	minDiffFromDoc := uint8(maxInt(6, int(docMad)*3+4))
	var rightAll, rightDiff, botAll, botDiff int
	var diffR, diffG, diffB, diffL []uint8
	var allR, allG, allB, allL []uint8

	for y := bezelY; y < gh-bezelY; y++ {
		for x := bezelX; x < gw-bezelX; x++ {
			inRight := x >= xStart
			inBot := y >= yStart
			if !inRight && !inBot {
				continue
			}
			idx := y*gw + x
			if maxUint8(gxGrid[idx], gyGrid[idx]) <= 4 && int(lGrid[idx])-int(minLGrid[idx]) <= 3 {
				r, g, b, l := rGrid[idx], gGrid[idx], bGrid[idx], lGrid[idx]
				allR = append(allR, r)
				allG = append(allG, g)
				allB = append(allB, b)
				allL = append(allL, l)
				if inRight {
					rightAll++
				}
				if inBot {
					botAll++
				}
				if cellColorDist(r, g, b, l, medDocR, medDocG, medDocB, medDocL) >= minDiffFromDoc {
					diffR = append(diffR, r)
					diffG = append(diffG, g)
					diffB = append(diffB, b)
					diffL = append(diffL, l)
					if inRight {
						rightDiff++
					}
					if inBot {
						botDiff++
					}
				}
			}
		}
	}

	// Only treat the differing cells as a distinct-colored scanner lid if they form a majority
	// (>=60%) of either the outer right strip or the outer bottom strip.
	rightHasLid := rightAll >= 16 && rightDiff*100/rightAll >= 60
	botHasLid := botAll >= 16 && botDiff*100/botAll >= 60
	if len(diffL) >= 32 && (rightHasLid || botHasLid) {
		medR := medianUint8(diffR)
		medG := medianUint8(diffG)
		medB := medianUint8(diffB)
		medL := medianUint8(diffL)
		var devs []uint8
		for i := range diffL {
			devs = append(devs, cellColorDist(diffR[i], diffG[i], diffB[i], diffL[i], medR, medG, medB, medL))
		}
		mad := medianUint8(devs)
		if mad <= 6 {
			return doc, lidBackground{
				r:               medR,
				g:               medG,
				b:               medB,
				lum:             medL,
				gradNoise:       gradNoise,
				colorMad:        mad,
				distinctFromDoc: true,
			}
		}
	}

	if len(allL) == 0 {
		return doc, lidBackground{gradNoise: gradNoise}
	}

	medR := medianUint8(allR)
	medG := medianUint8(allG)
	medB := medianUint8(allB)
	medL := medianUint8(allL)
	dist := cellColorDist(medR, medG, medB, medL, medDocR, medDocG, medDocB, medDocL)
	return doc, lidBackground{
		r:               medR,
		g:               medG,
		b:               medB,
		lum:             medL,
		gradNoise:       gradNoise,
		distinctFromDoc: dist >= minDiffFromDoc,
	}
}

func cellColorDist(r1, g1, b1, l1, r2, g2, b2, l2 uint8) uint8 {
	dr := absInt(int(r1) - int(r2))
	dg := absInt(int(g1) - int(g2))
	db := absInt(int(b1) - int(b2))
	dl := absInt(int(l1) - int(l2))
	return uint8(maxInt(maxInt(dr, dg), maxInt(db, dl)))
}

func contentBounds(contentCluster []bool, lGrid, minLGrid, strokeGrid []uint8, gw, gh, bezelX, bezelY int, doc surfaceColor) (int, int, int, int, bool) {
	cMinX, cMaxX, cMinY, cMaxY := gw, -1, gh, -1
	for y := 0; y < gh; y++ {
		for x := 0; x < gw; x++ {
			if contentCluster[y*gw+x] {
				if x < cMinX {
					cMinX = x
				}
				if x > cMaxX {
					cMaxX = x
				}
				if y < cMinY {
					cMinY = y
				}
				if y > cMaxY {
					cMaxY = y
				}
			}
		}
	}
	if cMinX > cMaxX || cMinY > cMaxY {
		return 0, 0, 0, 0, false
	}

	// If content reaches up to the inner edge of a bezel strip and that outer strip is paper
	// (not a dark glass bezel reflection where >=50% of cells have lGrid < 180), extend the
	// content bounding box through any ink strokes inside the outer strip.
	isBezelDarkV := func(x0, x1 int) bool {
		total, dark := 0, 0
		for y := bezelY; y < gh-bezelY; y++ {
			for x := x0; x <= x1; x++ {
				total++
				if lGrid[y*gw+x] < 180 {
					dark++
				}
			}
		}
		return total > 0 && dark*100/total >= 50
	}
	isBezelDarkH := func(y0, y1 int) bool {
		total, dark := 0, 0
		for y := y0; y <= y1; y++ {
			for x := bezelX; x < gw-bezelX; x++ {
				total++
				if lGrid[y*gw+x] < 180 {
					dark++
				}
			}
		}
		return total > 0 && dark*100/total >= 50
	}

	inkThresh := minInt(165, int(doc.lum)-45)
	if !doc.valid || inkThresh < 130 {
		inkThresh = 165
	}

	if cMinX <= bezelX+2 && !isBezelDarkV(0, bezelX-1) {
		for x := cMinX - 1; x >= 0; x-- {
			hasInk := false
			for y := cMinY; y <= cMaxY; y++ {
				idx := y*gw + x
				if int(minLGrid[idx]) <= inkThresh && strokeGrid[idx] >= 12 {
					contentCluster[idx] = true
					hasInk = true
				}
			}
			if hasInk {
				cMinX = x
			}
		}
	}
	if cMaxX >= gw-bezelX-3 && !isBezelDarkV(gw-bezelX, gw-1) {
		for x := cMaxX + 1; x < gw; x++ {
			hasInk := false
			for y := cMinY; y <= cMaxY; y++ {
				idx := y*gw + x
				if int(minLGrid[idx]) <= inkThresh && strokeGrid[idx] >= 12 {
					contentCluster[idx] = true
					hasInk = true
				}
			}
			if hasInk {
				cMaxX = x
			}
		}
	}
	if cMinY <= bezelY+2 && !isBezelDarkH(0, bezelY-1) {
		for y := cMinY - 1; y >= 0; y-- {
			hasInk := false
			for x := cMinX; x <= cMaxX; x++ {
				idx := y*gw + x
				if int(minLGrid[idx]) <= inkThresh && strokeGrid[idx] >= 12 {
					contentCluster[idx] = true
					hasInk = true
				}
			}
			if hasInk {
				cMinY = y
			}
		}
	}
	if cMaxY >= gh-bezelY-3 && !isBezelDarkH(gh-bezelY, gh-1) {
		for y := cMaxY + 1; y < gh; y++ {
			hasInk := false
			for x := cMinX; x <= cMaxX; x++ {
				idx := y*gw + x
				if int(minLGrid[idx]) <= inkThresh && strokeGrid[idx] >= 12 {
					contentCluster[idx] = true
					hasInk = true
				}
			}
			if hasInk {
				cMaxY = y
			}
		}
	}

	return cMinX, cMaxX, cMinY, cMaxY, true
}

func findFaintHorizPaperSeamAfter(minLGrid, lGrid []uint8, gw, gh, minX, maxX, afterY int, doc surfaceColor) (int, bool) {
	if !doc.valid || doc.lum < 210 {
		return 0, false
	}
	spanW := maxX - minX + 1
	startY := maxInt(3, afterY+2)
	if spanW < 20 || startY >= gh-4 {
		return 0, false
	}
	gap := maxInt(8, spanW*15/100)
	minRun := maxInt(10, spanW*35/100)
	minActive := maxInt(12, spanW*3/100)
	rowBuf := make([]bool, spanW)

	for y := gh - 4; y >= startY; y-- {
		yNext := minInt(gh-1, y+1)
		yOuter := minInt(gh-2, y+5)
		activeCnt, inkCnt := 0, 0
		for x := minX; x <= maxX; x++ {
			mL := minUint8(minLGrid[y*gw+x], minLGrid[yNext*gw+x])
			dipDoc := int(doc.lum) - int(mL)
			dipOuter := int(lGrid[yOuter*gw+x]) - int(mL)
			if dipDoc >= 45 {
				inkCnt++
			}
			if dipDoc >= 3 && dipDoc <= 40 && dipOuter >= 2 {
				rowBuf[x-minX] = true
				activeCnt++
			} else {
				rowBuf[x-minX] = false
			}
		}
		if activeCnt < minActive || inkCnt > 2 {
			continue
		}
		longest := longestBridgedRun(rowBuf, gap)
		if longest >= minRun && isOuterBlankMinL(minLGrid, gw, gh, minX, maxX, y+2, gh-2, doc) {
			return y, true
		}
	}
	return 0, false
}

func findFaintHorizPaperSeamBefore(minLGrid []uint8, gw, gh, minX, maxX, beforeY int, doc surfaceColor) (int, bool) {
	if !doc.valid || doc.lum < 210 || beforeY < gh*8/100 {
		return 0, false
	}
	spanW := maxX - minX + 1
	endY := beforeY - 2
	if spanW < 20 || endY <= 3 {
		return 0, false
	}
	gap := maxInt(8, spanW*15/100)
	minRun := maxInt(10, spanW*35/100)
	minActive := maxInt(12, spanW*3/100)
	rowBuf := make([]bool, spanW)

	for y := 3; y <= endY; y++ {
		yPrev := maxInt(0, y-1)
		activeCnt, inkCnt := 0, 0
		for x := minX; x <= maxX; x++ {
			dip0 := int(doc.lum) - int(minLGrid[y*gw+x])
			dip1 := int(doc.lum) - int(minLGrid[yPrev*gw+x])
			dip := maxInt(dip0, dip1)
			if dip >= 45 {
				inkCnt++
			}
			if dip >= 3 && dip <= 40 {
				rowBuf[x-minX] = true
				activeCnt++
			} else {
				rowBuf[x-minX] = false
			}
		}
		if activeCnt < minActive || inkCnt > 2 {
			continue
		}
		longest := longestBridgedRun(rowBuf, gap)
		if longest >= minRun && isOuterBlankMinL(minLGrid, gw, gh, minX, maxX, 2, y-2, doc) {
			return y, true
		}
	}
	return 0, false
}

func findFaintVertPaperSeamAfter(minLGrid, lGrid []uint8, gw, gh, minY, maxY, afterX int, doc surfaceColor) (int, bool) {
	if !doc.valid || doc.lum < 210 {
		return 0, false
	}
	spanH := maxY - minY + 1
	startX := maxInt(3, afterX+2)
	if spanH < 20 || startX >= gw-4 {
		return 0, false
	}
	gap := maxInt(8, spanH*15/100)
	minRun := maxInt(10, spanH*35/100)
	minActive := maxInt(12, spanH*3/100)
	colBuf := make([]bool, spanH)

	for x := gw - 4; x >= startX; x-- {
		xNext := minInt(gw-1, x+1)
		xOuter := minInt(gw-2, x+5)
		activeCnt, inkCnt := 0, 0
		for y := minY; y <= maxY; y++ {
			mL := minUint8(minLGrid[y*gw+x], minLGrid[y*gw+xNext])
			dipDoc := int(doc.lum) - int(mL)
			dipOuter := int(lGrid[y*gw+xOuter]) - int(mL)
			if dipDoc >= 45 {
				inkCnt++
			}
			if dipDoc >= 3 && dipDoc <= 40 && dipOuter >= 2 {
				colBuf[y-minY] = true
				activeCnt++
			} else {
				colBuf[y-minY] = false
			}
		}
		if activeCnt < minActive || inkCnt > 2 {
			continue
		}
		longest := longestBridgedRun(colBuf, gap)
		if longest >= minRun && isOuterBlankMinL(minLGrid, gw, gh, x+2, gw-2, minY, maxY, doc) {
			return x, true
		}
	}
	return 0, false
}

func findFaintVertPaperSeamBefore(minLGrid []uint8, gw, gh, minY, maxY, beforeX int, doc surfaceColor) (int, bool) {
	if !doc.valid || doc.lum < 210 || beforeX < gw*8/100 {
		return 0, false
	}
	spanH := maxY - minY + 1
	endX := beforeX - 2
	if spanH < 20 || endX <= 3 {
		return 0, false
	}
	gap := maxInt(8, spanH*15/100)
	minRun := maxInt(10, spanH*35/100)
	minActive := maxInt(12, spanH*3/100)
	colBuf := make([]bool, spanH)

	for x := 3; x <= endX; x++ {
		xPrev := maxInt(0, x-1)
		activeCnt, inkCnt := 0, 0
		for y := minY; y <= maxY; y++ {
			dip0 := int(doc.lum) - int(minLGrid[y*gw+x])
			dip1 := int(doc.lum) - int(minLGrid[y*gw+xPrev])
			dip := maxInt(dip0, dip1)
			if dip >= 45 {
				inkCnt++
			}
			if dip >= 3 && dip <= 40 {
				colBuf[y-minY] = true
				activeCnt++
			} else {
				colBuf[y-minY] = false
			}
		}
		if activeCnt < minActive || inkCnt > 2 {
			continue
		}
		longest := longestBridgedRun(colBuf, gap)
		if longest >= minRun && isOuterBlankMinL(minLGrid, gw, gh, 2, x-2, minY, maxY, doc) {
			return x, true
		}
	}
	return 0, false
}

func longestBridgedRun(active []bool, maxGap int) int {
	n := len(active)
	longest := 0
	i := 0
	for i < n {
		if !active[i] {
			i++
			continue
		}
		start := i
		lastActive := i
		j := i + 1
		for j < n {
			if active[j] {
				lastActive = j
				j++
			} else if j-lastActive <= maxGap {
				j++
			} else {
				break
			}
		}
		if run := lastActive - start + 1; run > longest {
			longest = run
		}
		i = lastActive + 1
	}
	return longest
}

func findCardBottomBoundaryY(vertEdge []bool, minLGrid []uint8, gw, gh, bezelX, bezelY, minX, maxX, minY, maxY int, hasVertEdgeMaxX bool, doc surfaceColor) (int, bool) {
	// 1. If a vertical right paper edge (vertEdge) was found inside the bed, its bottom endpoint
	// marks the physical bottom-right corner of the card/document.
	if hasVertEdgeMaxX && maxX < gw-bezelX-2 {
		x0 := maxInt(bezelX, maxX-3)
		x1 := minInt(gw-bezelX-1, maxX+2)
		activeRows := 0
		lastY := -1
		for y := bezelY; y < gh-bezelY; y++ {
			rowHas := false
			for x := x0; x <= x1; x++ {
				if vertEdge[y*gw+x] {
					rowHas = true
					break
				}
			}
			if rowHas {
				activeRows++
				lastY = y
			}
		}
		if activeRows >= gh*25/100 && lastY >= maxY && lastY < gh-bezelY-8 &&
			isOuterBlankMinL(minLGrid, gw, gh, minX, maxX, lastY+3, gh-bezelY-1, doc) {
			return lastY, true
		}
	}

	// 2. If the document sits against the left scanner guide (minX == 0), check where the left bezel
	// strip (x = 0..4) transitions from bright paper (minL >= 170) to dark uncovered guide (minL <= 80).
	if minX == 0 && doc.valid && doc.lum >= 200 {
		bx := minInt(bezelX-1, 4)
		if bx >= 1 {
			rowMinL := make([]uint8, gh)
			for y := 0; y < gh; y++ {
				m := uint8(255)
				for x := 0; x <= bx; x++ {
					if v := minLGrid[y*gw+x]; v < m {
						m = v
					}
				}
				rowMinL[y] = m
			}
			minEndY := maxInt(maxY, bezelY+gh*25/100)
			maxEndY := gh - bezelY - maxInt(8, gh*8/100)
			for endY := maxEndY; endY >= minEndY; endY-- {
				if rowMinL[endY] < 170 || rowMinL[maxInt(0, endY-1)] < 170 || rowMinL[minInt(gh-1, endY+3)] > 80 {
					continue
				}
				localBright := 0
				for y := maxInt(0, endY-8); y <= endY; y++ {
					if rowMinL[y] >= 170 {
						localBright++
					}
				}
				if localBright < 7 {
					continue
				}
				aboveTotal, aboveBright := 0, 0
				for y := bezelY; y <= endY; y++ {
					aboveTotal++
					if rowMinL[y] >= 170 {
						aboveBright++
					}
				}
				belowTotal, belowDark := 0, 0
				for y := endY + 3; y < gh-bezelY; y++ {
					belowTotal++
					if rowMinL[y] <= 80 {
						belowDark++
					}
				}
				if aboveTotal >= gh*25/100 && aboveBright*100/aboveTotal >= 85 &&
					belowTotal >= gh*8/100 && belowDark*100/belowTotal >= 80 &&
					isOuterBlankMinL(minLGrid, gw, gh, minX, maxX, endY+3, gh-bezelY-1, doc) {
					return endY, true
				}
			}
		}
	}

	return 0, false
}

func isOuterBlankMinL(minLGrid []uint8, gw, gh, x0, x1, y0, y1 int, doc surfaceColor) bool {
	bx := maxInt(2, gw*18/1000)
	by := maxInt(2, gh*18/1000)
	x0 = maxInt(bx, x0)
	x1 = minInt(gw-bx-1, x1)
	y0 = maxInt(by, y0)
	y1 = minInt(gh-by-1, y1)
	if x0 > x1 || y0 > y1 {
		return false
	}
	total, clean, ink := 0, 0, 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			total++
			dip := int(doc.lum) - int(minLGrid[y*gw+x])
			if dip <= 4 {
				clean++
			} else if dip >= 35 {
				ink++
			}
		}
	}
	maxDustInk := maxInt(8, total/1000)
	return total > 0 && ink <= maxDustInk && clean*100/total >= 97
}

func detectContentClusters(gxGrid, gyGrid, strokeGrid, minLGrid []uint8, vertEdge, horizEdge []bool, gw, gh, bezelX, bezelY int, edgeThresh uint8, params aggressionParams) []bool {
	result := make([]bool, gw*gh)
	active := make([]bool, gw*gh)

	strokeThresh := maxUint8(edgeThresh+3, 12)
	for y := bezelY; y < gh-bezelY; y++ {
		for x := bezelX; x < gw-bezelX; x++ {
			idx := y*gw + x
			// Pure 1D paper-edge shadow seams (with light shadow minL >= 160) are boundary seams,
			// not 2D interior printed/handwritten content.
			if minLGrid[idx] >= 160 && (vertEdge[idx] || horizEdge[idx]) {
				continue
			}
			if gxGrid[idx] >= edgeThresh || gyGrid[idx] >= edgeThresh || strokeGrid[idx] >= strokeThresh {
				active[idx] = true
			}
		}
	}

	visited := make([]bool, gw*gh)
	bridgeR := maxInt(2, minInt(gw, gh)*10/1000) // ~1% 2D gap bridge (connects letters/words in a line)
	minSpanX := maxInt(7, gw*params.minClusterSpanProm/1000)
	minSpanY := maxInt(7, gh*params.minClusterSpanProm/1000)

	queue := make([]int, 0, 256)
	component := make([]int, 0, 256)

	for y := bezelY; y < gh-bezelY; y++ {
		for x := bezelX; x < gw-bezelX; x++ {
			startIdx := y*gw + x
			if !active[startIdx] || visited[startIdx] {
				continue
			}

			visited[startIdx] = true
			queue = queue[:0]
			component = component[:0]
			queue = append(queue, startIdx)

			minCX, maxCX := x, x
			minCY, maxCY := y, y

			for len(queue) > 0 {
				curr := queue[len(queue)-1]
				queue = queue[:len(queue)-1]
				component = append(component, curr)

				cy := curr / gw
				cx := curr % gw
				if cx < minCX {
					minCX = cx
				}
				if cx > maxCX {
					maxCX = cx
				}
				if cy < minCY {
					minCY = cy
				}
				if cy > maxCY {
					maxCY = cy
				}

				ny0 := maxInt(bezelY, cy-bridgeR)
				ny1 := minInt(gh-bezelY-1, cy+bridgeR)
				nx0 := maxInt(bezelX, cx-bridgeR)
				nx1 := minInt(gw-bezelX-1, cx+bridgeR)

				for ny := ny0; ny <= ny1; ny++ {
					rowOff := ny * gw
					for nx := nx0; nx <= nx1; nx++ {
						nIdx := rowOff + nx
						if active[nIdx] && !visited[nIdx] {
							visited[nIdx] = true
							queue = append(queue, nIdx)
						}
					}
				}
			}

			spanX := maxCX - minCX + 1
			spanY := maxCY - minCY + 1
			if (spanX >= minSpanX || spanY >= minSpanY) && len(component) >= params.minClusterCells {
				for _, idx := range component {
					result[idx] = true
				}
			}
		}
	}

	return result
}

func markDocumentSurface(rGrid, gGrid, bGrid, lGrid []uint8, verified []bool, gw, gh, bezelX, bezelY int, doc surfaceColor, lid lidBackground, params aggressionParams) {
	if !doc.valid || !lid.distinctFromDoc {
		return
	}

	docBgDist := int(cellColorDist(doc.r, doc.g, doc.b, doc.lum, lid.r, lid.g, lid.b, lid.lum))
	minDist := maxInt(6, int(lid.colorMad)*2+4)
	if docBgDist < minDist {
		return
	}

	threshDist := uint8(maxInt(minDist, docBgDist*(100-params.surfaceRatioPct)/100))

	surfaceCell := make([]bool, gw*gh)
	for y := bezelY; y < gh-bezelY; y++ {
		for x := bezelX; x < gw-bezelX; x++ {
			idx := y*gw + x
			distBg := cellColorDist(rGrid[idx], gGrid[idx], bGrid[idx], lGrid[idx], lid.r, lid.g, lid.b, lid.lum)
			distDoc := cellColorDist(rGrid[idx], gGrid[idx], bGrid[idx], lGrid[idx], doc.r, doc.g, doc.b, doc.lum)
			if (distBg >= threshDist && distDoc < distBg) || verified[idx] {
				surfaceCell[idx] = true
			}
		}
	}

	minSurfRunX := maxInt(10, gw*8/100)
	minSurfRunY := maxInt(10, gh*8/100)
	surfRow := make([]bool, gw*gh)
	rowBuf := make([]bool, gw)
	for y := bezelY; y < gh-bezelY; y++ {
		for x := 0; x < gw; x++ {
			rowBuf[x] = surfaceCell[y*gw+x]
		}
		markBridgedRuns(rowBuf, gapForSurface(gw), minSurfRunX, func(x int) {
			surfRow[y*gw+x] = true
		})
	}

	colBuf := make([]bool, gh)
	for x := bezelX; x < gw-bezelX; x++ {
		for y := 0; y < gh; y++ {
			colBuf[y] = surfRow[y*gw+x]
		}
		markBridgedRuns(colBuf, gapForSurface(gh), minSurfRunY, func(y int) {
			verified[y*gw+x] = true
		})
	}
}

func trimSkewedLidWedges(rGrid, gGrid, bGrid, lGrid []uint8, gw, gh, minX, maxX, minY, maxY int, doc surfaceColor, lid lidBackground, params aggressionParams) (int, int, int, int) {
	// At 70%..100% aggression, require outer rows/columns to have at least minPaperPct%
	// non-lid cells so tilted scanner-base wedges (like a 1.5° skewed edge) are trimmed cleanly.
	minPaperPct := 25 + (params.pct-70)*2 // 25% at 70%, 45% at 80%, 65% at 90%, 85% at 100%
	maxTrimX := maxInt(4, (maxX-minX)*8/100)
	maxTrimY := maxInt(4, (maxY-minY)*8/100)

	isLidCell := func(x, y int) bool {
		idx := y*gw + x
		distBg := cellColorDist(rGrid[idx], gGrid[idx], bGrid[idx], lGrid[idx], lid.r, lid.g, lid.b, lid.lum)
		distDoc := cellColorDist(rGrid[idx], gGrid[idx], bGrid[idx], lGrid[idx], doc.r, doc.g, doc.b, doc.lum)
		return distBg < distDoc
	}

	for step := 0; step < maxTrimY && maxY > minY+10; step++ {
		total, paper := 0, 0
		for x := minX; x <= maxX; x++ {
			total++
			if !isLidCell(x, maxY) {
				paper++
			}
		}
		if total > 0 && paper*100/total < minPaperPct {
			maxY--
		} else {
			break
		}
	}

	for step := 0; step < maxTrimX && maxX > minX+10; step++ {
		total, paper := 0, 0
		for y := minY; y <= maxY; y++ {
			total++
			if !isLidCell(maxX, y) {
				paper++
			}
		}
		if total > 0 && paper*100/total < minPaperPct {
			maxX--
		} else {
			break
		}
	}

	return minX, maxX, minY, maxY
}

func isStripPaperSurface(rGrid, gGrid, bGrid, lGrid []uint8, gw, gh, x0, x1, y0, y1 int, doc surfaceColor) bool {
	if !doc.valid {
		return false
	}
	x0 = maxInt(0, x0)
	x1 = minInt(gw-1, x1)
	y0 = maxInt(0, y0)
	y1 = minInt(gh-1, y1)
	if x0 > x1 || y0 > y1 {
		return false
	}
	maxAllowed := uint8(maxInt(10, int(doc.mad)*3+8))
	total, matching := 0, 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			idx := y*gw + x
			total++
			if cellColorDist(rGrid[idx], gGrid[idx], bGrid[idx], lGrid[idx], doc.r, doc.g, doc.b, doc.lum) <= maxAllowed {
				matching++
			}
		}
	}
	return total > 0 && matching*100/total >= 70
}

func gapForSurface(n int) int {
	return maxInt(3, n*2/100)
}

func isStripLidBackground(rGrid, gGrid, bGrid, lGrid []uint8, gw, gh, x0, x1, y0, y1 int, lid lidBackground, params aggressionParams) bool {
	if !lid.distinctFromDoc {
		return false
	}
	x0 = maxInt(0, x0)
	x1 = minInt(gw-1, x1)
	y0 = maxInt(0, y0)
	y1 = minInt(gh-1, y1)
	if x0 > x1 || y0 > y1 {
		return false
	}

	maxAllowed := uint8(maxInt(params.surfaceColorThresh, int(lid.colorMad)*3+8))
	total, matching := 0, 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			idx := y*gw + x
			total++
			if cellColorDist(rGrid[idx], gGrid[idx], bGrid[idx], lGrid[idx], lid.r, lid.g, lid.b, lid.lum) <= maxAllowed {
				matching++
			}
		}
	}
	return total > 0 && matching*100/total >= 75
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

func hasPureEdgeLineH(horizEdge []bool, gw, gh, y, minX, maxX, minCount int) bool {
	count := 0
	y0 := maxInt(0, y-2)
	y1 := minInt(gh-1, y+2)
	for x := minX; x <= maxX; x++ {
		for yy := y0; yy <= y1; yy++ {
			if horizEdge[yy*gw+x] {
				count++
				break
			}
		}
	}
	return count >= minCount
}

func hasPureEdgeLineV(vertEdge []bool, gw, gh, x, minY, maxY, minCount int) bool {
	count := 0
	x0 := maxInt(0, x-2)
	x1 := minInt(gw-1, x+2)
	for y := minY; y <= maxY; y++ {
		for xx := x0; xx <= x1; xx++ {
			if vertEdge[y*gw+xx] {
				count++
				break
			}
		}
	}
	return count >= minCount
}

func findPureEdgeLineVNearMax(vertEdge []bool, gw, gh, x0, x1, minY, maxY, minCount int) (int, bool) {
	for x := x1; x >= x0; x-- {
		if hasPureEdgeLineV(vertEdge, gw, gh, x, minY, maxY, minCount) {
			return x, true
		}
	}
	return 0, false
}

func findPureEdgeLineVNearMin(vertEdge []bool, gw, gh, x0, x1, minY, maxY, minCount int) (int, bool) {
	for x := x0; x <= x1; x++ {
		if hasPureEdgeLineV(vertEdge, gw, gh, x, minY, maxY, minCount) {
			return x, true
		}
	}
	return 0, false
}

func findPureEdgeLineHNearMax(horizEdge []bool, gw, gh, y0, y1, minX, maxX, minCount int) (int, bool) {
	for y := y1; y >= y0; y-- {
		if hasPureEdgeLineH(horizEdge, gw, gh, y, minX, maxX, minCount) {
			return y, true
		}
	}
	return 0, false
}

func findPureEdgeLineHNearMin(horizEdge []bool, gw, gh, y0, y1, minX, maxX, minCount int) (int, bool) {
	for y := y0; y <= y1; y++ {
		if hasPureEdgeLineH(horizEdge, gw, gh, y, minX, maxX, minCount) {
			return y, true
		}
	}
	return 0, false
}

func minUint8(a, b uint8) uint8 {
	if a < b {
		return a
	}
	return b
}

func hasBoundaryLineH(horizEdge, verified []bool, gw, gh, y, minX, maxX, minCount int) bool {
	if hasPureEdgeLineH(horizEdge, gw, gh, y, minX, maxX, minCount) {
		return true
	}
	surfCount := 0
	y0 := maxInt(0, y-1)
	y1 := minInt(gh-1, y+2)
	for x := minX; x <= maxX; x++ {
		for yy := y0; yy <= y1; yy++ {
			if verified[yy*gw+x] {
				surfCount++
				break
			}
		}
	}
	return surfCount >= (maxX-minX+1)*75/100
}

func hasBoundaryLineV(vertEdge, verified []bool, gw, gh, x, minY, maxY, minCount int) bool {
	if hasPureEdgeLineV(vertEdge, gw, gh, x, minY, maxY, minCount) {
		return true
	}
	surfCount := 0
	x0 := maxInt(0, x-1)
	x1 := minInt(gw-1, x+2)
	for y := minY; y <= maxY; y++ {
		for xx := x0; xx <= x1; xx++ {
			if verified[y*gw+xx] {
				surfCount++
				break
			}
		}
	}
	return surfCount >= (maxY-minY+1)*75/100
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

// ApplyCropMargin adds the default safety padding margin around detected crop bounds.
func ApplyCropMargin(bounds, crop image.Rectangle, resolution int) image.Rectangle {
	return ApplyCropMarginWithAggression(bounds, crop, resolution, CropDefault)
}

// ApplyCropMarginWithAggression adjusts the crop boundary according to the aggression percentage.
func ApplyCropMarginWithAggression(bounds, crop image.Rectangle, resolution int, aggression CropAggression) image.Rectangle {
	if ParseCropAggression(string(aggression)) == CropOff {
		return bounds
	}
	if crop.Dx() >= bounds.Dx() && crop.Dy() >= bounds.Dy() {
		return bounds
	}

	params := paramsForAggression(aggression)
	var margin int
	if resolution > 0 {
		margin = resolution * params.marginDPIHundredth / 100
		if params.marginDPIHundredth > 0 && margin < params.minMarginPx {
			margin = params.minMarginPx
		} else if params.marginDPIHundredth < 0 && margin > params.minMarginPx {
			margin = params.minMarginPx
		}
	} else {
		margin = maxInt(bounds.Dx(), bounds.Dy()) * params.marginDPIHundredth / 1000
		if params.marginDPIHundredth > 0 && margin < params.minMarginPx {
			margin = params.minMarginPx
		} else if params.marginDPIHundredth < 0 && margin > params.minMarginPx {
			margin = params.minMarginPx
		}
	}

	minX := crop.Min.X
	if minX > bounds.Min.X {
		minX = maxInt(bounds.Min.X, minX-margin)
	}
	minY := crop.Min.Y
	if minY > bounds.Min.Y {
		minY = maxInt(bounds.Min.Y, minY-margin)
	}
	maxX := crop.Max.X
	if maxX < bounds.Max.X {
		maxX = minInt(bounds.Max.X, maxX+margin)
	}
	maxY := crop.Max.Y
	if maxY < bounds.Max.Y {
		maxY = minInt(bounds.Max.Y, maxY+margin)
	}

	if maxX <= minX+10 {
		maxX = minInt(bounds.Max.X, minX+10)
	}
	if maxY <= minY+10 {
		maxY = minInt(bounds.Max.Y, minY+10)
	}

	padded := image.Rect(minX, minY, maxX, maxY)
	if padded.Dx() >= bounds.Dx() && padded.Dy() >= bounds.Dy() {
		return bounds
	}
	return padded
}

// AutoCropFile detects the content boundaries and crops the file using default aggression.
func (c *Client) AutoCropFile(ctx context.Context, filePath string, resolution int) error {
	return c.AutoCropFileWithAggression(ctx, filePath, resolution, CropDefault)
}

// AutoCropFileWithAggression detects the content boundaries and crops the file using the specified aggression.
func (c *Client) AutoCropFileWithAggression(ctx context.Context, filePath string, resolution int, aggression CropAggression) error {
	agg := ParseCropAggression(string(aggression))
	if agg == CropOff {
		return nil
	}

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

		crop := CalculateCropBoundsWithAggression(img, agg)
		crop = ApplyCropMarginWithAggression(img.Bounds(), crop, resolution, agg)
		if crop.Dx() >= img.Bounds().Dx() && crop.Dy() >= img.Bounds().Dy() {
			return nil
		}

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

		crop := CalculateCropBoundsWithAggression(img, agg)
		crop = ApplyCropMarginWithAggression(img.Bounds(), crop, 72, agg)
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
