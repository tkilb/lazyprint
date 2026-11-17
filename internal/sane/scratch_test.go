package sane

import (
	"fmt"
	"image/png"
	"os"
	"testing"
)

func TestAnalyzeRealScan(t *testing.T) {
	f, err := os.Open("/home/tylerkilburn/Scans/scan_20261111_144032.png")
	if err != nil {
		t.Skip("no file")
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	fmt.Printf("Image bounds: %v\n", b)
	cropped := CalculateCropBounds(img)
	fmt.Printf("CalculateCropBounds -> bounds=%v\n", cropped)
}
