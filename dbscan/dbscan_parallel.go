package dbscan

import (
	"fmt"
	"sync"

	log "github.com/Sirupsen/logrus"

	parallel_dbscan "github.com/antongulenko/go-DBSCAN"
	"github.com/antongulenko/go-bitflow"
	"github.com/antongulenko/go-bitflow-pipeline"
	"github.com/antongulenko/go-onlinestats"
)

// This files uses an external implementation of DBSCAN which is designed
// to run in parallel, but seems to have memory usage constantly growing with
// an increasing number of input samples.

// Implements parallel_dbscan.ClusterablePoint
type ParallelDbscanPoint struct {
	sample  *bitflow.Sample
	convert sync.Once
	point   []float64
}

func (p *ParallelDbscanPoint) String() string {
	return fmt.Sprintf("Point[%v](%p)", len(p.sample.Values), p)
}

func (p *ParallelDbscanPoint) GetPoint() []float64 {
	p.convert.Do(func() {
		p.point = make([]float64, len(p.sample.Values))
		for i, val := range p.sample.Values {
			p.point[i] = float64(val)
		}
	})
	return p.point
}

type ParallelDbscanBatchClusterer struct {
	Eps    float64
	MinPts int
}

func (c *ParallelDbscanBatchClusterer) cluster(points []parallel_dbscan.ClusterablePoint) [][]parallel_dbscan.ClusterablePoint {
	clusterer := parallel_dbscan.NewDBSCANClusterer(c.Eps, c.MinPts)
	return clusterer.Cluster(points)
}

func (c *ParallelDbscanBatchClusterer) printSummary(clusters [][]parallel_dbscan.ClusterablePoint) {
	var stats onlinestats.Running
	for _, cluster := range clusters {
		stats.Push(float64(len(cluster)))
	}
	log.Printf("%v clusters, avg size %v, size stddev %v", len(clusters), stats.Mean(), stats.Stddev())
}

func (c *ParallelDbscanBatchClusterer) ProcessBatch(header *bitflow.Header, samples []*bitflow.Sample) (*bitflow.Header, []*bitflow.Sample, error) {
	points := make([]parallel_dbscan.ClusterablePoint, len(samples))
	for i, sample := range samples {
		points[i] = &ParallelDbscanPoint{sample: sample}
	}
	clusters := c.cluster(points)
	c.printSummary(clusters)
	outSamples := make([]*bitflow.Sample, 0, len(samples))
	for i, clust := range clusters {
		clusterName := pipeline.ClusterName(i)
		for _, p := range clust {
			point, ok := p.(*ParallelDbscanPoint)
			if !ok {
				panic(fmt.Sprintf("Wrong parallel_dbscan.ClusterablePoint implementation (%T): %v", p, p))
			}
			outSample := point.sample
			outSample.SetTag(pipeline.ClusterTag, clusterName)
			outSamples = append(outSamples, outSample)
		}
	}
	return header, outSamples, nil
}

func (c *ParallelDbscanBatchClusterer) String() string {
	return fmt.Sprintf("ParallelDbscan(eps: %v, minpts: %v)", c.Eps, c.MinPts)
}