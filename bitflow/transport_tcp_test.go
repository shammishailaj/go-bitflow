package bitflow

import (
	"sync"
	"testing"
	"time"

	"github.com/antongulenko/golib"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/suite"
)

type TcpListenerTestSuite struct {
	testSuiteWithSamples
}

func TestTcpListener(t *testing.T) {
	suite.Run(t, new(TcpListenerTestSuite))
}

func (suite *TcpListenerTestSuite) runGroup(sender golib.Task, generator SampleProcessor, receiver SampleSource, sink *testSampleSink) {
	var group golib.TaskGroup
	(&SamplePipeline{
		Processors: []SampleProcessor{generator},
	}).Construct(&group)
	(&SamplePipeline{
		Source:     receiver,
		Processors: []SampleProcessor{sink},
	}).Construct(&group)
	group.Add(sender)
	group.Add(&golib.TimeoutTask{DumpGoroutines: false, Timeout: 1 * time.Second})
	_, numErrs := group.WaitAndStop(1 * time.Second)
	suite.Equal(0, numErrs, "number of errors")
}

func (suite *TcpListenerTestSuite) testListenerSinkAll(m BidiMarshaller) {
	testSink := suite.newFilledTestSink()

	l := &TCPListenerSink{
		Endpoint:        ":7878",
		BufferedSamples: 100,
	}
	l.Writer.ParallelSampleHandler = parallel_handler
	l.SetMarshaller(m)

	s := &TCPSource{
		PrintErrors:   true,
		RemoteAddrs:   []string{"localhost:7878"},
		RetryInterval: time.Second,
		DialTimeout:   tcp_dial_timeout,
	}
	s.Reader.ParallelSampleHandler = parallel_handler

	sender := &oneShotTask{
		do: func() {
			suite.sendAllSamples(l)
		},
	}

	go func() {
		testSink.waitEmpty()
		s.Close()
		l.Close()
	}()

	suite.runGroup(sender, l, s, testSink)
}

func (suite *TcpListenerTestSuite) testListenerSinkIndividual(m Marshaller) {
	for i := range suite.headers {
		testSink := suite.newTestSinkFor(i)

		// TODO test that a smaller buffer leads to dropped samples

		l := &TCPListenerSink{
			Endpoint:        ":7878",
			BufferedSamples: 100,
		}
		l.Writer.ParallelSampleHandler = parallel_handler
		l.SetMarshaller(m)

		s := &TCPSource{
			PrintErrors:   true,
			RemoteAddrs:   []string{"localhost:7878"},
			RetryInterval: tcp_download_retry_interval,
			DialTimeout:   tcp_dial_timeout,
		}
		s.Reader.ParallelSampleHandler = parallel_handler

		sender := &oneShotTask{
			do: func() {
				suite.sendSamples(l, i)
			},
		}

		go func() {
			testSink.waitEmpty()
			s.Close()
			l.Close()
		}()

		suite.runGroup(sender, l, s, testSink)
	}
}

func (suite *TcpListenerTestSuite) TestListenerSinkIndividualCsv() {
	suite.testListenerSinkIndividual(new(CsvMarshaller))
}

func (suite *TcpListenerTestSuite) TestListenerSinkIndividualBinary() {
	suite.testListenerSinkIndividual(new(BinaryMarshaller))
}

func (suite *TcpListenerTestSuite) TestListenerSinkAllCsv() {
	suite.testListenerSinkAll(new(CsvMarshaller))
}

func (suite *TcpListenerTestSuite) TestListenerSinkAllBinary() {
	suite.testListenerSinkAll(new(BinaryMarshaller))
}

func (suite *TcpListenerTestSuite) testListenerSourceAll(m Marshaller) {
	testSink := suite.newFilledTestSink()

	l := NewTcpListenerSource(":7878")
	l.Reader = SampleReader{
		ParallelSampleHandler: parallel_handler,
	}

	s := &TCPSink{
		Endpoint:    "localhost:7878",
		DialTimeout: tcp_dial_timeout,
	}
	s.Writer.ParallelSampleHandler = parallel_handler
	s.SetMarshaller(m)

	sender := &oneShotTask{
		do: func() {
			suite.sendAllSamples(s)
		},
	}

	go func() {
		testSink.waitEmpty()
		l.Close()
		s.Close()
	}()

	suite.runGroup(sender, s, l, testSink)
}

func (suite *TcpListenerTestSuite) testListenerSourceIndividual(m BidiMarshaller) {
	for i := range suite.headers {
		testSink := suite.newTestSinkFor(i)

		l := NewTcpListenerSource(":7878")
		l.Reader = SampleReader{
			ParallelSampleHandler: parallel_handler,
		}

		s := &TCPSink{
			Endpoint:    "localhost:7878",
			DialTimeout: tcp_dial_timeout,
		}
		s.Writer.ParallelSampleHandler = parallel_handler
		s.SetMarshaller(m)

		sender := &oneShotTask{
			do: func() {
				suite.sendSamples(s, i)
			},
		}

		go func() {
			testSink.waitEmpty()
			l.Close()
			s.Close()
		}()

		suite.runGroup(sender, s, l, testSink)
	}
}

type oneShotTask struct {
	do func()
}

func (t *oneShotTask) Start(wg *sync.WaitGroup) (_ golib.StopChan) {
	t.do()
	return
}

func (t *oneShotTask) Stop() {
}

func (t *oneShotTask) String() string {
	return "oneshot"
}

func (suite *TcpListenerTestSuite) TestListenerSourceIndividualCsv() {
	suite.testListenerSourceIndividual(new(CsvMarshaller))
}

func (suite *TcpListenerTestSuite) TestListenerSourceIndividualBinary() {
	suite.testListenerSourceIndividual(new(BinaryMarshaller))
}

func (suite *TcpListenerTestSuite) TestListenerSourceAllCsv() {
	suite.testListenerSourceAll(new(CsvMarshaller))
}

func (suite *TcpListenerTestSuite) TestListenerSourceAllBinary() {
	suite.testListenerSourceAll(new(BinaryMarshaller))
}

func (suite *TcpListenerTestSuite) TestTcpListenerSourceError() {
	// Suppress error output
	level := log.GetLevel()
	defer log.SetLevel(level)
	log.SetLevel(log.PanicLevel)

	l := NewTcpListenerSource("8.8.8.8:7777") // The IP should not be valid for the current host -> give error
	l.Reader = SampleReader{
		ParallelSampleHandler: parallel_handler,
	}

	var group golib.TaskGroup
	(&SamplePipeline{
		Source: l,
	}).Construct(&group)
	task, numErrs := group.WaitAndStop(1 * time.Second)
	suite.Equal(1, numErrs, "number of errors")
	suite.IsType(new(SourceTaskWrapper), task)
	suite.Equal(l, task.(*SourceTaskWrapper).SampleSource)
}
