package store

// Rendering of XML launcher icons. Apps built for API 26+ often ship only an
// <adaptive-icon> whose layers are a colour and a <vector> drawable, with no
// raster fallback at any density. androidbinary can only decode bitmaps, so
// this file walks the compiled XML itself, translates the vector drawable to
// SVG, rasterizes it with oksvg and composites the layers the way a launcher
// would: the centre 72dp of the 108dp canvas behind a rounded-square mask.

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/shogo82148/androidbinary"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // adaptive-icon layers may be webp bitmaps
)

// Android attribute resource IDs (android.R.attr). Compiled XML carries the
// attribute name in its string pool too, but shrunken APKs may blank those,
// so attributes are matched by ID first and name second.
var attrIDs = map[string]uint32{
	"alpha":          16843551,
	"color":          16843173,
	"drawable":       16843161,
	"fillAlpha":      16843980,
	"fillColor":      16843780,
	"fillType":       16844062,
	"height":         16843093,
	"pathData":       16843781,
	"pivotX":         16843189,
	"pivotY":         16843190,
	"rotation":       16843558,
	"scaleX":         16843556,
	"scaleY":         16843557,
	"src":            16843033,
	"startColor":     16843165,
	"strokeAlpha":    16843979,
	"strokeColor":    16843782,
	"strokeLineCap":  16843787,
	"strokeLineJoin": 16843788,
	"strokeWidth":    16843783,
	"tint":           16843041,
	"translateX":     16843866,
	"translateY":     16843867,
	"viewportHeight": 16843779,
	"viewportWidth":  16843778,
	"width":          16843097,
}

// apkRes reads entries and resolves resource references in one APK.
type apkRes struct {
	z     *zip.ReadCloser
	table *androidbinary.TableFile
}

func openAPKRes(path string) (*apkRes, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	r := &apkRes{z: z}
	data, err := r.read("resources.arsc")
	if err != nil {
		z.Close()
		return nil, err
	}
	if r.table, err = androidbinary.NewTableFile(bytes.NewReader(data)); err != nil {
		z.Close()
		return nil, err
	}
	return r, nil
}

func (r *apkRes) Close() error { return r.z.Close() }

func (r *apkRes) read(name string) ([]byte, error) {
	for _, f := range r.z.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 16<<20))
		}
	}
	return nil, fmt.Errorf("%s: not in apk", name)
}

func looksLikeResID(v uint32) bool {
	p := v >> 24
	return p == 0x7f || p == 0x01
}

// resolve follows a resource reference, chasing a few levels of indirection
// (a drawable attribute pointing at a colour that aliases another colour).
// Colour and file entries come back as uint32 and string respectively; the
// table does not distinguish a colour from an unresolved reference, so a
// value that cannot be chased further is returned as-is.
func (r *apkRes) resolve(id uint32) (interface{}, error) {
	var v interface{}
	for i := 0; i < 4; i++ {
		var err error
		v, err = r.table.GetResource(androidbinary.ResID(id), nil)
		if err != nil {
			if i > 0 {
				return id, nil
			}
			return nil, err
		}
		n, ok := v.(uint32)
		if !ok || !looksLikeResID(n) {
			return v, nil
		}
		id = n
	}
	return v, nil
}

// --- compiled XML -----------------------------------------------------------

type xmlAttr struct {
	ID   uint32 // android attribute resource ID, 0 when unknown
	Name string
	Raw  string // string value, when the attribute is a string
	Type androidbinary.DataType
	Data uint32
}

type xmlNode struct {
	Name     string
	Attrs    []xmlAttr
	Children []*xmlNode
}

func (n *xmlNode) attr(name string) *xmlAttr {
	id := attrIDs[name]
	for i := range n.Attrs {
		a := &n.Attrs[i]
		if (id != 0 && a.ID == id) || a.Name == name {
			return a
		}
	}
	return nil
}

func (n *xmlNode) child(name string) *xmlNode {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// parseBinaryXML decodes an Android binary XML document into a tree of
// elements with typed attributes. androidbinary is used for the string pool;
// the element chunks are walked here because its own decoder flattens typed
// values (colours, floats) into opaque "@0x..." strings.
func parseBinaryXML(data []byte) (*xmlNode, error) {
	xf, err := androidbinary.NewXMLFile(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	str := func(ref androidbinary.ResStringPoolRef) string {
		if xf.HasString(ref) {
			return xf.GetString(ref)
		}
		return ""
	}
	le := binary.LittleEndian
	var hdr androidbinary.ResChunkHeader
	if err := binary.Read(bytes.NewReader(data), le, &hdr); err != nil {
		return nil, err
	}
	if hdr.Type != androidbinary.ResXMLChunkType {
		return nil, fmt.Errorf("not a binary xml document (chunk type 0x%04x)", uint16(hdr.Type))
	}
	end := len(data)
	if int(hdr.Size) < end {
		end = int(hdr.Size)
	}
	var resMap []uint32
	root := &xmlNode{}
	stack := []*xmlNode{root}
	off := int(hdr.HeaderSize)
	for off+8 <= end {
		var ch androidbinary.ResChunkHeader
		if err := binary.Read(bytes.NewReader(data[off:]), le, &ch); err != nil {
			return nil, err
		}
		if ch.Size < 8 || off+int(ch.Size) > end {
			return nil, errors.New("binary xml: bad chunk size")
		}
		chunk := data[off : off+int(ch.Size)]
		switch ch.Type {
		case androidbinary.ResXMLResourceMapType:
			n := (int(ch.Size) - int(ch.HeaderSize)) / 4
			resMap = make([]uint32, n)
			for i := 0; i < n; i++ {
				resMap[i] = le.Uint32(chunk[int(ch.HeaderSize)+i*4:])
			}
		case androidbinary.ResXMLStartElementType:
			if int(ch.HeaderSize)+20 > len(chunk) {
				return nil, errors.New("binary xml: short start element")
			}
			var ext androidbinary.ResXMLTreeAttrExt
			if err := binary.Read(bytes.NewReader(chunk[ch.HeaderSize:]), le, &ext); err != nil {
				return nil, err
			}
			n := &xmlNode{Name: str(ext.Name)}
			base := int(ch.HeaderSize) + int(ext.AttributeStart)
			for i := 0; i < int(ext.AttributeCount); i++ {
				o := base + i*int(ext.AttributeSize)
				if o+20 > len(chunk) {
					break
				}
				var a androidbinary.ResXMLTreeAttribute
				if err := binary.Read(bytes.NewReader(chunk[o:]), le, &a); err != nil {
					return nil, err
				}
				at := xmlAttr{Name: str(a.Name), Type: a.TypedValue.DataType, Data: a.TypedValue.Data}
				if int(a.Name) < len(resMap) {
					at.ID = resMap[a.Name]
				}
				if a.RawValue != androidbinary.NilResStringPoolRef {
					at.Raw = str(a.RawValue)
				} else if at.Type == androidbinary.TypeString {
					at.Raw = str(androidbinary.ResStringPoolRef(at.Data))
				}
				n.Attrs = append(n.Attrs, at)
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, n)
			stack = append(stack, n)
		case androidbinary.ResXMLEndElementType:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
		off += int(ch.Size)
	}
	if len(root.Children) == 0 {
		return nil, errors.New("binary xml: no root element")
	}
	return root.Children[0], nil
}

// --- typed attribute access ---------------------------------------------------

func (r *apkRes) floatAttr(n *xmlNode, name string, def float64) float64 {
	a := n.attr(name)
	if a == nil {
		return def
	}
	switch a.Type {
	case androidbinary.TypeFloat:
		return float64(math.Float32frombits(a.Data))
	case androidbinary.TypeIntDec, androidbinary.TypeIntHex:
		return float64(int32(a.Data))
	case androidbinary.TypeReference:
		if v, err := r.resolve(a.Data); err == nil {
			if u, ok := v.(uint32); ok {
				return float64(math.Float32frombits(u))
			}
		}
	default:
		if f, err := strconv.ParseFloat(strings.TrimSuffix(a.Raw, "dp"), 64); err == nil {
			return f
		}
	}
	return def
}

func (r *apkRes) intAttr(n *xmlNode, name string, def int) int {
	a := n.attr(name)
	if a == nil {
		return def
	}
	switch a.Type {
	case androidbinary.TypeIntDec, androidbinary.TypeIntHex, androidbinary.TypeIntBoolean:
		return int(int32(a.Data))
	}
	return def
}

func (r *apkRes) stringAttr(n *xmlNode, name string) string {
	a := n.attr(name)
	if a == nil {
		return ""
	}
	if a.Raw != "" {
		return a.Raw
	}
	if a.Type == androidbinary.TypeReference {
		if v, err := r.resolve(a.Data); err == nil {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func argb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: uint8(v >> 24)}
}

func typedColor(t androidbinary.DataType, v uint32) (color.NRGBA, bool) {
	switch t {
	case androidbinary.TypeIntColorARGB8:
		return argb(v), true
	case androidbinary.TypeIntColorRGB8:
		return argb(v | 0xff000000), true
	case androidbinary.TypeIntColorARGB4:
		x := func(n uint32) uint8 { return uint8(n&0xf) * 17 }
		return color.NRGBA{R: x(v >> 8), G: x(v >> 4), B: x(v), A: x(v >> 12)}, true
	case androidbinary.TypeIntColorRGB4:
		x := func(n uint32) uint8 { return uint8(n&0xf) * 17 }
		return color.NRGBA{R: x(v >> 8), G: x(v >> 4), B: x(v), A: 255}, true
	}
	return color.NRGBA{}, false
}

func parseHexColor(s string) (color.NRGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	switch len(s) {
	case 3:
		return typedColor(androidbinary.TypeIntColorRGB4, uint32(v))
	case 4:
		return typedColor(androidbinary.TypeIntColorARGB4, uint32(v))
	case 6:
		return argb(uint32(v) | 0xff000000), true
	case 8:
		return argb(uint32(v)), true
	}
	return color.NRGBA{}, false
}

// colorAttr resolves a colour attribute, following references. A reference
// to a colour-state-list or gradient XML yields its first colour.
func (r *apkRes) colorAttr(n *xmlNode, name string) (color.NRGBA, bool) {
	a := n.attr(name)
	if a == nil {
		return color.NRGBA{}, false
	}
	if c, ok := typedColor(a.Type, a.Data); ok {
		return c, true
	}
	if a.Type == androidbinary.TypeReference {
		return r.colorRef(a.Data, 0)
	}
	return parseHexColor(a.Raw)
}

func (r *apkRes) colorRef(id uint32, depth int) (color.NRGBA, bool) {
	v, err := r.resolve(id)
	if err != nil || depth > 2 {
		return color.NRGBA{}, false
	}
	switch v := v.(type) {
	case uint32:
		return argb(v), true
	case string:
		if !strings.HasSuffix(v, ".xml") {
			return color.NRGBA{}, false
		}
		data, err := r.read(v)
		if err != nil {
			return color.NRGBA{}, false
		}
		n, err := parseBinaryXML(data)
		if err != nil {
			return color.NRGBA{}, false
		}
		// <gradient android:startColor=...> or <selector><item android:color=...>
		if c, ok := r.colorAttr(n, "startColor"); ok {
			return c, true
		}
		for _, item := range n.Children {
			if c, ok := r.colorAttr(item, "color"); ok {
				return c, true
			}
		}
	}
	return color.NRGBA{}, false
}

// --- vector drawable → SVG ------------------------------------------------------

func svgColor(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// vectorToSVG translates a compiled <vector> drawable into SVG text. Groups
// become <g transform> (pivot/scale/rotate/translate in Android's order),
// paths carry their fill/stroke; clip paths and trim paths are ignored.
func (r *apkRes) vectorToSVG(n *xmlNode) (string, error) {
	vw := r.floatAttr(n, "viewportWidth", 0)
	vh := r.floatAttr(n, "viewportHeight", 0)
	if vw <= 0 || vh <= 0 {
		return "", errors.New("vector: missing viewport")
	}
	var tint *color.NRGBA
	if c, ok := r.colorAttr(n, "tint"); ok {
		tint = &c
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g">`, vw, vh, vw, vh)
	if alpha := r.floatAttr(n, "alpha", 1); alpha < 1 {
		fmt.Fprintf(&b, `<g opacity="%g">`, alpha)
		r.writeVectorChildren(&b, n, tint)
		b.WriteString("</g>")
	} else {
		r.writeVectorChildren(&b, n, tint)
	}
	b.WriteString("</svg>")
	return b.String(), nil
}

func (r *apkRes) writeVectorChildren(b *strings.Builder, n *xmlNode, tint *color.NRGBA) {
	for _, c := range n.Children {
		switch c.Name {
		case "group":
			tx, ty := r.floatAttr(c, "translateX", 0), r.floatAttr(c, "translateY", 0)
			px, py := r.floatAttr(c, "pivotX", 0), r.floatAttr(c, "pivotY", 0)
			sx, sy := r.floatAttr(c, "scaleX", 1), r.floatAttr(c, "scaleY", 1)
			rot := r.floatAttr(c, "rotation", 0)
			fmt.Fprintf(b, `<g transform="translate(%g, %g) rotate(%g) scale(%g, %g) translate(%g, %g)">`,
				tx+px, ty+py, rot, sx, sy, -px, -py)
			r.writeVectorChildren(b, c, tint)
			b.WriteString("</g>")
		case "path":
			d := r.stringAttr(c, "pathData")
			if d == "" {
				continue
			}
			fmt.Fprintf(b, `<path d="%s"`, escapeAttr(d))
			if fill, ok := r.colorAttr(c, "fillColor"); ok && fill.A > 0 {
				if tint != nil {
					fill = *tint
				}
				fmt.Fprintf(b, ` fill="%s" fill-opacity="%g"`, svgColor(fill), float64(fill.A)/255*r.floatAttr(c, "fillAlpha", 1))
			} else {
				b.WriteString(` fill="none"`)
			}
			if r.intAttr(c, "fillType", 0) == 1 {
				b.WriteString(` fill-rule="evenodd"`)
			}
			if sw := r.floatAttr(c, "strokeWidth", 0); sw > 0 {
				if stroke, ok := r.colorAttr(c, "strokeColor"); ok && stroke.A > 0 {
					if tint != nil {
						stroke = *tint
					}
					caps := [...]string{"butt", "round", "square"}
					joins := [...]string{"miter", "round", "bevel"}
					fmt.Fprintf(b, ` stroke="%s" stroke-opacity="%g" stroke-width="%g" stroke-linecap="%s" stroke-linejoin="%s"`,
						svgColor(stroke), float64(stroke.A)/255*r.floatAttr(c, "strokeAlpha", 1), sw,
						caps[clampIdx(r.intAttr(c, "strokeLineCap", 0), len(caps))],
						joins[clampIdx(r.intAttr(c, "strokeLineJoin", 0), len(joins))])
				}
			}
			b.WriteString("/>")
		}
	}
}

func clampIdx(i, n int) int {
	if i < 0 || i >= n {
		return 0
	}
	return i
}

func escapeAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// --- rendering ------------------------------------------------------------------

func (r *apkRes) renderVector(n *xmlNode, size int) (image.Image, error) {
	svg, err := r.vectorToSVG(n)
	if err != nil {
		return nil, err
	}
	icon, err := oksvg.ReadIconStream(strings.NewReader(svg), oksvg.WarnErrorMode)
	if err != nil {
		return nil, fmt.Errorf("vector: %w", err)
	}
	icon.SetTarget(0, 0, float64(size), float64(size))
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	scanner := rasterx.NewScannerGV(size, size, img, img.Bounds())
	icon.Draw(rasterx.NewDasher(size, size, scanner), 1.0)
	return img, nil
}

func solid(c color.Color, size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

func scaleSquare(src image.Image, size int) image.Image {
	if b := src.Bounds(); b.Dx() == size && b.Dy() == size {
		return src
	}
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

// renderDrawableFile renders a drawable zip entry (compiled XML or bitmap)
// to a size×size image.
func (r *apkRes) renderDrawableFile(path string, size int) (image.Image, error) {
	data, err := r.read(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".xml") {
		n, err := parseBinaryXML(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return r.renderNode(n, size)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return scaleSquare(img, size), nil
}

// renderDrawableRef renders a reference held in an attribute: a colour, or
// a resource ID pointing at a colour or a drawable file.
func (r *apkRes) renderDrawableRef(a *xmlAttr, size int) (image.Image, error) {
	if c, ok := typedColor(a.Type, a.Data); ok {
		return solid(c, size), nil
	}
	if a.Type != androidbinary.TypeReference {
		if c, ok := parseHexColor(a.Raw); ok {
			return solid(c, size), nil
		}
		return nil, fmt.Errorf("unsupported drawable value %q", a.Raw)
	}
	v, err := r.resolve(a.Data)
	if err != nil {
		return nil, err
	}
	switch v := v.(type) {
	case uint32:
		return solid(argb(v), size), nil
	case string:
		return r.renderDrawableFile(v, size)
	}
	return nil, fmt.Errorf("unsupported drawable resource 0x%08x", a.Data)
}

// renderNode renders a compiled drawable document.
func (r *apkRes) renderNode(n *xmlNode, size int) (image.Image, error) {
	switch n.Name {
	case "vector":
		return r.renderVector(n, size)
	case "adaptive-icon":
		return r.renderAdaptive(n, size)
	case "bitmap":
		if a := n.attr("src"); a != nil {
			return r.renderDrawableRef(a, size)
		}
	case "inset", "layer-list":
		// Best effort: draw the layers (or the single wrapped drawable) full
		// size without insets.
		canvas := image.NewNRGBA(image.Rect(0, 0, size, size))
		drawn := 0
		for _, c := range n.Children {
			if a := c.attr("drawable"); a != nil {
				if img, err := r.renderDrawableRef(a, size); err == nil {
					draw.Draw(canvas, canvas.Bounds(), img, image.Point{}, draw.Over)
					drawn++
				}
			} else if len(c.Children) > 0 {
				if img, err := r.renderNode(c.Children[0], size); err == nil {
					draw.Draw(canvas, canvas.Bounds(), img, image.Point{}, draw.Over)
					drawn++
				}
			}
		}
		if a := n.attr("drawable"); a != nil {
			if img, err := r.renderDrawableRef(a, size); err == nil {
				draw.Draw(canvas, canvas.Bounds(), img, image.Point{}, draw.Over)
				drawn++
			}
		}
		if drawn > 0 {
			return canvas, nil
		}
	}
	return nil, fmt.Errorf("unsupported drawable <%s>", n.Name)
}

// renderAdaptive composites an <adaptive-icon>: both layers fill a 108dp
// canvas, the launcher shows the centre 72dp through its mask. The output
// is size×size with a rounded-square mask applied.
func (r *apkRes) renderAdaptive(n *xmlNode, size int) (image.Image, error) {
	full := size * 108 / 72
	canvas := image.NewNRGBA(image.Rect(0, 0, full, full))
	drawn := 0
	var firstErr error
	for _, layerName := range []string{"background", "foreground"} {
		layer := n.child(layerName)
		if layer == nil {
			continue
		}
		var img image.Image
		var err error
		if a := layer.attr("drawable"); a != nil {
			img, err = r.renderDrawableRef(a, full)
		} else if len(layer.Children) > 0 {
			img, err = r.renderNode(layer.Children[0], full)
		} else {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("adaptive-icon %s: %w", layerName, err)
			}
			continue
		}
		draw.Draw(canvas, canvas.Bounds(), img, image.Point{}, draw.Over)
		drawn++
	}
	if drawn == 0 {
		if firstErr == nil {
			firstErr = errors.New("adaptive-icon: no layers")
		}
		return nil, firstErr
	}
	off := (full - size) / 2
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.DrawMask(out, out.Bounds(), canvas, image.Pt(off, off), roundedMask(size, 0.2), image.Point{}, draw.Src)
	return out, nil
}

// roundedMask is an anti-aliased rounded-square alpha mask; radius is a
// fraction of size.
func roundedMask(size int, radius float64) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, size, size))
	rad := radius * float64(size)
	half := float64(size) / 2
	inner := half - rad
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := math.Max(math.Abs(float64(x)+0.5-half)-inner, 0)
			dy := math.Max(math.Abs(float64(y)+0.5-half)-inner, 0)
			d := math.Sqrt(dx*dx+dy*dy) - rad
			a := math.Min(math.Max(0.5-d, 0), 1)
			m.Pix[y*m.Stride+x] = uint8(a * 255)
		}
	}
	return m
}

// renderIconFile renders the manifest icon when it is not a plain bitmap:
// an adaptive icon, a vector, or a bitmap wrapper. size is the output edge.
func renderIconFile(apkPath, iconPath string, size int) (image.Image, error) {
	res, err := openAPKRes(apkPath)
	if err != nil {
		return nil, err
	}
	defer res.Close()
	return res.renderDrawableFile(iconPath, size)
}
