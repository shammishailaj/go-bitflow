package dbscan

import (
	"fmt"
	"log"

	"github.com/antongulenko/data2go/sample"
	"github.com/carbocation/runningvariance"
)

const (
	ClusterTag    = "cluster"
	ClusterPrefix = "Cluster-"
)

type DbscanBatchClusterer struct {
	Dbscan

	TreeMinChildren int     // 25
	TreeMaxChildren int     // 50
	TreePointWidth  float64 // 0.0001
}

func (c *DbscanBatchClusterer) printSummary(clusters map[string][]*sample.Sample) {
	var stats runningvariance.RunningStat
	for _, cluster := range clusters {
		stats.Push(float64(len(cluster)))
	}
	log.Printf("%v clusters, avg size %v, size stddev %v\n", len(clusters), stats.Mean(), stats.StandardDeviation())
}

func (c *DbscanBatchClusterer) ProcessBatch(header *sample.Header, samples []*sample.Sample) (*sample.Header, []*sample.Sample) {
	log.Println("Building tree...")

	tree := NewRtreeSetOfPoints(len(header.Fields), c.TreeMinChildren, c.TreeMaxChildren, c.TreePointWidth)
	for _, sample := range samples {
		tree.Add(sample)
	}

	log.Println("Clustering ...")

	clusters := tree.Cluster(&c.Dbscan, ClusterTag, ClusterPrefix)
	c.printSummary(clusters)
	return header, samples
}

func (c *DbscanBatchClusterer) String() string {
	return fmt.Sprintf("Rtree-Dbscan(eps: %v, minpts: %v, tree: %v-%v, width: %v)",
		c.Eps, c.MinPts, c.TreeMinChildren, c.TreeMaxChildren, c.TreePointWidth)
}