package pipeline

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/antongulenko/go-bitflow"
	"github.com/antongulenko/golib"
	"github.com/gonum/plot"
	"github.com/gonum/plot/plotter"
	"github.com/gonum/plot/plotutil"
	"github.com/gonum/plot/vg"
	"github.com/gonum/plot/vg/draw"
	"github.com/lucasb-eyer/go-colorful"
)

const (
	PlotAxisTime = -1
	PlotAxisAuto = -2
	minAxis      = PlotAxisAuto

	PlotWidth  = 20 * vg.Centimeter
	PlotHeight = PlotWidth

	numColors      = 100
	plotTimeFormat = "02.01.2006 15:04:05"
	plotTimeLabel  = "time"

	ScatterPlot = PlotType(iota)
	LinePlot
	LinePointPlot
	InvalidPlotType
)

type PlotType uint

type PlotProcessor struct {
	bitflow.AbstractProcessor
	checker bitflow.HeaderChecker

	Type          PlotType
	NoLegend      bool
	AxisX         int
	AxisY         int
	OutputFile    string
	ColorTag      string
	SeparatePlots bool // If true, every ColorTag value will create a new plot

	data         map[string]plotter.XYs
	x, y         int
	xName, yName string
}

func (p *PlotProcessor) Start(wg *sync.WaitGroup) golib.StopChan {
	if p.Type >= InvalidPlotType {
		return golib.NewStoppedChan(fmt.Errorf("Invalid PlotType: %v", p.Type))
	}
	if p.OutputFile == "" {
		return golib.NewStoppedChan(errors.New("Plotter.OutputFile must be configured"))
	}
	if p.AxisX < minAxis || p.AxisY < minAxis {
		return golib.NewStoppedChan(fmt.Errorf("Invalid plot axis values: X=%v Y=%v", p.AxisX, p.AxisY))
	}
	p.data = make(map[string]plotter.XYs)

	if file, err := os.Create(p.OutputFile); err != nil {
		// Check if file can be created to quickly fail
		return golib.NewStoppedChan(err)
	} else {
		_ = file.Close() // Drop error
	}
	return p.AbstractProcessor.Start(wg)
}

func (p *PlotProcessor) Sample(sample *bitflow.Sample, header *bitflow.Header) error {
	if err := p.Check(sample, header); err != nil {
		return err
	}
	if p.checker.HeaderChanged(header) {
		if err := p.headerChanged(header); err != nil {
			return err
		}
	}
	p.storeSample(sample)
	return p.OutgoingSink.Sample(sample, header)
}

func (p *PlotProcessor) headerChanged(header *bitflow.Header) error {
	p.x = p.AxisX
	p.y = p.AxisY
	if p.x == PlotAxisAuto {
		if len(header.Fields) > 1 {
			p.x = 0
		} else {
			p.x = PlotAxisTime
		}
	}
	if p.y == PlotAxisAuto {
		if len(header.Fields) > 1 {
			p.y = 1
		} else {
			p.y = 0
		}
	}

	max := p.x
	if p.y > p.x {
		max = p.y
	}
	if len(header.Fields) <= max {
		return fmt.Errorf("%v: Header has %v fields, cannot plot with X=%v and Y=%v", p, len(header.Fields), p.x, p.y)
	}

	var xName, yName string
	if p.x < 0 {
		xName = plotTimeLabel
	} else {
		xName = header.Fields[p.x]
	}
	if p.y < 0 {
		yName = plotTimeLabel
	} else {
		yName = header.Fields[p.y]
	}

	if p.xName == "" && p.yName == "" {
		p.xName = xName
		p.yName = yName
	} else if p.xName != xName || p.yName != yName {
		return fmt.Errorf("%v: Header updated and changed the X/Y metric names from %v, %v -> %v, %v", p.xName, p.yName, xName, yName)
	}
	return nil
}

func (p *PlotProcessor) storeSample(sample *bitflow.Sample) {
	key := ""
	if p.ColorTag != "" {
		key = sample.Tag(p.ColorTag)
		if key == "" {
			key = "(none)"
		}
	}

	var x, y float64
	if p.x < 0 {
		x = float64(sample.Time.Unix())
	} else {
		x = float64(sample.Values[p.x])
	}
	if p.y < 0 {
		y = float64(sample.Time.Unix())
	} else {
		y = float64(sample.Values[p.y])
	}

	p.data[key] = append(p.data[key], struct{ X, Y float64 }{x, y})
}

func (p *PlotProcessor) Close() {
	if p.Type >= InvalidPlotType || p.OutputFile == "" {
		return
	}

	defer p.CloseSink()
	if p.checker.LastHeader == nil {
		log.Warnf("%s: No data received for plotting", p)
		return
	}
	plot := Plot{
		LabelX:   p.xName,
		LabelY:   p.yName,
		Type:     p.Type,
		NoLegend: p.NoLegend,
	}
	var err error
	if p.SeparatePlots {
		_ = os.Remove(p.OutputFile) // Delete file created in Start(), drop error.
		err = plot.saveSeparatePlots(p.data, p.OutputFile)
	} else {
		err = plot.savePlot(p.data, nil, p.OutputFile)
	}
	if err != nil {
		p.Error(err)
	}
}

func (p *PlotProcessor) String() string {
	color := "not colored"
	if p.ColorTag != "" {
		color = "color: " + p.ColorTag
	}
	file := p.OutputFile
	if p.SeparatePlots {
		file = "separate files: " + file
	} else {
		file = "file: " + file
	}
	return fmt.Sprintf("Plotter (%s)(%s)", color, file)
}

// ================================= Plot =================================

type Plot struct {
	LabelX, LabelY string
	Type           PlotType
	NoLegend       bool
}

func (p *Plot) saveSeparatePlots(plotData map[string]plotter.XYs, targetFile string) error {
	bounds, err := p.createPlot(plotData, nil)
	if err != nil {
		return err
	}
	group := bitflow.NewFileGroup(targetFile)
	for name, data := range plotData {
		plotData := map[string]plotter.XYs{name: data}
		plotFile := group.BuildFilenameStr(name)
		if err := p.savePlot(plotData, bounds, plotFile); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plot) savePlot(plotData map[string]plotter.XYs, copyBounds *plot.Plot, targetFile string) error {
	plot, err := p.createPlot(plotData, copyBounds)
	if err != nil {
		return err
	}
	err = plot.Save(PlotWidth, PlotHeight, targetFile)
	if err != nil {
		err = errors.New("Error saving plot: " + err.Error())
	}
	return err
}

func (p *Plot) createPlot(plotData map[string]plotter.XYs, copyBounds *plot.Plot) (*plot.Plot, error) {
	plot, err := plot.New()
	if err != nil {
		return nil, errors.New("Error creating new plot: " + err.Error())
	}
	if copyBounds != nil {
		plot.X.Min = copyBounds.X.Min
		plot.X.Max = copyBounds.X.Max
		plot.Y.Min = copyBounds.Y.Min
		plot.Y.Max = copyBounds.Y.Max
	}
	p.configureAxes(plot)
	return plot, p.fillPlot(plot, plotData)
}

func (p *Plot) configureAxes(plt *plot.Plot) {
	plt.X.Label.Text = p.LabelX
	plt.Y.Label.Text = p.LabelY
	if p.LabelX == plotTimeLabel {
		plt.X.Tick.Marker = plot.TimeTicks{Format: plotTimeFormat}
	}
	if p.LabelY == plotTimeLabel {
		plt.Y.Tick.Marker = plot.TimeTicks{Format: plotTimeFormat}
	}
}

func (p *Plot) fillPlot(plot *plot.Plot, plotData map[string]plotter.XYs) error {
	shape, err := NewPlotShapeGenerator(numColors)
	if err != nil {
		return err
	}

	for name, data := range plotData {
		var line *plotter.Line
		var scatter *plotter.Scatter

		switch p.Type {
		case ScatterPlot:
			scatter, err = plotter.NewScatter(data)
		case LinePlot:
			line, err = plotter.NewLine(data)
		case LinePointPlot:
			line, scatter, err = plotter.NewLinePoints(data)
		default:
			return fmt.Errorf("Invalid PlotType: %v", p.Type)
		}

		if err != nil {
			return fmt.Errorf("Error creating plot (type %v): %v", p.Type, err)
		}
		color := shape.Colors.Next()
		legend := name != "" && !p.NoLegend
		if line != nil {
			line.Color = color
			line.Dashes = shape.Dashes.Next()
			plot.Add(line)
			if legend {
				plot.Legend.Add(name, line)
				legend = false
			}
		}
		if scatter != nil {
			scatter.Color = color
			scatter.Shape = shape.Glyphs.Next()
			plot.Add(scatter)
			if legend && line == nil {
				plot.Legend.Add(name, scatter)
				legend = false
			}
		}
	}
	return nil
}

// ================================= Random Colors/Shapes =================================

type PlotShapeGenerator struct {
	Colors *ColorGenerator
	Glyphs *GlyphGenerator
	Dashes *DashesGenerator
}

func NewPlotShapeGenerator(numColors int) (*PlotShapeGenerator, error) {
	colors, err := NewColorGenerator(numColors)
	if err != nil {
		return nil, fmt.Errorf("Failed to generate %v colors: %v", numColors, err)
	}
	return &PlotShapeGenerator{
		Colors: colors,
		Glyphs: NewGlyphGenerator(),
		Dashes: NewDashesGenerator(),
	}, nil
}

type ColorGenerator struct {
	palette []color.Color
	next    int
}

func NewColorGenerator(numColors int) (*ColorGenerator, error) {
	if numColors < 1 {
		numColors = 1
	}
	palette, err := colorful.HappyPalette(numColors)
	if err != nil {
		return nil, err
	}
	colors := make([]color.Color, len(palette))
	for i, c := range palette {
		colors[i] = c
	}
	return &ColorGenerator{
		palette: colors,
	}, nil
}

func (g *ColorGenerator) Next() color.Color {
	if g.next >= len(g.palette) {
		g.next = 0
	}
	color := g.palette[g.next]
	g.next++
	return color
}

type GlyphGenerator struct {
	glyphs []draw.GlyphDrawer
	next   int
}

func NewGlyphGenerator() *GlyphGenerator {
	return &GlyphGenerator{
		glyphs: []draw.GlyphDrawer{
			draw.RingGlyph{},
			draw.SquareGlyph{},
			draw.TriangleGlyph{},
			draw.CrossGlyph{},
			draw.PlusGlyph{},
			//		draw.CircleGlyph{},
			//		draw.BoxGlyph{},
			//		draw.PyramidGlyph{},
		},
	}
}

func (g *GlyphGenerator) Next() draw.GlyphDrawer {
	if g.next >= len(g.glyphs) {
		g.next = 0
	}
	glyph := g.glyphs[g.next]
	g.next++
	return glyph
}

type DashesGenerator struct {
	dashes [][]vg.Length
	next   int
}

func NewDashesGenerator() *DashesGenerator {
	return &DashesGenerator{
		dashes: plotutil.DefaultDashes,
	}
}

func (g *DashesGenerator) Next() []vg.Length {
	if g.next >= len(g.dashes) {
		g.next = 0
	}
	dashes := g.dashes[g.next]
	g.next++
	return dashes
}
