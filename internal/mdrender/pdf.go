package mdrender

// PDF rendering. The markdown AST produced by the same goldmark parser that
// feeds the HTML view is walked here and painted onto a PDF with go-pdf/fpdf,
// a pure-Go writer — no headless browser or external converter is involved.
//
// Layout is done by hand: inline content is gathered into styled runs, then
// flowed word by word against the current column width, which is what makes
// nested lists, blockquote indents and wrapped table cells line up. Text is
// encoded to cp1252 because the PDF core fonts (Helvetica/Courier) are
// single-byte; runes outside that repertoire are transliterated where there is
// an obvious equivalent and dropped to '?' otherwise.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Page geometry, in millimetres (A4 portrait).
const (
	pdfPageW  = 210.0
	pdfPageH  = 297.0
	pdfMargin = 18.0
	pdfBottom = 18.0

	pdfBodySize = 10.5
	pdfCodeSize = 8.8

	pdfIndent     = 7.0 // per list nesting level
	pdfQuoteInset = 6.0 // blockquote indent
	pdfCellPad    = 1.8 // table cell padding
)

type rgb struct{ r, g, b int }

// Colours mirror the light half of assets/github.css so a printed page looks
// like the browser view.
var (
	colFg     = rgb{31, 35, 40}
	colMuted  = rgb{89, 99, 110}
	colBorder = rgb{209, 217, 224}
	colCodeBg = rgb{246, 248, 250}
	colAccent = rgb{9, 105, 218}
)

// lineHeight converts a point size to the millimetre advance between baselines.
func lineHeight(size, factor float64) float64 { return size * 0.3528 * factor }

// inlineStyle is the accumulated emphasis state at some point in the inline
// tree; it maps directly onto a font selection.
type inlineStyle struct {
	bold, italic, code, strike bool
	link                       string
}

type runKind int

const (
	runText  runKind = iota
	runBreak         // hard line break
	runImage
)

type inlineRun struct {
	kind runKind
	text string // cp1252-encoded
	st   inlineStyle
	img  string // resolved filesystem path, for runImage
}

// pdfDoc carries the paint state for one document: the target page, the
// markdown source (needed to resolve AST segments), and the current column.
type pdfDoc struct {
	pdf  *fpdf.Fpdf
	src  []byte
	dir  string // directory of the markdown file, for relative image paths
	left float64
	imgN int
}

func (d *pdfDoc) right() float64    { return pdfPageW - pdfMargin }
func (d *pdfDoc) contentW() float64 { return d.right() - d.left }

func (d *pdfDoc) setColor(c rgb) { d.pdf.SetTextColor(c.r, c.g, c.b) }

// font selects the core font matching st at the given size.
func (d *pdfDoc) font(st inlineStyle, size float64) {
	family := "Helvetica"
	style := ""
	if st.code {
		family = "Courier"
		size *= 0.92
	}
	if st.bold {
		style += "B"
	}
	if st.italic {
		style += "I"
	}
	if st.link != "" {
		style += "U"
	}
	d.pdf.SetFont(family, style, size)
}

// ensure starts a new page when h more millimetres would not fit, and returns
// the y to draw at.
func (d *pdfDoc) ensure(y, h float64) float64 {
	if y+h <= pdfPageH-pdfBottom {
		return y
	}
	d.pdf.AddPage()
	return pdfMargin
}

// renderPDF converts markdown source into a PDF document. title labels the
// document (its header line, footer and PDF metadata).
func RenderPDF(md goldmark.Markdown, src []byte, title, srcDir string) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pdfMargin, pdfMargin, pdfMargin)
	// Page breaks are placed by hand so that block spacing, table rows and
	// code blocks are never split at an arbitrary point.
	pdf.SetAutoPageBreak(false, pdfBottom)
	pdf.SetCellMargin(0)
	pdf.SetTitle(title, true)
	pdf.SetCreator("godown", true)

	footer := cp1252(title)
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "", 8)
		pdf.SetTextColor(colMuted.r, colMuted.g, colMuted.b)
		half := (pdfPageW - 2*pdfMargin) / 2
		pdf.CellFormat(half, 6, footer, "", 0, "L", false, 0, "")
		pdf.CellFormat(half, 6, fmt.Sprintf("%d", pdf.PageNo()), "", 0, "R", false, 0, "")
	})

	pdf.AddPage()

	d := &pdfDoc{pdf: pdf, src: src, dir: srcDir, left: pdfMargin}

	// Document header: the file name, matching the header on the HTML page.
	y := pdfMargin
	d.setColor(colMuted)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetXY(d.left, y)
	pdf.CellFormat(d.contentW(), 5, footer, "", 0, "L", false, 0, "")
	y += 6
	d.rule(y)
	y += 5

	doc := md.Parser().Parse(text.NewReader(src))
	y = d.blocks(doc, y)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rule draws a full-width hairline at y.
func (d *pdfDoc) rule(y float64) {
	d.pdf.SetDrawColor(colBorder.r, colBorder.g, colBorder.b)
	d.pdf.SetLineWidth(0.2)
	d.pdf.Line(d.left, y, d.right(), y)
}

// blocks renders every child of n in order, returning the y below the last one.
func (d *pdfDoc) blocks(n ast.Node, y float64) float64 {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		y = d.block(c, y)
	}
	return y
}

// block renders one block-level node and returns the y below it.
func (d *pdfDoc) block(n ast.Node, y float64) float64 {
	switch n := n.(type) {
	case *ast.Heading:
		return d.heading(n, y)
	case *ast.Paragraph:
		y = d.flow(d.inlines(n), flowOpts{x: d.left, y: y, width: d.contentW(),
			size: pdfBodySize, factor: 1.45, color: colFg})
		return y + 3.2
	case *ast.TextBlock:
		// Paragraph body inside a tight list item: no trailing gap.
		return d.flow(d.inlines(n), flowOpts{x: d.left, y: y, width: d.contentW(),
			size: pdfBodySize, factor: 1.45, color: colFg})
	case *ast.FencedCodeBlock:
		return d.codeBlock(n.Lines(), y)
	case *ast.CodeBlock:
		return d.codeBlock(n.Lines(), y)
	case *ast.Blockquote:
		return d.blockquote(n, y)
	case *ast.List:
		return d.list(n, y)
	case *ast.ThematicBreak:
		y = d.ensure(y, 8) + 3
		d.pdf.SetDrawColor(colBorder.r, colBorder.g, colBorder.b)
		d.pdf.SetLineWidth(0.5)
		d.pdf.Line(d.left, y, d.right(), y)
		return y + 5
	case *east.Table:
		return d.table(n, y)
	case *ast.HTMLBlock:
		// Raw HTML has no meaning on a printed page; skip it rather than
		// dumping markup into the text.
		return y
	default:
		if n.Type() == ast.TypeBlock {
			return d.blocks(n, y)
		}
		return y
	}
}

var headingSize = map[int]float64{1: 19, 2: 15, 3: 12.8, 4: 11.2, 5: 10.5, 6: 10}

func (d *pdfDoc) heading(n *ast.Heading, y float64) float64 {
	size := headingSize[n.Level]
	if size == 0 {
		size = pdfBodySize
	}
	y += 4
	y = d.ensure(y, lineHeight(size, 1.3)+4)
	color := colFg
	if n.Level >= 6 {
		color = colMuted
	}
	runs := d.inlines(n)
	for i := range runs {
		runs[i].st.bold = true
	}
	y = d.flow(runs, flowOpts{x: d.left, y: y, width: d.contentW(),
		size: size, factor: 1.3, color: color})
	if n.Level <= 2 {
		y += 1.5
		d.rule(y)
		y += 1
	}
	return y + 3
}

// codeBlock paints the fenced/indented code lines on a tinted panel, wrapping
// any line too wide for the column rather than letting it run off the page.
func (d *pdfDoc) codeBlock(lines *text.Segments, y float64) float64 {
	d.pdf.SetFont("Courier", "", pdfCodeSize)
	lh := lineHeight(pdfCodeSize, 1.42)
	avail := d.contentW() - 2*pdfCellPad

	var out []string
	for i := 0; i < lines.Len(); i++ {
		s := lines.At(i)
		line := strings.TrimRight(string(s.Value(d.src)), "\r\n")
		line = strings.ReplaceAll(line, "\t", "    ")
		out = append(out, d.wrapChars(cp1252(line), avail)...)
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}

	y += 1
	for i := 0; i < len(out); i++ {
		// A panel is drawn per page-run so a block spanning a page break gets
		// a background on each page.
		start := y
		if i == 0 {
			start = d.ensure(y, lh*2+2*pdfCellPad)
			y = start
		}
		rows := 0
		for i+rows < len(out) && y+lh <= pdfPageH-pdfBottom {
			y += lh
			rows++
		}
		if rows == 0 {
			d.pdf.AddPage()
			y = pdfMargin
			continue
		}
		d.pdf.SetFillColor(colCodeBg.r, colCodeBg.g, colCodeBg.b)
		d.pdf.Rect(d.left, start-pdfCellPad, d.contentW(), float64(rows)*lh+2*pdfCellPad, "F")
		d.setColor(colFg)
		d.pdf.SetFont("Courier", "", pdfCodeSize)
		ty := start
		for j := 0; j < rows; j++ {
			d.pdf.SetXY(d.left+pdfCellPad, ty)
			d.pdf.CellFormat(avail, lh, out[i+j], "", 0, "L", false, 0, "")
			ty += lh
		}
		y += pdfCellPad
		i += rows - 1
		if i+1 < len(out) {
			d.pdf.AddPage()
			y = pdfMargin
		}
	}
	if len(out) == 0 {
		return y
	}
	return y + 3.2
}

func (d *pdfDoc) blockquote(n *ast.Blockquote, y float64) float64 {
	saved := d.left
	d.left += pdfQuoteInset
	start := y
	startPage := d.pdf.PageNo()
	y = d.blocks(n, y)
	// The quote bar only tracks the part of the quote on the page it started;
	// a continued quote gets its bar from the page top.
	barTop, barBottom := start, y
	if d.pdf.PageNo() != startPage {
		barTop = pdfMargin
	}
	d.pdf.SetDrawColor(colBorder.r, colBorder.g, colBorder.b)
	d.pdf.SetLineWidth(1)
	d.pdf.Line(saved+1.5, barTop, saved+1.5, barBottom-1)
	d.left = saved
	return y
}

func (d *pdfDoc) list(n *ast.List, y float64) float64 {
	saved := d.left
	d.left += pdfIndent
	num := n.Start
	if num == 0 {
		num = 1
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		item, ok := c.(*ast.ListItem)
		if !ok {
			continue
		}
		marker := cp1252("•")
		if n.IsOrdered() {
			marker = fmt.Sprintf("%d.", num)
			num++
		}
		// A task-list checkbox replaces the bullet.
		if cb := taskCheckBox(item); cb != nil {
			if cb.IsChecked {
				marker = "[x]"
			} else {
				marker = "[ ]"
			}
		}
		y = d.ensure(y, lineHeight(pdfBodySize, 1.45))
		d.pdf.SetFont("Helvetica", "", pdfBodySize)
		d.setColor(colMuted)
		d.pdf.SetXY(saved+1, y)
		d.pdf.CellFormat(pdfIndent-1.5, lineHeight(pdfBodySize, 1.45), marker, "", 0, "L", false, 0, "")
		y = d.blocks(item, y)
		if !n.IsTight {
			y += 2
		} else {
			y += 0.8
		}
	}
	d.left = saved
	return y + 2.4
}

// taskCheckBox returns the checkbox leading a task-list item, if any.
func taskCheckBox(item *ast.ListItem) *east.TaskCheckBox {
	first := item.FirstChild()
	if first == nil {
		return nil
	}
	if cb, ok := first.FirstChild().(*east.TaskCheckBox); ok {
		return cb
	}
	return nil
}

// --- inline collection -----------------------------------------------------

// inlines flattens the inline children of n into styled runs.
func (d *pdfDoc) inlines(n ast.Node) []inlineRun {
	var out []inlineRun
	d.collect(n, inlineStyle{}, &out)
	return out
}

func (d *pdfDoc) collect(n ast.Node, st inlineStyle, out *[]inlineRun) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			*out = append(*out, inlineRun{text: cp1252(string(c.Segment.Value(d.src))), st: st})
			if c.HardLineBreak() {
				*out = append(*out, inlineRun{kind: runBreak})
			} else if c.SoftLineBreak() {
				*out = append(*out, inlineRun{text: " ", st: st})
			}
		case *ast.String:
			*out = append(*out, inlineRun{text: cp1252(string(c.Value)), st: st})
		case *ast.Emphasis:
			sub := st
			if c.Level >= 2 {
				sub.bold = true
			} else {
				sub.italic = true
			}
			d.collect(c, sub, out)
		case *east.Strikethrough:
			sub := st
			sub.strike = true
			d.collect(c, sub, out)
		case *ast.CodeSpan:
			sub := st
			sub.code = true
			d.collect(c, sub, out)
		case *ast.Link:
			sub := st
			sub.link = string(c.Destination)
			d.collect(c, sub, out)
		case *ast.AutoLink:
			url := string(c.URL(d.src))
			sub := st
			sub.link = url
			*out = append(*out, inlineRun{text: cp1252(url), st: sub})
		case *ast.Image:
			if p := d.resolveImage(string(c.Destination)); p != "" {
				*out = append(*out, inlineRun{kind: runImage, img: p})
			} else {
				// Unreachable image: fall back to its alt text.
				sub := st
				sub.italic = true
				d.collect(c, sub, out)
			}
		case *ast.RawHTML:
			// skip inline markup
		case *east.TaskCheckBox:
			// rendered as the list marker
		default:
			d.collect(c, st, out)
		}
	}
}

// resolveImage maps a markdown image destination to a readable local file,
// returning "" for remote or unsupported images.
func (d *pdfDoc) resolveImage(dest string) string {
	if strings.Contains(dest, "://") || strings.HasPrefix(dest, "data:") {
		return ""
	}
	switch strings.ToLower(filepath.Ext(dest)) {
	case ".png", ".jpg", ".jpeg", ".gif":
	default:
		return ""
	}
	p := dest
	if !filepath.IsAbs(p) {
		p = filepath.Join(d.dir, dest)
	}
	if info, err := os.Stat(p); err != nil || info.IsDir() {
		return ""
	}
	return p
}

// --- inline flow -----------------------------------------------------------

type flowOpts struct {
	x, y   float64
	width  float64
	size   float64
	factor float64 // line height multiplier
	color  rgb
	dry    bool // measure only: draw nothing and never break the page
}

// flow lays runs out as wrapped text inside the column at o.x..o.x+o.width,
// starting at o.y, and returns the y just below the last line.
func (d *pdfDoc) flow(runs []inlineRun, o flowOpts) float64 {
	lh := lineHeight(o.size, o.factor)
	x, y := o.x, o.y
	rightEdge := o.x + o.width
	lineEmpty := true
	// A separator is carried between words rather than emitted immediately, so
	// that a space spanning two runs (the gap after a link, or a soft line
	// break) is written once, and never as trailing space on a wrapped line.
	sawSpace := false

	newline := func() {
		y += lh
		x = o.x
		lineEmpty = true
		sawSpace = false
		if !o.dry {
			y = d.ensure(y, lh)
		}
	}

	for _, r := range runs {
		switch r.kind {
		case runBreak:
			newline()
			continue
		case runImage:
			if !lineEmpty {
				newline()
			}
			if !o.dry {
				y = d.image(r.img, y)
			}
			continue
		}
		d.font(r.st, o.size)
		for _, tok := range tokenize(r.text) {
			if tok == " " {
				sawSpace = true
				continue
			}
			d.font(r.st, o.size)
			sp := 0.0
			if sawSpace && !lineEmpty {
				sp = d.pdf.GetStringWidth(" ")
			}
			sawSpace = false
			w := d.pdf.GetStringWidth(tok)
			if w > o.width {
				// A single token wider than the column (a long URL or path):
				// break it across lines by character.
				for _, part := range d.wrapChars(tok, o.width) {
					if !lineEmpty {
						newline()
					}
					d.draw(part, r, x, y, lh, o)
					x += d.pdf.GetStringWidth(part)
					lineEmpty = false
				}
				continue
			}
			if !lineEmpty && x+sp+w > rightEdge {
				newline()
				sp = 0
			}
			x += sp
			d.draw(tok, r, x, y, lh, o)
			x += w
			lineEmpty = false
		}
	}
	if !lineEmpty || y == o.y {
		y += lh
	}
	return y
}

// draw paints one token with the styling of its run.
func (d *pdfDoc) draw(tok string, r inlineRun, x, y, lh float64, o flowOpts) {
	if o.dry {
		return
	}
	d.font(r.st, o.size)
	w := d.pdf.GetStringWidth(tok)
	if r.st.code {
		d.pdf.SetFillColor(colCodeBg.r, colCodeBg.g, colCodeBg.b)
		d.pdf.Rect(x-0.4, y+0.2, w+0.8, lh-0.6, "F")
	}
	color := o.color
	if r.st.link != "" {
		color = colAccent
	}
	d.setColor(color)
	d.pdf.SetXY(x, y)
	d.pdf.CellFormat(w, lh, tok, "", 0, "L", false, 0, r.st.link)
	if r.st.strike {
		d.pdf.SetDrawColor(color.r, color.g, color.b)
		d.pdf.SetLineWidth(0.2)
		mid := y + lh*0.52
		d.pdf.Line(x, mid, x+w, mid)
	}
}

// image places a local image scaled to fit the column.
func (d *pdfDoc) image(path string, y float64) float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return y
	}
	tp := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if tp == "jpeg" {
		tp = "jpg"
	}
	d.imgN++
	name := fmt.Sprintf("img%d", d.imgN)
	info := d.pdf.RegisterImageOptionsReader(name, fpdf.ImageOptions{ImageType: tp}, bytes.NewReader(data))
	if info == nil || d.pdf.Err() {
		d.pdf.ClearError()
		return y
	}
	w, h := info.Extent()
	if w <= 0 || h <= 0 {
		return y
	}
	if w > d.contentW() {
		h *= d.contentW() / w
		w = d.contentW()
	}
	// Shrink an over-tall image rather than pushing it onto a page of its own.
	if maxH := pdfPageH - pdfMargin - pdfBottom; h > maxH {
		w *= maxH / h
		h = maxH
	}
	y = d.ensure(y, h)
	d.pdf.ImageOptions(name, d.left, y, w, h, false, fpdf.ImageOptions{ImageType: tp}, 0, "")
	return y + h + 2
}

// --- tables ----------------------------------------------------------------

func (d *pdfDoc) table(n *east.Table, y float64) float64 {
	type cell struct {
		runs  []inlineRun
		align east.Alignment
	}
	var header []cell
	var body [][]cell

	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []cell
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			tc, ok := c.(*east.TableCell)
			if !ok {
				continue
			}
			cells = append(cells, cell{runs: d.inlines(tc), align: tc.Alignment})
		}
		if _, ok := row.(*east.TableHeader); ok {
			header = cells
		} else {
			body = append(body, cells)
		}
	}
	cols := len(header)
	for _, r := range body {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return y
	}

	// Natural width of each column is its widest unwrapped cell; columns are
	// then scaled down proportionally if the table overflows the page.
	natural := make([]float64, cols)
	measure := func(cells []cell, bold bool) {
		for i, c := range cells {
			if i >= cols {
				break
			}
			w := 0.0
			for _, r := range c.runs {
				st := r.st
				st.bold = st.bold || bold
				d.font(st, pdfBodySize)
				w += d.pdf.GetStringWidth(r.text)
			}
			if w+2*pdfCellPad > natural[i] {
				natural[i] = w + 2*pdfCellPad
			}
		}
	}
	measure(header, true)
	for _, r := range body {
		measure(r, false)
	}
	total := 0.0
	for _, w := range natural {
		total += w
	}
	widths := make([]float64, cols)
	avail := d.contentW()
	for i, w := range natural {
		if total > avail {
			widths[i] = w / total * avail
		} else {
			widths[i] = w + (avail-total)/float64(cols)
		}
	}

	lh := lineHeight(pdfBodySize, 1.35)
	// rowHeight measures the tallest cell in a row without drawing.
	rowHeight := func(cells []cell, bold bool) float64 {
		h := lh
		for i, c := range cells {
			if i >= cols {
				break
			}
			runs := c.runs
			if bold {
				runs = boldRuns(runs)
			}
			ch := d.flow(runs, flowOpts{x: 0, y: 0, width: widths[i] - 2*pdfCellPad,
				size: pdfBodySize, factor: 1.35, color: colFg, dry: true})
			if ch > h {
				h = ch
			}
		}
		return h + 2*pdfCellPad
	}

	drawRow := func(cells []cell, y, h float64, bold, fill bool) {
		x := d.left
		for i := 0; i < cols; i++ {
			if fill {
				d.pdf.SetFillColor(colCodeBg.r, colCodeBg.g, colCodeBg.b)
				d.pdf.Rect(x, y, widths[i], h, "F")
			}
			d.pdf.SetDrawColor(colBorder.r, colBorder.g, colBorder.b)
			d.pdf.SetLineWidth(0.2)
			d.pdf.Rect(x, y, widths[i], h, "D")
			if i < len(cells) {
				runs := cells[i].runs
				if bold {
					runs = boldRuns(runs)
				}
				cw := widths[i] - 2*pdfCellPad
				cx := x + pdfCellPad
				// Right/centre alignment is applied by shifting the whole
				// block, measured dry, within the cell.
				if a := cells[i].align; a == east.AlignRight || a == east.AlignCenter {
					used := d.textWidth(runs, pdfBodySize)
					if used < cw {
						if a == east.AlignRight {
							cx += cw - used
						} else {
							cx += (cw - used) / 2
						}
						cw = used
					}
				}
				d.flow(runs, flowOpts{x: cx, y: y + pdfCellPad, width: cw,
					size: pdfBodySize, factor: 1.35, color: colFg})
			}
			x += widths[i]
		}
	}

	y += 1
	headerH := 0.0
	if len(header) > 0 {
		headerH = rowHeight(header, true)
		y = d.ensure(y, headerH+lh)
		drawRow(header, y, headerH, true, true)
		y += headerH
	}
	for _, r := range body {
		h := rowHeight(r, false)
		if ny := d.ensure(y, h); ny != y {
			y = ny
			// Repeat the header at the top of each continued page.
			if len(header) > 0 {
				drawRow(header, y, headerH, true, true)
				y += headerH
			}
		}
		drawRow(r, y, h, false, false)
		y += h
	}
	return y + 3.2
}

func boldRuns(runs []inlineRun) []inlineRun {
	out := make([]inlineRun, len(runs))
	copy(out, runs)
	for i := range out {
		out[i].st.bold = true
	}
	return out
}

// textWidth is the unwrapped width of runs, used for cell alignment.
func (d *pdfDoc) textWidth(runs []inlineRun, size float64) float64 {
	w := 0.0
	for _, r := range runs {
		d.font(r.st, size)
		w += d.pdf.GetStringWidth(r.text)
	}
	return w
}

// --- text helpers ----------------------------------------------------------

// tokenize splits s into words and single-space separators, so the flow loop
// can decide where lines break.
func tokenize(s string) []string {
	var out []string
	var word strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			if word.Len() > 0 {
				out = append(out, word.String())
				word.Reset()
			}
			// A leading separator is kept: it may be the space that follows a
			// link or other styled span, which arrives as its own run.
			if len(out) == 0 || out[len(out)-1] != " " {
				out = append(out, " ")
			}
		default:
			word.WriteByte(s[i])
		}
	}
	if word.Len() > 0 {
		out = append(out, word.String())
	}
	return out
}

// wrapChars splits s into pieces no wider than maxW using the current font.
func (d *pdfDoc) wrapChars(s string, maxW float64) []string {
	if s == "" {
		return []string{""}
	}
	var out []string
	start, w := 0, 0.0
	for i := 0; i < len(s); i++ {
		cw := d.pdf.GetStringWidth(s[i : i+1])
		if w+cw > maxW && i > start {
			out = append(out, s[start:i])
			start, w = i, 0
		}
		w += cw
	}
	return append(out, s[start:])
}

// cp1252High maps the printable runes that occupy 0x80-0x9F in cp1252.
var cp1252High = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84,
	'…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88,
	'‰': 0x89, 'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C,
	'Ž': 0x8E, '‘': 0x91, '’': 0x92, '“': 0x93,
	'”': 0x94, '•': 0x95, '–': 0x96, '—': 0x97,
	'˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B,
	'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F,
}

// cp1252Fold transliterates common runes with no cp1252 code point.
var cp1252Fold = map[rune]string{
	'→': "->", '←': "<-", '⇒': "=>", '⇐': "<=",
	'≤': "<=", '≥': ">=", '≠': "!=", '≈': "~=",
	'−': "-", '‐': "-", '‑': "-", '‒': "-",
	' ': " ", ' ': " ", ' ': " ", '​': "",
	'′': "'", '″': "\"", '─': "-", '│': "|",
}

// cp1252 encodes a UTF-8 string for the PDF core fonts, which are single-byte.
// Runes outside the repertoire are transliterated when there is an obvious
// equivalent and replaced with '?' otherwise.
func cp1252(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b []byte
	for _, r := range s {
		switch {
		case r < 0x80:
			b = append(b, byte(r))
		case r >= 0xA0 && r <= 0xFF:
			b = append(b, byte(r))
		default:
			if c, ok := cp1252High[r]; ok {
				b = append(b, c)
			} else if f, ok := cp1252Fold[r]; ok {
				b = append(b, f...)
			} else {
				b = append(b, '?')
			}
		}
	}
	return string(b)
}
