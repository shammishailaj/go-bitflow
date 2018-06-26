package fork

import (
	"fmt"
	"sync"

	"github.com/antongulenko/go-bitflow"
	"github.com/antongulenko/go-bitflow-pipeline"
	"github.com/antongulenko/golib"
	log "github.com/sirupsen/logrus"
)

type Subpipeline struct {
	Pipe *pipeline.SamplePipeline
	Key  string
}

type ForkDistributor interface {
	Distribute(sample *bitflow.Sample, header *bitflow.Header) ([]Subpipeline, error)
	String() string
}

type subpipelineStart struct {
	pipe      *pipeline.SamplePipeline
	firstStep bitflow.SampleProcessor
	key       string
}

type SampleFork struct {
	MultiPipeline
	bitflow.NoopProcessor

	Distributor ForkDistributor

	// If true, errors of subpipelines will be logged but don't stop the entire MultiPipeline
	// Finished pipelines must be reported through LogFinishedPipeline()
	NonfatalErrors bool

	pipelines map[*pipeline.SamplePipeline]subpipelineStart
	lock      sync.Mutex

	newPipelineHandler func(bitflow.SampleProcessor) bitflow.SampleProcessor // Optional hook
	ForkPath           []string
}

func (f *SampleFork) Start(wg *sync.WaitGroup) golib.StopChan {
	result := f.NoopProcessor.Start(wg)
	f.MultiPipeline.Init(f.GetSink(), f.CloseSink, wg)
	f.pipelines = make(map[*pipeline.SamplePipeline]subpipelineStart)
	return result
}

func (f *SampleFork) Close() {
	f.StopPipelines()
}

func (f *SampleFork) Sample(sample *bitflow.Sample, header *bitflow.Header) error {
	subpipes, err := f.Distributor.Distribute(sample, header)
	if err != nil {
		return err
	}
	return f.getSubpipelineSink(subpipes).Sample(sample, header)
}

func (f *SampleFork) getSubpipelineSink(subpipes []Subpipeline) bitflow.SampleProcessor {
	sinks := make([]bitflow.SampleProcessor, len(subpipes))
	for i, subpipe := range subpipes {
		sinks[i] = f.getPipeline(subpipe)
	}
	return &sinkMultiplexer{sinks: sinks}
}

func (f *SampleFork) getPipeline(subpipe Subpipeline) bitflow.SampleProcessor {
	f.lock.Lock()
	defer f.lock.Unlock()

	pipe, ok := f.pipelines[subpipe.Pipe]
	if !ok {
		firstStep := f.initializePipeline(subpipe)
		if hook := f.newPipelineHandler; hook != nil {
			firstStep = hook(firstStep)
		}
		pipe = subpipelineStart{key: subpipe.Key, pipe: subpipe.Pipe, firstStep: firstStep}
		f.pipelines[subpipe.Pipe] = pipe
	} else if subpipe.Key != pipe.key {
		log.Debugf("[%v]: Subpipeline %v is reusing the pipeline started previously for key %v", f, subpipe.Key, pipe.key)
	}
	return pipe.firstStep
}

func (f *SampleFork) initializePipeline(subpipe Subpipeline) bitflow.SampleProcessor {
	pipe := subpipe.Pipe
	path := f.setForkPaths(subpipe.Pipe, subpipe.Key)
	log.Debugf("[%v]: Starting forked subpipeline %v", f, path)
	if pipe.Source != nil {
		// Forked pipelines should not have an explicit source, as they receive
		// samples from the steps preceding them
		log.Warnf("[%v]: The Source field of the %v subpipeline was set and will be ignored: %v", f, path, pipe.Source)
		pipe.Source = nil
	}
	pipe.Add(&f.merger)
	return pipe.Processors[0]
}

func (f *SampleFork) setForkPaths(pipeline *pipeline.SamplePipeline, key string) []string {
	path := make([]string, len(f.ForkPath)+1)
	copy(path, f.ForkPath)
	path[len(path)-1] = key
	for _, proc := range pipeline.Processors {
		if forkContainer, ok := proc.(abstractForkContainer); ok {
			forkContainer.getAbstractFork().ForkPath = path
		}
	}
	return path
}

func (f *SampleFork) ContainedStringers() []fmt.Stringer {
	if container, ok := f.Distributor.(pipeline.StringerContainer); ok {
		return container.ContainedStringers()
	} else {
		return []fmt.Stringer{f.Distributor}
	}
}

func (f *SampleFork) String() string {
	res := "Fork "
	if _, complexDistributor := f.Distributor.(pipeline.StringerContainer); complexDistributor {
		res += f.Distributor.String()
	}
	return res
}

type abstractForkContainer interface {
	getAbstractFork() *SampleFork
}

func (f *SampleFork) getAbstractFork() *SampleFork {
	return f
}

type sinkMultiplexer struct {
	bitflow.DroppingSampleProcessor
	sinks []bitflow.SampleProcessor
}

func (s *sinkMultiplexer) Sample(sample *bitflow.Sample, header *bitflow.Header) error {
	// The samples are not forwarded in parallel. Parllelism between pipelines can be achieved by decoupling steps on each subpipeline.
	var errors golib.MultiError
	for _, sink := range s.sinks {
		if sink != nil {
			// The DeepClone() is necessary since the forks might change the sample
			// values independently. In some cases it might not be necessary, but that
			// would be a rather complex optimization.
			errors.Add(sink.Sample(sample.DeepClone(), header))
		}
	}
	return errors.NilOrError()
}

func (s *sinkMultiplexer) String() string {
	return fmt.Sprintf("parallel multi sink len %v", len(s.sinks))
}
