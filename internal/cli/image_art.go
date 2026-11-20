package cli

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"
)

const (
	avatarWidth      = 28 // 28 terminal columns -> 56 Braille horizontal dots
	avatarRows       = 12 // 12 terminal rows -> 48 Braille vertical dots
	avatarSubW       = avatarWidth * 2
	avatarSubH       = avatarRows * 4
	avatarBgTol      = 42.0
	avatarAlphaClamp = 128
)

var brailleBits = [4][2]rune{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// ImageFileToANSI reads a PNG, JPEG, or GIF file and converts it into 12 rows
// of 28-column Unicode Braille ANSI art (56x48 subpixel resolution).
func ImageFileToANSI(path string) ([]string, error) {
	cleaned := strings.TrimSpace(path)
	cleaned = strings.Trim(cleaned, `"'`)
	f, err := os.Open(cleaned)
	if err != nil {
		return nil, fmt.Errorf("open image: %w", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode image (supported: png, jpg, gif): %w", err)
	}
	return ImageToANSI(img), nil
}

// ImageBytesToANSI decodes raw PNG, JPEG, or GIF bytes and converts them to Braille ANSI rows.
func ImageBytesToANSI(data []byte) ([]string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image bytes: %w", err)
	}
	return ImageToANSI(img), nil
}

// ImageToANSI converts an arbitrary image.Image into 12 rows of 28-column
// Unicode Braille ANSI art (56x48 subpixels), trimming outer margins first.
func ImageToANSI(src image.Image) []string {
	cropped := cropToContent(src)
	rgba := resizeBoxAverage(cropped, avatarSubW, avatarSubH)
	bg := floodFillCornerBackground(rgba)

	lineArt := isDarkOnLightLineArt(rgba)

	rows := make([]string, avatarRows)
	for r := 0; r < avatarRows; r++ {
		var sb strings.Builder
		yBase := r * 4

		for c := 0; c < avatarWidth; c++ {
			xBase := c * 2
			var mask rune
			var sumR, sumG, sumB, count int

			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					px := xBase + dx
					py := yBase + dy
					pix := rgba.RGBAAt(px, py)
					if pix.A < avatarAlphaClamp {
						continue
					}

					lit := false
					var dotR, dotG, dotB int
					if lineArt {
						// On white-background icons, light up dark ink strokes AND colored accents,
						// leaving white fill/background transparent.
						luma := 0.299*float64(pix.R) + 0.587*float64(pix.G) + 0.114*float64(pix.B)
						maxC := max(pix.R, max(pix.G, pix.B))
						minC := min(pix.R, min(pix.G, pix.B))
						chroma := int(maxC) - int(minC)
						if chroma > 35 {
							lit = true
							dotR, dotG, dotB = int(pix.R), int(pix.G), int(pix.B)
						} else if luma < 175 {
							lit = true
							dotR, dotG, dotB = 243, 232, 255 // Crisp lavender-white on dark terminals
						}
					} else {
						lit = !bg[py*avatarSubW+px]
						dotR, dotG, dotB = int(pix.R), int(pix.G), int(pix.B)
					}

					if lit {
						mask |= brailleBits[dy][dx]
						sumR += dotR
						sumG += dotG
						sumB += dotB
						count++
					}
				}
			}

			if mask == 0 || count == 0 {
				sb.WriteByte(' ')
				continue
			}
			avgR := sumR / count
			avgG := sumG / count
			avgB := sumB / count
			sb.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm%c\x1b[0m", avgR, avgG, avgB, 0x2800+mask))
		}
		rows[r] = sb.String()
	}
	return rows
}

func isDarkOnLightLineArt(img *image.RGBA) bool {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	if w == 0 || h == 0 {
		return false
	}
	ref := img.RGBAAt(0, 0)
	refLuma := 0.299*float64(ref.R) + 0.587*float64(ref.G) + 0.114*float64(ref.B)
	if ref.A < avatarAlphaClamp || refLuma < 200 {
		return false
	}

	var total, dark, white int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			if c.A < avatarAlphaClamp {
				continue
			}
			total++
			luma := 0.299*float64(c.R) + 0.587*float64(c.G) + 0.114*float64(c.B)
			if luma < 120 {
				dark++
			} else if luma > 215 {
				white++
			}
		}
	}
	if total == 0 {
		return false
	}
	return dark > total/20 && white > total/3
}

func cropToContent(src image.Image) image.Image {
	b := src.Bounds()
	if b.Dx() <= 2 || b.Dy() <= 2 {
		return src
	}
	c0 := color.NRGBAModel.Convert(src.At(b.Min.X, b.Min.Y)).(color.NRGBA)
	ref := color.RGBA{R: c0.R, G: c0.G, B: c0.B, A: c0.A}

	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	found := false

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cn := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
			c := color.RGBA{R: cn.R, G: cn.G, B: cn.B, A: cn.A}
			if c.A >= avatarAlphaClamp && (ref.A < avatarAlphaClamp || colorDist(c, ref) > avatarBgTol) {
				found = true
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
	if !found || maxX <= minX || maxY <= minY {
		return src
	}
	if minX > b.Min.X {
		minX--
	}
	if minY > b.Min.Y {
		minY--
	}
	if maxX+1 < b.Max.X {
		maxX++
	}
	if maxY+1 < b.Max.Y {
		maxY++
	}
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := src.(subImager); ok {
		return si.SubImage(image.Rect(minX, minY, maxX+1, maxY+1))
	}
	return src
}

func resizeBoxAverage(src image.Image, dstW, dstH int) *image.RGBA {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	if srcW == 0 || srcH == 0 {
		return dst
	}

	for y := 0; y < dstH; y++ {
		y0 := bounds.Min.Y + (y*srcH)/dstH
		y1 := bounds.Min.Y + ((y+1)*srcH)/dstH
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dstW; x++ {
			x0 := bounds.Min.X + (x*srcW)/dstW
			x1 := bounds.Min.X + ((x+1)*srcW)/dstW
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sumR, sumG, sumB, sumA, count uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
					sumR += uint64(c.R)
					sumG += uint64(c.G)
					sumB += uint64(c.B)
					sumA += uint64(c.A)
					count++
				}
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(sumR / count),
				G: uint8(sumG / count),
				B: uint8(sumB / count),
				A: uint8(sumA / count),
			})
		}
	}
	return dst
}

func floodFillCornerBackground(img *image.RGBA) []bool {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	bg := make([]bool, w*h)
	visited := make([]bool, w*h)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if img.RGBAAt(x, y).A < avatarAlphaClamp {
				bg[y*w+x] = true
			}
		}
	}

	ref := img.RGBAAt(0, 0)
	if ref.A < avatarAlphaClamp {
		return bg
	}

	type pt struct{ x, y int }
	queue := []pt{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}}
	for _, p := range queue {
		visited[p.y*w+p.x] = true
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		idx := cur.y*w + cur.x
		c := img.RGBAAt(cur.x, cur.y)
		if c.A >= avatarAlphaClamp && colorDist(c, ref) > avatarBgTol {
			continue
		}
		bg[idx] = true

		neighbors := [4]pt{
			{cur.x - 1, cur.y},
			{cur.x + 1, cur.y},
			{cur.x, cur.y - 1},
			{cur.x, cur.y + 1},
		}
		for _, n := range neighbors {
			if n.x < 0 || n.x >= w || n.y < 0 || n.y >= h {
				continue
			}
			nIdx := n.y*w + n.x
			if !visited[nIdx] {
				visited[nIdx] = true
				queue = append(queue, n)
			}
		}
	}

	return bg
}

func colorDist(a, b color.RGBA) float64 {
	dr := float64(a.R) - float64(b.R)
	dg := float64(a.G) - float64(b.G)
	db := float64(a.B) - float64(b.B)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}
