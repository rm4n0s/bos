// Package mask generates region masks from flat-colour reference images
// (SPEC §7.3). Regions are painted in solid colours on a white background; the
// package produces one binary mask per colour and one for the background.
// Automatic person segmentation is out of scope (SPEC §14.4, D5).
package mask

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/rm4n0s/errors"
)

// MaxColours is the number of distinct non-white colours an image may have.
const MaxColours = 10

// Background is the keyword of the mask of the white pixels.
const Background = "background"

// Mask is a binary mask of one region: 255 where the region is painted.
type Mask struct {
	Keyword string
	Image   *image.Gray
}

var colourNames = map[[3]uint8]string{
	{255, 0, 0}: "red", {0, 255, 0}: "green", {0, 0, 255}: "blue", {255, 255, 0}: "yellow",
	{255, 0, 255}: "magenta", {0, 255, 255}: "cyan", {128, 0, 0}: "maroon", {0, 128, 0}: "dark_green",
	{0, 0, 128}: "navy", {128, 128, 0}: "olive", {255, 165, 0}: "orange", {128, 0, 128}: "purple",
	{255, 192, 203}: "pink", {165, 42, 42}: "brown", {128, 128, 128}: "gray",
}

// Name returns the keyword of a colour: a colour name, or `color_R_G_B`.
func Name(r, g, b uint8) string {
	if n, ok := colourNames[[3]uint8{r, g, b}]; ok {
		return n
	}
	return fmt.Sprintf("color_%d_%d_%d", r, g, b)
}

func failed(reason string, cause error) *errors.Error {
	if cause != nil {
		return errors.NewErr("MaskGenerationFailed", cause).SetMetadata("reason", reason)
	}
	return errors.New("MaskGenerationFailed", "cannot generate masks: "+reason, "reason", reason)
}

// Generate reads a PNG and returns a mask for every distinct non-white colour,
// in the order the colours first appear (rows from the top), followed by the
// background mask. Lossy formats are rejected because they introduce stray
// colours. Transparent pixels count as white.
func Generate(r io.Reader) ([]Mask, error) {
	img, err := decodePNG(r)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if b.Empty() {
		return nil, failed("the image is empty", nil)
	}

	var order [][3]uint8
	seen := map[[3]uint8]bool{}
	pixels := make([][3]uint8, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := rgb(img.At(x, y))
			pixels = append(pixels, p)
			if p == [3]uint8{255, 255, 255} || seen[p] {
				continue
			}
			if len(order) == MaxColours {
				return nil, failed("the image has more than "+strconv.Itoa(MaxColours)+
					" colours besides white; use flat colours without anti-aliasing", nil)
			}
			seen[p] = true
			order = append(order, p)
		}
	}
	if len(order) == 0 {
		return nil, failed("the image has no colour besides white", nil)
	}

	masks := make([]Mask, 0, len(order)+1)
	for _, c := range order {
		masks = append(masks, Mask{Keyword: Name(c[0], c[1], c[2]), Image: paint(b, pixels, func(p [3]uint8) bool { return p == c })})
	}
	masks = append(masks, Mask{Keyword: Background, Image: paint(b, pixels, func(p [3]uint8) bool { return p == [3]uint8{255, 255, 255} })})
	return masks, nil
}

// pngSignature starts every PNG file.
const pngSignature = "\x89PNG\r\n\x1a\n"

// maxImageBytes bounds how much of the reader is read.
const maxImageBytes = 64 << 20

func decodePNG(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxImageBytes))
	if err != nil {
		return nil, failed("the image cannot be read", err)
	}
	if !bytes.HasPrefix(data, []byte(pngSignature)) {
		return nil, failed("the image is not a PNG; masks need a lossless PNG with flat colours", nil)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, failed("the PNG cannot be decoded", err)
	}
	return img, nil
}

// rgb converts a pixel to 8-bit RGB, compositing transparency over white.
func rgb(c color.Color) [3]uint8 {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	if n.A == 255 {
		return [3]uint8{n.R, n.G, n.B}
	}
	a := uint32(n.A)
	mix := func(v uint8) uint8 { return uint8((uint32(v)*a + 255*(255-a) + 127) / 255) }
	return [3]uint8{mix(n.R), mix(n.G), mix(n.B)}
}

func paint(b image.Rectangle, pixels [][3]uint8, in func([3]uint8) bool) *image.Gray {
	out := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for i, p := range pixels {
		if in(p) {
			out.Pix[i] = 255
		}
	}
	return out
}

// Save writes the masks to dir as `mask_<keyword>.png` and returns the path of
// each by keyword. The directory is created when missing.
func Save(dir string, masks []Mask) (map[string]string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, errors.NewErr("FileStoreFailure", err).SetMetadata("path", dir, "operation", "mkdir")
	}
	paths := make(map[string]string, len(masks))
	for _, m := range masks {
		p := filepath.Join(dir, "mask_"+m.Keyword+".png")
		if err := writePNG(p, m.Image); err != nil {
			return nil, err
		}
		paths[m.Keyword] = p
	}
	return paths, nil
}

func writePNG(path string, img image.Image) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return errors.NewErr("FileStoreFailure", err).SetMetadata("path", path, "operation", "create")
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = errors.NewErr("FileStoreFailure", closeErr).SetMetadata("path", path, "operation", "close")
		}
	}()
	if encErr := png.Encode(f, img); encErr != nil {
		return errors.NewErr("FileStoreFailure", encErr).SetMetadata("path", path, "operation", "write")
	}
	return nil
}
