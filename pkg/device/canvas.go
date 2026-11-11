package device

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitlab.com/keckxde/loupedeck-commander-go/pkg/config"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type cachedIconEntry struct {
	img   image.Image
	mtime time.Time
	size  int64
}

type scaledIconKey struct {
	src image.Image
	w   int
	h   int
}

var (
	iconCacheMu   sync.RWMutex
	iconCache     = make(map[string]cachedIconEntry)
	scaledCacheMu sync.RWMutex
	scaledCache   = make(map[scaledIconKey]*image.RGBA)
	fastPNGEnc    = &png.Encoder{CompressionLevel: png.BestSpeed}

	fontOnce    sync.Once
	regularFont *opentype.Font
	boldFont    *opentype.Font
	faceCacheMu sync.Mutex
	faceCache   = make(map[string]font.Face)
	fontSizeRe  = regexp.MustCompile(`(\d+(?:\.\d+)?)px`)
)

func initFonts() {
	fontOnce.Do(func() {
		regularFont, _ = opentype.Parse(goregular.TTF)
		boldFont, _ = opentype.Parse(gobold.TTF)
	})
}

func getFontFace(fontSpec string) (font.Face, float64) {
	initFonts()
	size := 15.0
	if m := fontSizeRe.FindStringSubmatch(fontSpec); len(m) >= 2 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil && v > 0 {
			size = v
		}
	}
	isBold := strings.Contains(strings.ToLower(fontSpec), "bold")
	cacheKey := strconv.FormatBool(isBold) + ":" + strconv.FormatFloat(size, 'f', 1, 64)

	faceCacheMu.Lock()
	defer faceCacheMu.Unlock()
	if f, ok := faceCache[cacheKey]; ok {
		return f, size
	}
	ttf := regularFont
	if isBold && boldFont != nil {
		ttf = boldFont
	}
	if ttf == nil {
		return nil, size
	}
	face, err := opentype.NewFace(ttf, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, size
	}
	faceCache[cacheKey] = face
	return face, size
}

func LoadCachedImage(filePath string) (image.Image, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	iconCacheMu.RLock()
	cached, exists := iconCache[filePath]
	iconCacheMu.RUnlock()
	if exists && cached.mtime.Equal(stat.ModTime()) && cached.size == stat.Size() {
		return cached.img, nil
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}

	iconCacheMu.Lock()
	iconCache[filePath] = cachedIconEntry{
		img:   img,
		mtime: stat.ModTime(),
		size:  stat.Size(),
	}
	iconCacheMu.Unlock()
	return img, nil
}

type Canvas struct {
	Width  int
	Height int
	Img    *image.RGBA
}

func NewCanvas(width, height int) *Canvas {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	return &Canvas{
		Width:  width,
		Height: height,
		Img:    img,
	}
}

func (c *Canvas) GetContext2D() *Context2D {
	return &Context2D{
		Canvas:                   c,
		FillStyle:                "#000000",
		Font:                     "15px Arial",
		TextAlign:                "left",
		TextBaseline:             "alphabetic",
		GlobalCompositeOperation: "source-over",
	}
}

func (c *Canvas) ToPNGBytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := fastPNGEnc.Encode(&buf, c.Img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *Canvas) ToDataURL() string {
	b, err := c.ToPNGBytes()
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
}

func (c *Canvas) ToRGB565(bigEndian bool) []byte {
	return Rgba2Rgb565(c.Img.Pix, c.Width*c.Height, bigEndian)
}

type contextState struct {
	FillStyle                string
	Font                     string
	TextAlign                string
	TextBaseline             string
	GlobalCompositeOperation string
}

type Context2D struct {
	Canvas                   *Canvas
	FillStyle                string
	Font                     string
	TextAlign                string
	TextBaseline             string
	GlobalCompositeOperation string
	stack                    []contextState
}

func (ctx *Context2D) Save() {
	ctx.stack = append(ctx.stack, contextState{
		FillStyle:                ctx.FillStyle,
		Font:                     ctx.Font,
		TextAlign:                ctx.TextAlign,
		TextBaseline:             ctx.TextBaseline,
		GlobalCompositeOperation: ctx.GlobalCompositeOperation,
	})
}

func (ctx *Context2D) Restore() {
	if len(ctx.stack) == 0 {
		return
	}
	top := ctx.stack[len(ctx.stack)-1]
	ctx.stack = ctx.stack[:len(ctx.stack)-1]
	ctx.FillStyle = top.FillStyle
	ctx.Font = top.Font
	ctx.TextAlign = top.TextAlign
	ctx.TextBaseline = top.TextBaseline
	ctx.GlobalCompositeOperation = top.GlobalCompositeOperation
}

func (ctx *Context2D) FillRect(x, y, width, height float64) {
	r, g, b := config.ColorToRGBBytes(ctx.FillStyle)
	col := color.RGBA{R: r, G: g, B: b, A: 255}
	rect := image.Rect(int(x), int(y), int(x+width), int(y+height)).Intersect(ctx.Canvas.Img.Bounds())
	if rect.Empty() {
		return
	}
	draw.Draw(ctx.Canvas.Img, rect, &image.Uniform{C: col}, image.Point{}, draw.Src)
}

func (ctx *Context2D) DrawImage(src image.Image, dx, dy, dw, dh float64) {
	if src == nil {
		return
	}
	dstRect := image.Rect(int(dx), int(dy), int(dx+dw), int(dy+dh))
	if dstRect.Empty() {
		return
	}
	tw := dstRect.Dx()
	th := dstRect.Dy()
	key := scaledIconKey{src: src, w: tw, h: th}

	scaledCacheMu.RLock()
	scaled, ok := scaledCache[key]
	scaledCacheMu.RUnlock()

	if !ok {
		scaled = image.NewRGBA(image.Rect(0, 0, tw, th))
		xdraw.BiLinear.Scale(scaled, scaled.Bounds(), src, src.Bounds(), draw.Src, nil)
		scaledCacheMu.Lock()
		scaledCache[key] = scaled
		scaledCacheMu.Unlock()
	}

	draw.Draw(ctx.Canvas.Img, dstRect, scaled, image.Point{}, draw.Over)
}

func (ctx *Context2D) FillText(text string, x, y float64) {
	if text == "" {
		return
	}
	faceCacheMu.Lock()
	defer faceCacheMu.Unlock()

	initFonts()
	size := 15.0
	if m := fontSizeRe.FindStringSubmatch(ctx.Font); len(m) >= 2 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil && v > 0 {
			size = v
		}
	}
	isBold := strings.Contains(strings.ToLower(ctx.Font), "bold")
	cacheKey := strconv.FormatBool(isBold) + ":" + strconv.FormatFloat(size, 'f', 1, 64)
	face, ok := faceCache[cacheKey]
	if !ok {
		ttf := regularFont
		if isBold && boldFont != nil {
			ttf = boldFont
		}
		if ttf == nil {
			return
		}
		var err error
		face, err = opentype.NewFace(ttf, &opentype.FaceOptions{
			Size:    size,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if err != nil {
			return
		}
		faceCache[cacheKey] = face
	}

	r, g, b := config.ColorToRGBBytes(ctx.FillStyle)
	col := color.RGBA{R: r, G: g, B: b, A: 255}

	adv := font.MeasureString(face, text)
	textWidth := float64(adv.Round())
	metrics := face.Metrics()
	ascent := float64(metrics.Ascent.Round())
	descent := float64(metrics.Descent.Round())

	drawX := x
	switch strings.ToLower(ctx.TextAlign) {
	case "center":
		drawX = x - textWidth/2.0
	case "right", "end":
		drawX = x - textWidth
	}

	drawY := y
	switch strings.ToLower(ctx.TextBaseline) {
	case "top", "hanging":
		drawY = y + ascent
	case "middle":
		drawY = y + (ascent-descent)/2.0
	case "bottom", "ideographic":
		drawY = y - descent
	}

	d := &font.Drawer{
		Dst:  ctx.Canvas.Img,
		Src:  image.NewUniform(col),
		Face: face,
		Dot:  fixed.P(int(drawX), int(drawY)),
	}
	d.DrawString(text)
}

