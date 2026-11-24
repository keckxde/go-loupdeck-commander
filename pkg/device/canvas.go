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

// cachedIconEntry stores a decoded icon image alongside its file metadata for
// mtime-and-size cache invalidation.
type cachedIconEntry struct {
	img   image.Image
	mtime time.Time
	size  int64
}

// scaledIconKey uniquely identifies a cached bilinear-scaled RGBA image by
// source image reference and target pixel dimensions.
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

// initFonts lazily parses the embedded Go Regular and Go Bold TrueType fonts
// once per process.
func initFonts() {
	fontOnce.Do(func() {
		regularFont, _ = opentype.Parse(goregular.TTF)
		boldFont, _ = opentype.Parse(gobold.TTF)
	})
}

// getFontFace parses a CSS-like font specification (e.g., "bold 15px Arial") and
// returns a cached OpenType font.Face along with the resolved pixel size.
// Returns (nil, size) if font parsing or face creation fails.
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

// LoadCachedImage decodes an image (PNG, JPEG, or GIF) from filePath and caches
// the result in memory, invalidating the cache entry if the file's modification
// time or byte size changes.
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

// Canvas represents an off-screen RGBA pixel surface used to render device screens.
type Canvas struct {
	Width  int
	Height int
	Img    *image.RGBA
}

// NewCanvas allocates a new Canvas of the given pixel width and height.
func NewCanvas(width, height int) *Canvas {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	return &Canvas{
		Width:  width,
		Height: height,
		Img:    img,
	}
}

// GetContext2D returns a new Context2D bound to the Canvas with default
// HTML5-style fill, font, alignment, and compositing properties.
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

// ToPNGBytes encodes the Canvas RGBA image into fast-compressed PNG bytes.
func (c *Canvas) ToPNGBytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := fastPNGEnc.Encode(&buf, c.Img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ToDataURL encodes the Canvas as a base64-encoded PNG data URL, returning an
// empty string if PNG encoding fails.
func (c *Canvas) ToDataURL() string {
	b, err := c.ToPNGBytes()
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
}

// ToRGB565 converts the Canvas RGBA pixels into packed RGB565 bytes using the
// specified endianness (big-endian if bigEndian is true, little-endian otherwise).
func (c *Canvas) ToRGB565(bigEndian bool) []byte {
	return Rgba2Rgb565(c.Img.Pix, c.Width*c.Height, bigEndian)
}

// contextState stores a snapshot of Context2D styling attributes for Save/Restore.
type contextState struct {
	FillStyle                string
	Font                     string
	TextAlign                string
	TextBaseline             string
	GlobalCompositeOperation string
}

// Context2D provides an HTML5 Canvas 2D-like drawing API for rendering solid
// rectangles, scaled images, and aligned text onto a Canvas.
type Context2D struct {
	Canvas                   *Canvas
	FillStyle                string
	Font                     string
	TextAlign                string
	TextBaseline             string
	GlobalCompositeOperation string
	stack                    []contextState
}

// Save pushes the current Context2D styling attributes onto the state stack.
func (ctx *Context2D) Save() {
	ctx.stack = append(ctx.stack, contextState{
		FillStyle:                ctx.FillStyle,
		Font:                     ctx.Font,
		TextAlign:                ctx.TextAlign,
		TextBaseline:             ctx.TextBaseline,
		GlobalCompositeOperation: ctx.GlobalCompositeOperation,
	})
}

// Restore pops the most recently saved styling attributes from the state stack,
// or does nothing if the stack is empty.
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

// FillRect fills the rectangle at (x, y) with dimensions (width, height) using
// the current FillStyle color, clipped to the Canvas bounds.
func (ctx *Context2D) FillRect(x, y, width, height float64) {
	r, g, b := config.ColorToRGBBytes(ctx.FillStyle)
	col := color.RGBA{R: r, G: g, B: b, A: 255}
	rect := image.Rect(int(x), int(y), int(x+width), int(y+height)).Intersect(ctx.Canvas.Img.Bounds())
	if rect.Empty() {
		return
	}
	draw.Draw(ctx.Canvas.Img, rect, &image.Uniform{C: col}, image.Point{}, draw.Src)
}

// DrawImage scales src to (dw, dh) using bilinear interpolation (caching the
// scaled RGBA result) and alpha-composites it onto the Canvas at (dx, dy).
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

// FillText renders text at (x, y) using the current Font, FillStyle, TextAlign,
// and TextBaseline properties.
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
