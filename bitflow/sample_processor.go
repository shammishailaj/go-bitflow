package bitflow

import (
	"fmt"
	"sync"

	"github.com/antongulenko/golib"
	log "github.com/sirupsen/logrus"
)

// SampleProcessor is the basic interface to receive and process samples.
// It receives Samples through the Sample method and
// sends samples to the subsequent SampleProcessor configured over SetSink. The forwarded Samples
// can be the same as received, completely new generated samples, and also a different
// number of Samples from the incoming ones. The Header can also be changed, but then
// the SampleProcessor implementation must take care to adjust the outgoing
// Samples accordingly. All required goroutines must be started in Start()
// and stopped when Close() is called. When Start() is called, it can be assumed that SetSink()
// has already been called to configure a non-nil subsequent SampleProcessor.
// As a special case, some SampleProcessor implementations output samples to external sinks
// like files or network connections. In this case, the incoming samples should usually be
// forwarded to the subsequent SampleProcessor without changes.
type SampleProcessor interface {
	SampleSource
	SampleSink
}

// AbstractSampleProcessor provides a few basic methods for implementations of
// SampleProcessor. It currently simply embeds the AbstractSampleSource type,
// but should be used instead of it to make the purpose more clear.
type AbstractSampleProcessor struct {
	AbstractSampleSource
}

// AbstractSampleOutput is a partial implementation of SampleProcessor intended for
// processors that output samples to an external data sink (e.g. console, file, ...).
// Configuration variables are provided for controlling the error handling.
type AbstractSampleOutput struct {
	AbstractSampleProcessor

	// DontForwardSamples can be set to true to disable forwarding of received samples
	// to the subsequent SampleProcessor.
	DontForwardSamples bool

	// DropOutputErrors can be set to true to make this AbstractSampleOutput ignore
	// errors that occurred from outputting samples to byte streams like files or network Connections.
	// In that case, such errors will be logged and the samples will be forwarded to subsequent
	// processing steps.
	DropOutputErrors bool
}

// Sample forwards the received header and sample the the subsequent SampleProcessor,
// unless the DontForwardSamples flag has been set. Actual implementations of SampleOutput
// should provide an implementation that writes the samples to some destination.
// The error parameter should be an error (possibly nil), that resulted from previously
// writing the sample to some byte stream output (like a file or network connection).
// Depending on the configuration of this AbstractSampleOutput, this error will be returned
// immediately or simply logged so that the sample can be forwarded to the subsequent processing step.
func (out *AbstractSampleOutput) Sample(err error, sample *Sample, header *Header) error {
	if err != nil {
		if out.DropOutputErrors {
			log.Errorln(err)
		} else {
			return err
		}
	}
	if out.DontForwardSamples {
		return nil
	}
	return out.GetSink().Sample(sample, header)
}

// MarshallingSampleOutput is a SampleProcessor that outputs the received samples to a
// byte stream that is generated by a Marshaller instance.
type MarshallingSampleOutput interface {
	SampleProcessor

	// SetMarshaller must configure a valid instance of Marshaller before Start() is called.
	// All received samples will be converted to a byte stream using the configured marshaller.
	SetMarshaller(marshaller Marshaller)
}

// AbstractMarshallingSampleOutput is a partial implementation of MarshallingSampleOutput
// with a simple implementation of SetMarshaller().
type AbstractMarshallingSampleOutput struct {
	AbstractSampleOutput

	// Marshaller will be used when converting Samples to byte buffers before
	// writing them to the given output stream.
	Marshaller Marshaller

	// Writer contains variables that control the marshalling and writing process.
	// They must be configured before calling Start() on this AbstractSampleOutput.
	Writer SampleWriter
}

// SetMarshaller implements the SampleOutput interface.
func (out *AbstractMarshallingSampleOutput) SetMarshaller(marshaller Marshaller) {
	out.Marshaller = marshaller
}

// DroppingSampleProcessor implements the SampleProcessor interface by dropping any incoming
// samples.
type DroppingSampleProcessor struct {
	AbstractSampleProcessor
}

// Start implements the golib.Task interface.
func (s *DroppingSampleProcessor) Start(wg *sync.WaitGroup) (_ golib.StopChan) {
	return
}

// Close implements the SampleProcessor interface.
func (s *DroppingSampleProcessor) Close() {
	s.CloseSink()
}

// String implements the golib.Task interface.
func (s *DroppingSampleProcessor) String() string {
	return "dropping samples"
}

// Sample implements the SampleProcessor interface.
func (s *DroppingSampleProcessor) Sample(sample *Sample, header *Header) error {
	return nil
}

// NoopProcessor is an empty implementation of SampleProcessor. It can be
// directly added to a SamplePipeline and will behave as a no-op processing step.
// Other implementations of SampleProcessor can embed this and override parts of
// the methods as required. No initialization is needed for this type, but an
// instance can only be used once, in one pipeline.
type NoopProcessor struct {
	AbstractSampleProcessor
	StopChan golib.StopChan
}

// Sample implements the SampleProcessor interface. It forwards the sample to the
// subsequent processor.
func (p *NoopProcessor) Sample(sample *Sample, header *Header) error {
	return p.GetSink().Sample(sample, header)
}

// Start implements the SampleProcessor interface. It creates an error-channel
// with a small channel buffer. Calling CloseSink() or Error() writes a value
// to that channel to signalize that this NoopProcessor is finished.
func (p *NoopProcessor) Start(wg *sync.WaitGroup) golib.StopChan {
	p.StopChan = golib.NewStopChan()
	return p.StopChan
}

// CloseSink reports that this NoopProcessor is finished processing.
// All goroutines must be stopped, and all Headers and Samples must be already
// forwarded to the outgoing sink, when this is called. CloseSink forwards
// the Close() invocation to the outgoing sink.
func (p *NoopProcessor) CloseSink() {
	// If there was no error, make sure to signal that this task is done.
	p.StopChan.Stop()
	p.AbstractSampleProcessor.CloseSink()
}

// Error reports that NoopProcessor has encountered an error and has stopped
// operation. After calling this, no more Headers and Samples can be forwarded
// to the outgoing sink. Ultimately, p.Close() will be called for cleaning up.
func (p *NoopProcessor) Error(err error) {
	p.StopChan.StopErr(err)
}

// Close implements the SampleProcessor interface by closing the outgoing
// sink and internal golib.StopChan. Other types that embed NoopProcessor can override this to perform
// specific actions when closing, but CloseSink() should always be called in the end.
func (p *NoopProcessor) Close() {
	p.CloseSink()
}

// String implements the SampleProcessor interface. This should be overridden
// by types that are embedding NoopProcessor.
func (p *NoopProcessor) String() string {
	return "NoopProcessor"
}

// MergeableProcessor is an extension of SampleProcessor, that also allows
// merging two processor instances of the same time into one. Merging is only allowed
// when the result of the merge would has exactly the same functionality as using the
// two separate instances. This can be used as an optional optimization.
type MergeableProcessor interface {
	SampleProcessor
	MergeProcessor(other SampleProcessor) bool
}

type SimpleProcessor struct {
	NoopProcessor
	Description          string
	Process              func(sample *Sample, header *Header) (*Sample, *Header, error)
	OnClose              func()
	OutputSampleSizeFunc func(sampleSize int) int
}

func (p *SimpleProcessor) Sample(sample *Sample, header *Header) error {
	if process := p.Process; process == nil {
		return fmt.Errorf("%s: Process function is not set", p)
	} else {
		sample, header, err := process(sample, header)
		if err == nil && sample != nil && header != nil {
			err = p.NoopProcessor.Sample(sample, header)
		}
		return err
	}
}

func (p *SimpleProcessor) Close() {
	if c := p.OnClose; c != nil {
		c()
	}
	p.NoopProcessor.Close()
}

func (p *SimpleProcessor) OutputSampleSize(sampleSize int) int {
	if f := p.OutputSampleSizeFunc; f != nil {
		return f(sampleSize)
	}
	return sampleSize
}

func (p *SimpleProcessor) String() string {
	if p.Description == "" {
		return "SimpleProcessor"
	} else {
		return p.Description
	}
}
