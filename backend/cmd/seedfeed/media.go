// media.go 本地生成图片与视频封面（PNG），无需外部素材与 ffmpeg 依赖。
// 每张图按索引与随机源生成不同的配色、光斑与编号，便于在客户端上肉眼区分。
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
)

// cardKind 决定图片上叠加的角标类型。
type cardKind int

const (
	kindVideo cardKind = iota // 视频封面：叠加播放角标
	kindImage                 // 图文配图：叠加多图角标
)

const (
	cardW = 540
	cardH = 720
)

// palettes 是渐变配色池，取值偏明快，接近短视频封面观感。
var palettes = [][2]color.NRGBA{
	{{255, 94, 98, 255}, {255, 195, 113, 255}},
	{{54, 209, 220, 255}, {91, 134, 229, 255}},
	{{255, 175, 189, 255}, {255, 195, 160, 255}},
	{{67, 206, 162, 255}, {24, 90, 157, 255}},
	{{247, 151, 30, 255}, {255, 210, 0, 255}},
	{{168, 192, 255, 255}, {63, 43, 150, 255}},
	{{255, 126, 179, 255}, {255, 117, 140, 255}},
	{{15, 32, 39, 255}, {44, 83, 100, 255}},
	{{238, 156, 167, 255}, {255, 221, 225, 255}},
	{{86, 204, 242, 255}, {47, 128, 237, 255}},
	{{252, 227, 138, 255}, {243, 129, 129, 255}},
	{{78, 205, 196, 255}, {85, 98, 112, 255}},
}

// renderCard 生成一张 PNG 卡片图。
func renderCard(idx int, rnd *rand.Rand, kind cardKind) ([]byte, error) {
	p := palettes[rnd.Intn(len(palettes))]
	img := image.NewNRGBA(image.Rect(0, 0, cardW, cardH))

	// 对角线渐变底色。
	for y := 0; y < cardH; y++ {
		fy := float64(y) / float64(cardH)
		for x := 0; x < cardW; x++ {
			t := float64(x)/float64(cardW)*0.35 + fy*0.65
			img.SetNRGBA(x, y, lerp(p[0], p[1], t))
		}
	}

	// 半透明光斑，避免整图过于平淡。
	blobs := 3 + rnd.Intn(3)
	for i := 0; i < blobs; i++ {
		fillCircle(img,
			rnd.Intn(cardW), rnd.Intn(cardH), 60+rnd.Intn(150),
			color.NRGBA{255, 255, 255, uint8(16 + rnd.Intn(24))})
	}

	// 底部压暗，模拟标题栏。
	for y := cardH - 150; y < cardH; y++ {
		alpha := float64(y-(cardH-150)) / 150 * 90
		for x := 0; x < cardW; x++ {
			blend(img, x, y, color.NRGBA{0, 0, 0, uint8(alpha)})
		}
	}

	switch kind {
	case kindVideo:
		drawPlayIcon(img)
	case kindImage:
		drawStackIcon(img)
	}

	drawLabel(img, fmt.Sprintf("#%04d", idx%10000), 40, 40, 9, color.NRGBA{255, 255, 255, 240})

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("编码 PNG 失败: %w", err)
	}
	return buf.Bytes(), nil
}

// lerp 在两个颜色之间线性插值。
func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 255}
}

// blend 以 src 的 alpha 将其叠加到目标像素上。
func blend(img *image.NRGBA, x, y int, src color.NRGBA) {
	if x < 0 || y < 0 || x >= cardW || y >= cardH || src.A == 0 {
		return
	}
	dst := img.NRGBAAt(x, y)
	a := float64(src.A) / 255
	mix := func(s, d uint8) uint8 { return uint8(float64(s)*a + float64(d)*(1-a)) }
	img.SetNRGBA(x, y, color.NRGBA{mix(src.R, dst.R), mix(src.G, dst.G), mix(src.B, dst.B), 255})
}

// fillCircle 填充一个圆形区域。
func fillCircle(img *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	r2 := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				blend(img, x, y, c)
			}
		}
	}
}

// drawPlayIcon 在中心绘制播放角标（圆底 + 三角）。
func drawPlayIcon(img *image.NRGBA) {
	cx, cy, r := cardW/2, cardH/2, 74
	fillCircle(img, cx, cy, r, color.NRGBA{0, 0, 0, 90})
	fillCircle(img, cx, cy, r-6, color.NRGBA{255, 255, 255, 40})
	// 等腰三角形，底边在左侧。
	h := 64
	for dy := -h / 2; dy <= h/2; dy++ {
		width := (h/2 - abs(dy)) * 3 / 2
		for dx := 0; dx < width; dx++ {
			blend(img, cx-16+dx, cy+dy, color.NRGBA{255, 255, 255, 235})
		}
	}
}

// drawStackIcon 在右上角绘制多图角标（两个叠放的圆角方块示意）。
func drawStackIcon(img *image.NRGBA) {
	x0, y0, w, h := cardW-118, 44, 62, 62
	fillRect(img, x0+14, y0-10, w, h, color.NRGBA{255, 255, 255, 110})
	fillRect(img, x0, y0, w, h, color.NRGBA{255, 255, 255, 190})
	fillRect(img, x0+8, y0+8, w-16, h-16, color.NRGBA{0, 0, 0, 60})
}

func fillRect(img *image.NRGBA, x0, y0, w, h int, c color.NRGBA) {
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			blend(img, x, y, c)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// glyphs 是 3x5 点阵字模，仅覆盖编号所需字符。
var glyphs = map[rune][5]string{
	'0': {"###", "# #", "# #", "# #", "###"},
	'1': {" # ", "## ", " # ", " # ", "###"},
	'2': {"###", "  #", "###", "#  ", "###"},
	'3': {"###", "  #", "###", "  #", "###"},
	'4': {"# #", "# #", "###", "  #", "  #"},
	'5': {"###", "#  ", "###", "  #", "###"},
	'6': {"###", "#  ", "###", "# #", "###"},
	'7': {"###", "  #", " # ", " # ", " # "},
	'8': {"###", "# #", "###", "# #", "###"},
	'9': {"###", "# #", "###", "  #", "###"},
	'#': {"# #", "###", "# #", "###", "# #"},
}

// drawLabel 以点阵字模绘制编号文本，scale 为单点像素边长。
func drawLabel(img *image.NRGBA, text string, x, y, scale int, c color.NRGBA) {
	cursor := x
	for _, ch := range text {
		g, ok := glyphs[ch]
		if !ok {
			cursor += 4 * scale
			continue
		}
		for row := 0; row < 5; row++ {
			line := g[row]
			for col := 0; col < 3; col++ {
				if line[col] != '#' {
					continue
				}
				fillRect(img, cursor+col*scale, y+row*scale, scale, scale, c)
			}
		}
		cursor += 4 * scale
	}
}
