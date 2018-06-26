package query

import (
	"fmt"
	"strconv"

	"github.com/antongulenko/go-bitflow"
	pipeline "github.com/antongulenko/go-bitflow-pipeline"
	"github.com/antongulenko/go-bitflow-pipeline/fork"
)

const MultiplexForkName = "multiplex"

type PipelineVerification interface {
	VerifyInput(inputs []string) error
	VerifyOutput(output string) error
	VerifyStep(name Token, params map[string]string) error
	VerifyFork(name Token, params map[string]string) error
}

func RegisterMultiplexFork(builder *PipelineBuilder) {
	builder.RegisterFork(MultiplexForkName, createMultiplexFork, "Basic fork forwarding samples to all subpipelines. Subpipeline keys are ignored.", []string{})
}

func createMultiplexFork(subpipelines []Subpipeline, _ map[string]string) (fork.ForkDistributor, error) {
	var res fork.MultiplexDistributor
	res.Subpipelines = make([]*pipeline.SamplePipeline, len(subpipelines))
	var err error
	for i, pipe := range subpipelines {
		res.Subpipelines[i], err = pipe.Build()
		if err != nil {
			return nil, err
		}
	}
	return &res, nil
}

func strTok(str string) Token {
	return Token{Type: STR, Lit: str}
}

var emptyEndpointToken = strTok(string(bitflow.EmptyEndpoint) + "://-")

func (pipe Pipeline) Transform(verify PipelineVerification) (Pipeline, error) {
	res, err := pipe.transform(verify, true)
	if err == nil {
		switch res[0].(type) {
		case Input, MultiInput:
			break
		default:
			res = append(Pipeline{Input{emptyEndpointToken}}, res...)
		}
	}
	return res, err
}

func (pipe Pipeline) transform(verify PipelineVerification, isInput bool) (Pipeline, error) {
	if len(pipe) == 0 {
		return nil, ParserError{
			Pos:     pipe.Pos(),
			Message: "Empty pipeline is not allowed",
		}
	}
	var res Pipeline
	var err error

	switch input := pipe[0].(type) {
	case Input:
		pipe = pipe[1:]
		inputs := make([]string, len(input))
		for i, in := range input {
			inputs[i] = in.Content()
		}
		if isInput {
			if err = verify.VerifyInput(inputs); err != nil {
				return nil, err
			}
		}
		res = append(res, input)
	case Pipelines:
		if isInput {
			pipe = pipe[1:]
			newInput, err := input.transformMultiInput(verify)
			if err != nil {
				return nil, err
			}
			res = append(res, newInput)
		}
	}
	for _, step := range pipe {
		var newStep PipelineStep
		switch step := step.(type) {
		case Output:
			err = verify.VerifyOutput(Token(step).Content())
			newStep = step
		case Step:
			newStep, err = step.transformStep(verify)
		case Pipelines:
			newStep, err = step.transformMultiplex(verify)
		case Fork:
			newStep, err = step.transformFork(verify)
		default:
			err = ParserError{
				Pos:     step.Pos(),
				Message: fmt.Sprintf("Unsupported pipeline step type during transformation: %T", step),
			}
		}
		if err != nil {
			break
		}
		res = append(res, newStep)
	}
	return res, err
}

func (pipes Pipelines) transformMultiInput(verify PipelineVerification) (MultiInput, error) {
	res := MultiInput{Pipelines: make(Pipelines, len(pipes))}
	for i, subPipe := range pipes {
		subPipe, err := subPipe.transform(verify, true)
		if err != nil {
			return MultiInput{}, err
		}
		res.Pipelines[i] = subPipe
	}
	return res, nil
}

func (step Step) transformStep(verify PipelineVerification) (Step, error) {
	err := verify.VerifyStep(step.Name, step.ParamsMap())
	if err != nil {
		err = ParserError{
			Pos:     step.Name,
			Message: fmt.Sprintf("%v: %v", step.Name.Content(), err),
		}
	}
	return step, err
}

func (pipes Pipelines) transformMultiplex(verify PipelineVerification) (Fork, error) {
	newPipes := make(Pipelines, len(pipes))
	for i, pipe := range pipes {
		newPipes[i] = append(Pipeline{Input{strTok(strconv.Itoa(i))}}, pipe...)
	}
	return Fork{
		Step: Step{
			Name:   strTok(MultiplexForkName),
			Params: make(map[Token]Token),
		},
		Pipelines: newPipes,
	}.transformFork(verify)
}

func (f Fork) transformFork(verify PipelineVerification) (outFork Fork, err error) {
	err = verify.VerifyFork(f.Name, f.ParamsMap())
	if err == nil {
		outFork.Step = f.Step
		outFork.Pipelines = make(Pipelines, len(f.Pipelines))
		for i, subPipe := range f.Pipelines {
			subPipe, err = subPipe.transform(verify, false)
			if err != nil {
				return
			}
			outFork.Pipelines[i] = subPipe
		}
	}
	return
}
