// Package identicon 本地生成一个确定性的几何图案头像（同一个seed永远生成同一张图），
// 效果上跟Gravatar的identicon一样，但完全本机渲染，不依赖任何外部服务——
// gravatar.com在国内经常连不上，新用户没设头像时不能指着一个可能加载不出来的图。
package identicon

import (
	"bytes"
	"crypto/md5"
	"image"
	"image/color"
	"image/png"
	"math"
)

const (
	gridSize = 5
	cellPx   = 40
)

// PNG 返回seed对应的确定性几何头像PNG字节。5x5网格左右对称（经典identicon风格），
// 背景浅灰，色块颜色由seed哈希算出的色相决定。
func PNG(seed string) []byte {
	sum := md5.Sum([]byte(seed))
	hue := float64(sum[0]) / 255 * 360
	fg := hslToRGBA(hue, 0.65, 0.55)
	bg := color.RGBA{0xee, 0xf2, 0xf9, 0xff}

	size := gridSize * cellPx
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, bg)
		}
	}

	cols := (gridSize + 1) / 2 // 5列只需算前3列，剩下2列靠镜像
	for row := 0; row < gridSize; row++ {
		for col := 0; col < cols; col++ {
			idx := row*cols + col
			b := sum[(idx+1)%len(sum)]
			if b%2 == 0 {
				continue
			}
			fillCell(img, col, row, fg)
			mirrorCol := gridSize - 1 - col
			if mirrorCol != col {
				fillCell(img, mirrorCol, row, fg)
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func fillCell(img *image.RGBA, col, row int, c color.RGBA) {
	x0, y0 := col*cellPx, row*cellPx
	for y := y0; y < y0+cellPx; y++ {
		for x := x0; x < x0+cellPx; x++ {
			img.Set(x, y, c)
		}
	}
}

func hslToRGBA(h, s, l float64) color.RGBA {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return color.RGBA{
		R: uint8((r + m) * 255),
		G: uint8((g + m) * 255),
		B: uint8((b + m) * 255),
		A: 0xff,
	}
}
