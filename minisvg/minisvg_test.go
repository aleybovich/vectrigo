package minisvg

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func mustWrite(t *testing.T, d *Document) string {
	t.Helper()
	var sb strings.Builder
	if _, err := d.WriteTo(&sb); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return sb.String()
}

func mustWriteOpts(t *testing.T, d *Document, opt WriteOptions) string {
	t.Helper()
	var sb strings.Builder
	if _, err := d.WriteToOpts(&sb, opt); err != nil {
		t.Fatalf("WriteToOpts: %v", err)
	}
	return sb.String()
}

func TestNewDocumentDefaultViewBox(t *testing.T) {
	doc := New(100, 50)
	got := mustWrite(t, doc)
	want := "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"100\" height=\"50\" viewBox=\"0 0 100 50\">\n</svg>"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestSetViewBoxOverride(t *testing.T) {
	doc := New(100, 100)
	doc.SetViewBox(1, 2, 300.5, 400)
	got := mustWrite(t, doc)
	want := "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"100\" height=\"100\" viewBox=\"1 2 300.5 400\">\n</svg>"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestSetViewBoxReturnsDocumentForChaining(t *testing.T) {
	doc := New(10, 10)
	if doc.SetViewBox(0, 0, 5, 5) != doc {
		t.Errorf("SetViewBox did not return the same *Document")
	}
}

func TestDocumentPathExactOutput(t *testing.T) {
	doc := New(10, 10)
	doc.Path("M0 0 L10 0 L10 10 Z", "#ff0000")
	got := mustWrite(t, doc)
	want := "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"10\" height=\"10\" viewBox=\"0 0 10 10\">\n" +
		"  <path d=\"M0 0 L10 0 L10 10 Z\" fill=\"#ff0000\"/>\n" +
		"</svg>"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestPathChaining(t *testing.T) {
	doc := New(10, 10)
	ret := doc.Path("M0 0 Z", "red").Path("M1 1 Z", "blue")
	if ret != doc {
		t.Fatalf("Path did not return the same *Document")
	}
	got := mustWrite(t, doc)
	if strings.Count(got, "<path") != 2 {
		t.Errorf("expected 2 <path> elements, got:\n%s", got)
	}
}

func TestPathEmptyFillOmitsAttribute(t *testing.T) {
	doc := New(10, 10)
	doc.Path("M0 0 Z", "")
	got := mustWrite(t, doc)
	if strings.Contains(got, "fill=") {
		t.Errorf("expected no fill attribute, got:\n%s", got)
	}
	if !strings.Contains(got, `<path d="M0 0 Z"/>`) {
		t.Errorf("expected self-closed path without fill, got:\n%s", got)
	}
}

func TestGroupNesting(t *testing.T) {
	doc := New(20, 20)
	g := doc.Group("blue")
	g.Path("M0 0 Z", "red")
	g2 := g.Group("")
	g2.Path("M1 1 Z", "green")

	got := mustWrite(t, doc)
	want := "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"20\" height=\"20\" viewBox=\"0 0 20 20\">\n" +
		"  <g fill=\"blue\">\n" +
		"    <path d=\"M0 0 Z\" fill=\"red\"/>\n" +
		"    <g>\n" +
		"      <path d=\"M1 1 Z\" fill=\"green\"/>\n" +
		"    </g>\n" +
		"  </g>\n" +
		"</svg>"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestGroupChaining(t *testing.T) {
	doc := New(10, 10)
	g := doc.Group("black")
	ret := g.Path("M0 0 Z", "red")
	if ret != g {
		t.Fatalf("Group.Path did not return the same *Group")
	}
}

func TestGroupReturnedFromDocumentIsIndependent(t *testing.T) {
	doc := New(10, 10)
	g1 := doc.Group("a")
	g2 := doc.Group("b")
	g1.Path("M0 0 Z", "red")
	g2.Path("M1 1 Z", "blue")

	got := mustWrite(t, doc)
	if strings.Index(got, "fill=\"a\"") > strings.Index(got, "fill=\"b\"") {
		t.Errorf("groups not written in append order:\n%s", got)
	}
}

func TestMultipleTopLevelChildrenPreserveOrder(t *testing.T) {
	doc := New(10, 10)
	doc.Path("M0 0 Z", "first")
	doc.Group("second")
	doc.Path("M1 1 Z", "third")

	got := mustWrite(t, doc)
	iFirst := strings.Index(got, "first")
	iSecond := strings.Index(got, "second")
	iThird := strings.Index(got, "third")
	if !(iFirst < iSecond && iSecond < iThird) {
		t.Errorf("document order not preserved:\n%s", got)
	}
}

func TestXMLEscapingInFillAttribute(t *testing.T) {
	doc := New(10, 10)
	doc.Path("M0 0 Z", Color(`a&b<c>d"e'f`))
	got := mustWrite(t, doc)
	if !strings.Contains(got, `fill="a&amp;b&lt;c&gt;d&quot;e&apos;f"`) {
		t.Errorf("attribute value not correctly escaped, got:\n%s", got)
	}
}

func TestXMLEscapingInPathData(t *testing.T) {
	doc := New(10, 10)
	doc.Path(`M0 0 & <weird> "data"`, "red")
	got := mustWrite(t, doc)
	if !strings.Contains(got, `d="M0 0 &amp; &lt;weird&gt; &quot;data&quot;"`) {
		t.Errorf("d attribute value not correctly escaped, got:\n%s", got)
	}
}

func TestXMLEscapingInGroupFill(t *testing.T) {
	doc := New(10, 10)
	g := doc.Group(Color(`x&y`))
	g.Path("M0 0 Z", "red")
	got := mustWrite(t, doc)
	if !strings.Contains(got, `<g fill="x&amp;y">`) {
		t.Errorf("group fill not correctly escaped, got:\n%s", got)
	}
}

func TestEscapeAttrDoesNotDoubleEscape(t *testing.T) {
	got := escapeAttr("&amp;")
	want := "&amp;amp;"
	if got != want {
		t.Errorf("escapeAttr(%q) = %q, want %q", "&amp;", got, want)
	}
}

func TestPathBuilderRectangle(t *testing.T) {
	pb := new(PathBuilder).
		MoveTo(0, 0).
		LineTo(100, 0).
		LineTo(100, 100).
		LineTo(0, 100).
		Close()
	got := pb.String()
	want := "M0 0 L100 0 L100 100 L0 100 Z"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPathBuilderCubic(t *testing.T) {
	pb := new(PathBuilder).
		MoveTo(0, 0).
		LineTo(10, 0).
		CubicTo(15, 0, 20, 5, 20, 10).
		Close()
	got := pb.String()
	want := "M0 0 L10 0 C15 0 20 5 20 10 Z"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPathBuilderFloatFormatting(t *testing.T) {
	pb := new(PathBuilder).MoveTo(1.5, -2.25).LineTo(-0.0, 3)
	got := pb.String()
	want := "M1.5 -2.25 L0 3"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPathBuilderZeroValueUsable(t *testing.T) {
	var pb PathBuilder
	pb.MoveTo(0, 0).Close()
	if pb.String() != "M0 0 Z" {
		t.Errorf("zero-value PathBuilder failed: %q", pb.String())
	}
}

func TestPathBuilderChainingReturnsSamePointer(t *testing.T) {
	pb := new(PathBuilder)
	if pb.MoveTo(0, 0) != pb {
		t.Error("MoveTo did not return same pointer")
	}
	if pb.LineTo(1, 1) != pb {
		t.Error("LineTo did not return same pointer")
	}
	if pb.CubicTo(1, 1, 2, 2, 3, 3) != pb {
		t.Error("CubicTo did not return same pointer")
	}
	if pb.Close() != pb {
		t.Error("Close did not return same pointer")
	}
}

func TestMinifyStripsWhitespace(t *testing.T) {
	doc := New(10, 10)
	doc.Path("M0 0 L10 0 L10 10 Z", "#000")
	got := mustWriteOpts(t, doc, WriteOptions{Minify: true, Precision: -1})
	want := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 10 10"><path d="M0 0 L10 0 L10 10 Z" fill="#000"/></svg>`
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	if strings.ContainsAny(got, "\n\t") {
		t.Errorf("minified output still contains whitespace/newlines: %q", got)
	}
}

func TestMinifyWithNestedGroups(t *testing.T) {
	doc := New(5, 5)
	g := doc.Group("blue")
	g.Path("M0 0 Z", "red")
	got := mustWriteOpts(t, doc, WriteOptions{Minify: true, Precision: -1})
	want := `<svg xmlns="http://www.w3.org/2000/svg" width="5" height="5" viewBox="0 0 5 5"><g fill="blue"><path d="M0 0 Z" fill="red"/></g></svg>`
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestNonMinifiedHasNoTrailingNewline(t *testing.T) {
	doc := New(1, 1)
	got := mustWrite(t, doc)
	if strings.HasSuffix(got, "\n") {
		t.Errorf("expected no trailing newline, got %q", got)
	}
}

func TestWriteToOptsRoundsPathData(t *testing.T) {
	doc := New(10, 10)
	doc.Path("M12.345678 0 L-12.996 5.005", "#000")
	got := mustWriteOpts(t, doc, WriteOptions{Minify: true, Precision: 2})
	want := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 10 10"><path d="M12.35 0 L-13 5.01" fill="#000"/></svg>`
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestWriteToOptsRoundsViewBox(t *testing.T) {
	doc := New(10, 10)
	doc.SetViewBox(0, 0, 10.126, 10.124)
	got := mustWriteOpts(t, doc, WriteOptions{Minify: true, Precision: 2})
	if !strings.Contains(got, `viewBox="0 0 10.13 10.12"`) {
		t.Errorf("viewBox not rounded correctly, got:\n%s", got)
	}
}

func TestPrecisionNegativeDisablesRounding(t *testing.T) {
	doc := New(10, 10)
	doc.Path("M12.345678 0", "#000")
	got := mustWriteOpts(t, doc, WriteOptions{Minify: true, Precision: -1})
	if !strings.Contains(got, `d="M12.345678 0"`) {
		t.Errorf("expected unrounded coordinates, got:\n%s", got)
	}
}

func TestRoundNumbers(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		precision int
		want      string
	}{
		{"basic truncation example from spec", "12.345678", 2, "12.35"},
		{"negative coordinate", "-12.345678", 2, "-12.35"},
		{"round up carries through nines", "12.996", 2, "13"},
		{"round down keeps value", "12.994", 2, "12.99"},
		{"round half up at precision 0", "0.5", 0, "1"},
		{"round half away from zero, negative", "-0.5", 0, "-1"},
		{"round half up integer boundary", "2.5", 0, "3"},
		{"round half away from zero integer boundary, negative", "-2.5", 0, "-3"},
		{"just under half rounds down", "2.4999", 0, "2"},
		{"just over half rounds up", "2.5001", 0, "3"},
		{"small negative rounds to zero, sign dropped", "-0.001", 2, "0"},
		{"precision larger than input keeps value", "1.2", 5, "1.2"},
		{"trailing zero trimmed even without rounding", "1.20", 5, "1.2"},
		{"precision 0 drops decimal point entirely", "12.4", 0, "12"},
		{"precision 0 rounds up and drops decimal point", "12.6", 0, "13"},
		{"multiple numbers in one string", "M5 10.256 L20.004 3", 2, "M5 10.26 L20 3"},
		{"zero stays zero", "0.0", 2, "0"},
		{"exact precision, no trailing digits to inspect", "3.14", 2, "3.14"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := roundNumbers(tc.in, tc.precision)
			if got != tc.want {
				t.Errorf("roundNumbers(%q, %d) = %q, want %q", tc.in, tc.precision, got, tc.want)
			}
		})
	}
}

func TestRoundNumbersLeavesIntegersAlone(t *testing.T) {
	in := "M0 0 L100 0 L100 100 L0 100 Z"
	got := roundNumbers(in, 0)
	if got != in {
		t.Errorf("integers should be untouched: got %q, want %q", got, in)
	}
}

func TestApplyPrecisionOnlyAffectsDAndViewBox(t *testing.T) {
	got := applyPrecision("12.345678", "fill", 2)
	if got != "12.345678" {
		t.Errorf("applyPrecision should not touch non-d/viewBox attrs, got %q", got)
	}
	got = applyPrecision("12.345678", "d", 2)
	if got != "12.35" {
		t.Errorf("applyPrecision should round d attrs, got %q", got)
	}
	got = applyPrecision("12.345678", "viewBox", 2)
	if got != "12.35" {
		t.Errorf("applyPrecision should round viewBox attrs, got %q", got)
	}
}

func TestApplyPrecisionNegativeIsNoOp(t *testing.T) {
	got := applyPrecision("12.345678", "d", -1)
	if got != "12.345678" {
		t.Errorf("negative precision should disable rounding, got %q", got)
	}
}

func TestStrokedPathExactOutput(t *testing.T) {
	doc := New(10, 10)
	doc.StrokedPath("M0 0 L10 0 Z", "#ff0000", "#00ff00", 1.5)
	got := mustWrite(t, doc)
	want := "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"10\" height=\"10\" viewBox=\"0 0 10 10\">\n" +
		"  <path d=\"M0 0 L10 0 Z\" fill=\"#ff0000\" stroke=\"#00ff00\" stroke-width=\"1.5\"/>\n" +
		"</svg>"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestStrokedPathReturnsDocumentForChaining(t *testing.T) {
	doc := New(10, 10)
	if doc.StrokedPath("M0 0 Z", "red", "black", 1) != doc {
		t.Errorf("StrokedPath did not return the same *Document")
	}
}

func TestStrokedPathStrokeOnlyEmitsFillNone(t *testing.T) {
	doc := New(10, 10)
	doc.StrokedPath("M0 0 L10 0 Z", "none", "#000", 2)
	got := mustWrite(t, doc)
	if !strings.Contains(got, `fill="none"`) {
		t.Errorf("expected fill=\"none\", got:\n%s", got)
	}
	if !strings.Contains(got, `stroke="#000"`) || !strings.Contains(got, `stroke-width="2"`) {
		t.Errorf("expected stroke attrs, got:\n%s", got)
	}
}

func TestStrokedPathEmptyStrokeOmitsStrokeAttrs(t *testing.T) {
	// With an empty stroke color, output must be byte-identical to Path
	// (backward-compat: no stroke/stroke-width attributes emitted).
	stroked := New(10, 10)
	stroked.StrokedPath("M0 0 L10 0 Z", "#ff0000", "", 3)
	gotStroked := mustWrite(t, stroked)

	plain := New(10, 10)
	plain.Path("M0 0 L10 0 Z", "#ff0000")
	gotPlain := mustWrite(t, plain)

	if gotStroked != gotPlain {
		t.Errorf("empty-stroke StrokedPath not identical to Path:\nstroked=%q\nplain=%q", gotStroked, gotPlain)
	}
	if strings.Contains(gotStroked, "stroke") {
		t.Errorf("expected no stroke attributes, got:\n%s", gotStroked)
	}
}

func TestStrokedPathEmptyFillOmitsFillAttr(t *testing.T) {
	doc := New(10, 10)
	doc.StrokedPath("M0 0 Z", "", "#000", 1)
	got := mustWrite(t, doc)
	if strings.Contains(got, "fill=") {
		t.Errorf("expected no fill attribute, got:\n%s", got)
	}
	if !strings.Contains(got, `stroke="#000"`) {
		t.Errorf("expected stroke attribute, got:\n%s", got)
	}
}

func TestStrokedPathWidthRoundsUnderPrecision(t *testing.T) {
	doc := New(10, 10)
	doc.StrokedPath("M0 0 Z", "none", "#000", 1.23456)
	got := mustWriteOpts(t, doc, WriteOptions{Minify: true, Precision: 2})
	if !strings.Contains(got, `stroke-width="1.23"`) {
		t.Errorf("expected stroke-width rounded to 1.23, got:\n%s", got)
	}
}

func TestStrokedPathWidthUnroundedByDefault(t *testing.T) {
	doc := New(10, 10)
	doc.StrokedPath("M0 0 Z", "none", "#000", 1.23456)
	got := mustWrite(t, doc)
	if !strings.Contains(got, `stroke-width="1.23456"`) {
		t.Errorf("expected unrounded stroke-width, got:\n%s", got)
	}
}

func TestGroupStrokedPath(t *testing.T) {
	doc := New(10, 10)
	g := doc.Group("")
	if g.StrokedPath("M0 0 Z", "red", "blue", 2) != g {
		t.Errorf("Group.StrokedPath did not return the same *Group")
	}
	got := mustWrite(t, doc)
	if !strings.Contains(got, `<path d="M0 0 Z" fill="red" stroke="blue" stroke-width="2"/>`) {
		t.Errorf("group stroked path output unexpected, got:\n%s", got)
	}
}

func TestSetBackgroundExactOutput(t *testing.T) {
	doc := New(100, 50)
	doc.SetBackground("#808080")
	doc.Path("M0 0 L10 0 Z", "#ff0000")
	got := mustWrite(t, doc)
	want := "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"100\" height=\"50\" viewBox=\"0 0 100 50\">\n" +
		"  <rect width=\"100\" height=\"50\" fill=\"#808080\"/>\n" +
		"  <path d=\"M0 0 L10 0 Z\" fill=\"#ff0000\"/>\n" +
		"</svg>"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestSetBackgroundRendersFirstEvenWhenSetLast(t *testing.T) {
	doc := New(20, 20)
	doc.Path("M0 0 Z", "#111")
	doc.Group("#222")
	doc.SetBackground("#eee") // set after content
	got := mustWrite(t, doc)
	iRect := strings.Index(got, "<rect")
	iPath := strings.Index(got, "<path")
	iGroup := strings.Index(got, "<g ")
	if iRect < 0 || !(iRect < iPath && iRect < iGroup) {
		t.Errorf("background rect not rendered first, got:\n%s", got)
	}
}

func TestSetBackgroundReplacesNotDuplicates(t *testing.T) {
	doc := New(10, 10)
	doc.SetBackground("#111").SetBackground("#222")
	got := mustWrite(t, doc)
	if strings.Count(got, "<rect") != 1 {
		t.Errorf("expected exactly one background rect, got:\n%s", got)
	}
	if !strings.Contains(got, `fill="#222"`) || strings.Contains(got, `fill="#111"`) {
		t.Errorf("expected background replaced with #222, got:\n%s", got)
	}
}

func TestSetBackgroundReturnsDocumentForChaining(t *testing.T) {
	doc := New(10, 10)
	if doc.SetBackground("#000") != doc {
		t.Errorf("SetBackground did not return the same *Document")
	}
}

func TestSetBackgroundEmptyFillOmitsFillAttr(t *testing.T) {
	doc := New(10, 10)
	doc.SetBackground("")
	got := mustWrite(t, doc)
	if !strings.Contains(got, `<rect width="10" height="10"/>`) {
		t.Errorf("expected rect without fill, got:\n%s", got)
	}
}

func TestNoBackgroundIsUnchanged(t *testing.T) {
	doc := New(10, 10)
	doc.Path("M0 0 L10 0 L10 10 Z", "#ff0000")
	got := mustWrite(t, doc)
	if strings.Contains(got, "<rect") {
		t.Errorf("document without SetBackground should have no rect, got:\n%s", got)
	}
}

func TestSetBackgroundMinified(t *testing.T) {
	doc := New(5, 5)
	doc.SetBackground("#abc")
	doc.Path("M0 0 Z", "red")
	got := mustWriteOpts(t, doc, WriteOptions{Minify: true, Precision: -1})
	want := `<svg xmlns="http://www.w3.org/2000/svg" width="5" height="5" viewBox="0 0 5 5"><rect width="5" height="5" fill="#abc"/><path d="M0 0 Z" fill="red"/></svg>`
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestStrokedAndBackgroundWellFormedXML(t *testing.T) {
	doc := New(30, 30)
	doc.SetBackground("#202020")
	doc.StrokedPath("M0 0 L30 0 L30 30 Z", "#ff0000", "#ff0000", 0.5)
	g := doc.Group("")
	g.StrokedPath("M5 5 L10 5 Z", "none", "#00ff00", 1.25)

	for _, opt := range []WriteOptions{
		{Minify: false, Precision: -1},
		{Minify: true, Precision: 2},
	} {
		out := mustWriteOpts(t, doc, opt)
		dec := xml.NewDecoder(strings.NewReader(out))
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("output is not well-formed XML (%+v): %v\n%s", opt, err, out)
				break
			}
		}
	}
}

func TestSetBackgroundCoversViewBox(t *testing.T) {
	// With a custom viewBox larger than the pixel dims, the background rect must
	// be sized from the viewBox so it fully covers the user-space canvas.
	var sb strings.Builder
	New(100, 100).SetViewBox(0, 0, 200, 200).SetBackground("#000").render(&sb, WriteOptions{Precision: -1})
	out := sb.String()
	if !strings.Contains(out, `<rect width="200" height="200" fill="#000"`) {
		t.Fatalf("viewBox background rect not sized from viewBox: %s", out)
	}
	// Offset viewBox: x/y must be emitted so the rect covers the offset canvas.
	sb.Reset()
	New(100, 100).SetViewBox(-50, -50, 100, 100).SetBackground("#000").render(&sb, WriteOptions{Precision: -1})
	out = sb.String()
	if !strings.Contains(out, `<rect x="-50" y="-50" width="100" height="100" fill="#000"`) {
		t.Fatalf("offset viewBox background rect wrong: %s", out)
	}
	// No viewBox: unchanged, sized from pixel dims (byte-compat).
	sb.Reset()
	New(100, 100).SetBackground("#000").render(&sb, WriteOptions{Precision: -1})
	if !strings.Contains(sb.String(), `<rect width="100" height="100" fill="#000"`) {
		t.Fatalf("default background rect changed: %s", sb.String())
	}
}

func TestSetShapeRendering(t *testing.T) {
	var sb strings.Builder
	New(10, 10).SetShapeRendering("crispEdges").Path("M0 0 L1 1 Z", "#000").render(&sb, WriteOptions{Precision: -1})
	if !strings.Contains(sb.String(), `viewBox="0 0 10 10" shape-rendering="crispEdges">`) {
		t.Fatalf("shape-rendering not emitted correctly: %s", sb.String())
	}
	// default: no attribute
	sb.Reset()
	New(10, 10).Path("M0 0 L1 1 Z", "#000").render(&sb, WriteOptions{Precision: -1})
	if strings.Contains(sb.String(), "shape-rendering") {
		t.Fatalf("shape-rendering emitted when unset: %s", sb.String())
	}
}
