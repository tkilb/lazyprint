package main

import (
	"fmt"
	"image"
	_ "image/png"
	"os"

	"github.com/tkilb/lazyprint/internal/sane"
)

func main() {
	f, err := os.Open("/home/tylerkilburn/Scans/scan_20261111_142914.png")
	if err != nil {
		fmt.Printf("err: %v\n", err)
		return
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		fmt.Printf("decode err: %v\n", err)
		return
	}

	bounds := img.Bounds()
	crop := sane.CalculateCropBounds(img)
	fmt.Printf("Image bounds: %v\n", bounds)
	fmt.Printf("Recalculate crop on already-cropped: %v (dx=%d, dy=%d)\n", crop, crop.Dx(), crop.Dy())
}
