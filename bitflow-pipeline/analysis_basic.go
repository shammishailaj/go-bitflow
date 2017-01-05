package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/antongulenko/go-bitflow"
	. "github.com/antongulenko/go-bitflow-pipeline"
)

func init() {
	RegisterAnalysisParams("decouple", decouple_samples, "number of buffered samples")
	RegisterAnalysis("merge_headers", merge_headers)
	RegisterAnalysisParams("pick", pick_x_percent, "samples to keep 0..1")
	RegisterAnalysisParams("head", pick_head, "number of first samples to keep")
	RegisterAnalysis("print", print_samples)
	RegisterAnalysisParams("filter_tag", filter_tag, "tag=value or tag!=value")

	RegisterAnalysis("shuffle", shuffle_data)
	RegisterAnalysisParams("sort", sort_data, "comma-separated list of tags")

	RegisterAnalysis("scale_min_max", normalize_min_max)
	RegisterAnalysis("standardize", normalize_standardize)

	RegisterAnalysisParams("plot", plot, "[<color tag>,]<output filename>")
	RegisterAnalysisParams("plot_separate", separate_plots, "same as plot")
	RegisterAnalysisParams("stats", feature_stats, "output filename for metric statistics")

	RegisterAnalysisParams("remap", remap_features, "comma-separated list of metrics")
	RegisterAnalysisParams("filter_variance", filter_variance, "minimum weighted stddev of the population (stddev / mean)")

	RegisterAnalysisParams("tags", set_tags, "comma-separated list of key-value tags")
	RegisterAnalysisParams("split_files", split_files, "tag to use for separating the data")
	RegisterAnalysisParams("rename", rename_metrics, "comma-separated list of regex=replace pairs")

	RegisterAnalysisParams("include", filter_metrics_include, "Regex to match metrics to be included")
	RegisterAnalysisParams("exclude", filter_metrics_exclude, "Regex to match metrics to be excluded")

	RegisterAnalysis("strip", strip_metrics)
	RegisterAnalysis("sleep", sleep_samples)
	RegisterAnalysis("set_time", set_time_processor)
}

func print_samples(p *SamplePipeline) {
	p.Add(NewSamplePrinter())
}

func shuffle_data(p *SamplePipeline) {
	p.Batch(NewSampleShuffler())
}

func sort_data(p *SamplePipeline, params string) {
	var tags []string
	if params != "" {
		tags = strings.Split(params, ",")
	}
	p.Batch(&SampleSorter{tags})
}

func merge_headers(p *SamplePipeline) {
	p.Add(NewMultiHeaderMerger())
}

func normalize_min_max(p *SamplePipeline) {
	p.Batch(new(MinMaxScaling))
}

func normalize_standardize(p *SamplePipeline) {
	p.Batch(new(StandardizationScaling))
}

func pick_x_percent(p *SamplePipeline, params string) {
	pick_percentage, err := strconv.ParseFloat(params, 64)
	if err != nil {
		log.Fatalln("Failed to parse parameter for -e pick:", err)
	}
	counter := float64(0)
	p.Add(&SampleFilter{
		Description: String(fmt.Sprintf("Pick %.2f%%", pick_percentage*100)),
		IncludeFilter: func(inSample *bitflow.Sample) bool {
			counter += pick_percentage
			if counter > 1.0 {
				counter -= 1.0
				return true
			}
			return false
		},
	})
}

func filter_metrics_include(p *SamplePipeline, param string) {
	p.Add(NewMetricFilter().IncludeRegex(param))
}

func filter_metrics_exclude(p *SamplePipeline, param string) {
	p.Add(NewMetricFilter().ExcludeRegex(param))
}

func filter_tag(p *SamplePipeline, params string) {
	val := ""
	equals := true
	index := strings.Index(params, "!=")
	if index >= 0 {
		val = params[index+2:]
		equals = false
	} else {
		index = strings.IndexRune(params, '=')
		if index == -1 {
			log.Fatalln("Parameter for -e filter_tag must be '<tag>=<value>' or '<tag>!=<value>'")
		} else {
			val = params[index+1:]
		}
	}
	tag := params[:index]
	filter := new(SampleTagFilter)
	if equals {
		filter.Equal(tag, val)
	} else {
		filter.Unequal(tag, val)
	}
	p.Add(filter)
}

func plot(pipe *SamplePipeline, params string) {
	do_plot(pipe, params, false)
}

func separate_plots(pipe *SamplePipeline, params string) {
	do_plot(pipe, params, true)
}

func do_plot(pipe *SamplePipeline, params string, separatePlots bool) {
	if params == "" {
		log.Fatalln("-e plot needs parameters (-e plot,[<tag>,]<filename>)")
	}
	index := strings.IndexRune(params, ',')
	tag := ""
	filename := params
	if index == -1 {
		log.Warnln("-e plot got no tag parameter, not coloring plot (-e plot,[<tag>,]<filename>)")
	} else {
		tag = params[:index]
		filename = params[index+1:]
	}
	pipe.Add(&Plotter{OutputFile: filename, ColorTag: tag, SeparatePlots: separatePlots})
}

func decouple_samples(pipe *SamplePipeline, params string) {
	buf := 150000
	if params != "" {
		var err error
		if buf, err = strconv.Atoi(params); err != nil {
			log.Fatalln("Failed to parse parameter for -e decouple:", err)
		}
	} else {
		log.Warnln("No parameter for -e decouple, default channel buffer:", buf)
	}
	pipe.Add(&DecouplingProcessor{ChannelBuffer: buf})
}

func feature_stats(pipe *SamplePipeline, params string) {
	if params == "" {
		log.Fatalln("-e stats needs parameter: file to store feature statistics")
	} else {
		pipe.Add(NewStoreStats(params))
	}
}

func remap_features(pipe *SamplePipeline, params string) {
	var metrics []string
	if params != "" {
		metrics = strings.Split(params, ",")
	}
	pipe.Add(NewMetricMapper(metrics))
}

func filter_variance(pipe *SamplePipeline, params string) {
	variance, err := strconv.ParseFloat(params, 64)
	if err != nil {
		log.Fatalln("Error parsing parameter for -e filter_variance:", err)
	}
	pipe.Batch(NewMetricVarianceFilter(variance))
}

func pick_head(pipe *SamplePipeline, params string) {
	num, err := strconv.Atoi(params)
	if err != nil {
		log.Fatalln("Error parsing parameter for -e head:", err)
	}
	processed := 0
	pipe.Add(&SimpleProcessor{
		Description: "Pick first " + strconv.Itoa(num) + " samples",
		Process: func(sample *bitflow.Sample, header *bitflow.Header) (*bitflow.Sample, *bitflow.Header, error) {
			if num > processed {
				processed++
				return sample, header, nil
			} else {
				return nil, nil, nil
			}
		},
	})
}

func set_tags(pipe *SamplePipeline, params string) {
	pairs := strings.Split(params, ",")
	keys := make([]string, len(pairs))
	values := make([]string, len(pairs))
	for i, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			log.Fatalln("Parameter for -e tags must be comma-separated key-value pairs: -e tags,KEY=VAL,KEY2=VAL2")
		}
		keys[i] = parts[0]
		values[i] = parts[1]
	}
	pipe.Add(&SimpleProcessor{
		Description: "",
		Process: func(sample *bitflow.Sample, header *bitflow.Header) (*bitflow.Sample, *bitflow.Header, error) {
			for i, key := range keys {
				sample.SetTag(key, values[i])
			}
			return sample, header, nil
		},
	})
}

func split_files(p *SamplePipeline, params string) {
	distributor := &TagsDistributor{
		Tags:        []string{params},
		Separator:   "-",
		Replacement: "_unknown_",
	}
	p.Add(NewMetricFork(distributor, MultiFileSuffixBuilder(nil)))
}

func rename_metrics(p *SamplePipeline, params string) {
	var regexes []*regexp.Regexp
	var replacements []string
	for i, part := range strings.Split(params, ",") {
		keyVal := strings.SplitN(part, "=", 2)
		if len(keyVal) != 2 {
			log.Fatalf("Parmameter %v for -e rename is not regex=replace: %v", i, part)
		}
		regexCode := keyVal[0]
		replace := keyVal[1]
		r, err := regexp.Compile(regexCode)
		if err != nil {
			log.Fatalf("Error compiling regex %v: %v", regexCode, err)
		}
		regexes = append(regexes, r)
		replacements = append(replacements, replace)
	}
	if len(regexes) == 0 {
		log.Fatalln("-e rename needs at least one regex=replace parameter (comma-separated)")
	}
	p.Add(NewMetricRenamer(regexes, replacements))
}

func strip_metrics(p *SamplePipeline) {
	p.Add(&SimpleProcessor{
		Description: "remove metrics",
		Process: func(sample *bitflow.Sample, header *bitflow.Header) (*bitflow.Sample, *bitflow.Header, error) {
			return sample.Metadata().NewSample(nil), header.Clone(nil), nil
		},
	})
}

func sleep_samples(p *SamplePipeline) {
	var lastTimestamp time.Time
	p.Add(&SimpleProcessor{
		Description: "sleep between samples",
		Process: func(sample *bitflow.Sample, header *bitflow.Header) (*bitflow.Sample, *bitflow.Header, error) {
			last := lastTimestamp
			if !last.IsZero() {
				diff := sample.Time.Sub(last)
				if diff > 0 {
					time.Sleep(diff)
				}
			}
			lastTimestamp = sample.Time
			return sample, header, nil
		},
	})
}

func set_time_processor(p *SamplePipeline) {
	p.Add(&SimpleProcessor{
		Description: "reset time",
		Process: func(sample *bitflow.Sample, header *bitflow.Header) (*bitflow.Sample, *bitflow.Header, error) {
			sample.Time = time.Now()
			return sample, header, nil
		},
	})
}
