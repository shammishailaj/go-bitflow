package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	log "github.com/Sirupsen/logrus"

	"github.com/antongulenko/go-bitflow"
	"github.com/antongulenko/go-bitflow-pipeline"
	"github.com/antongulenko/golib"
)

type AnalysisFunc func(pipeline *SamplePipeline)
type ParameterizedAnalysisFunc func(pipeline *SamplePipeline, params string)

type registeredAnalysis struct {
	Name   string
	Func   ParameterizedAnalysisFunc
	Params string
}

// Can be filled from init() functions using RegisterAnalysis() and RegisterParameterizedAnalysis
var analysis_registry = make(map[string]registeredAnalysis)

func RegisterAnalysis(name string, setupPipeline AnalysisFunc) {
	RegisterAnalysisParams(name, func(pipeline *SamplePipeline, _ string) {
		setupPipeline(pipeline)
	}, "")
}

func RegisterAnalysisParams(name string, setupPipeline ParameterizedAnalysisFunc, paramDescription string) {
	if _, ok := analysis_registry[name]; ok {
		panic("Analysis already registered: " + name)
	}
	analysis_registry[name] = registeredAnalysis{name, setupPipeline, paramDescription}
}

func main() {
	os.Exit(do_main())
}

func do_main() int {
	var analysisNames golib.StringSlice
	flag.Var(&analysisNames, "e", fmt.Sprintf("Select one or more analyses to execute. Use -print-analyses for listing all analyses."))
	printAnalyses := flag.Bool("print-analyses", false, "Print a list of available analyses and exit.")

	var p SamplePipeline
	var f bitflow.EndpointFactory
	bitflow.RegisterGolibFlags()
	f.RegisterAllFlags()
	flag.Parse()
	golib.ConfigureLogging()
	if *printAnalyses {
		fmt.Printf("Available analyses:%v\n", allAnalyses())
		return 0
	}
	analyses, err := resolvePipeline(analysisNames)
	if err != nil {
		log.Fatalln(err)
	}
	if err := f.ReadInputArguments(); err != nil {
		log.Fatalln(err)
	}
	defer golib.ProfileCpu()()
	if err := p.Configure(&f); err != nil {
		log.Fatalln(err)
	}
	p.setup(analyses)
	if err := p.CheckTasks(); err != nil {
		log.Fatalln(err)
	}
	for _, str := range p.print() {
		log.Println(str)
	}
	return p.StartAndWait()
}

type parameterizedAnalysis struct {
	setup  ParameterizedAnalysisFunc
	params string
}

func resolvePipeline(analysisNames golib.StringSlice) ([]parameterizedAnalysis, error) {
	result := make([]parameterizedAnalysis, 0, len(analysisNames))
	for _, name := range analysisNames {
		params := ""
		if index := strings.IndexRune(name, ','); index >= 0 {
			params = name[index+1:]
			name = name[:index]
		}
		analysis, ok := analysis_registry[name]
		if !ok {
			return nil, fmt.Errorf("Analysis '%v' not registered. Available analyses:%v", name, allAnalyses())
		}
		result = append(result, parameterizedAnalysis{analysis.Func, params})
	}
	return result, nil
}

func allAnalyses() string {
	all := make(SortedAnalyses, 0, len(analysis_registry))
	for _, analysis := range analysis_registry {
		all = append(all, analysis)
	}
	sort.Sort(all)
	var buf bytes.Buffer
	for i, analysis := range all {
		if analysis.Func == nil {
			continue
		}
		if i > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(" - ")
		buf.WriteString(analysis.Name)
		if analysis.Params != "" {
			buf.WriteString(" (Params: ")
			buf.WriteString(analysis.Params)
			buf.WriteString(")")
		}
	}
	return buf.String()
}

type SortedAnalyses []registeredAnalysis

func (slice SortedAnalyses) Len() int {
	return len(slice)
}

func (slice SortedAnalyses) Less(i, j int) bool {
	return slice[i].Name < slice[j].Name
}

func (slice SortedAnalyses) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}

type SamplePipeline struct {
	bitflow.SamplePipeline
	lastProcessor bitflow.SampleProcessor
}

func (p *SamplePipeline) Add(step bitflow.SampleProcessor) *SamplePipeline {
	if p.lastProcessor != nil {
		if merger, ok := p.lastProcessor.(pipeline.MergableProcessor); ok {
			if merger.MergeProcessor(step) {
				// Merge successful: drop the incoming step
				return p
			}
		}
	}
	p.lastProcessor = step
	p.SamplePipeline.Add(step)
	return p
}

func (p *SamplePipeline) Batch(steps ...pipeline.BatchProcessingStep) *SamplePipeline {
	batch := new(pipeline.BatchProcessor)
	for _, step := range steps {
		batch.Add(step)
	}
	return p.Add(batch)
}

func (p *SamplePipeline) setup(analyses []parameterizedAnalysis) {
	for _, analysis := range analyses {
		if setup := analysis.setup; setup != nil {
			setup(p, analysis.params)
		}
	}
}

func (p *SamplePipeline) print() []string {
	processors := p.Processors
	if len(processors) == 0 {
		return []string{"Empty analysis pipeline"}
	} else if len(processors) == 1 {
		return []string{"Analysis: " + processors[0].String()}
	} else {
		res := make([]string, 0, len(processors)+1)
		res = append(res, "Analysis pipeline:")
		for i, proc := range processors {
			indent := "├─"
			if i == len(processors)-1 {
				indent = "└─"
			}
			res = append(res, indent+proc.String())
		}
		return res
	}
}
