package dbscan

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/antongulenko/go-bitflow"
	"github.com/antongulenko/go-bitflow-pipeline"
	"github.com/dhconnelly/rtreego"
)

type RtreeSetOfPoints struct {
	tree       *rtreego.Rtree
	allPoints  []Point
	PointWidth float64
}

func NewRtreeSetOfPoints(dim, minChildren, maxChildren int, pointWidth float64) *RtreeSetOfPoints {
	return &RtreeSetOfPoints{
		tree:       rtreego.NewTree(dim, minChildren, maxChildren),
		PointWidth: pointWidth,
	}
}

func (tree *RtreeSetOfPoints) Add(s *bitflow.Sample) {
	point := NewRtreePoint(s, tree.PointWidth)
	tree.tree.Insert(point)
	tree.allPoints = append(tree.allPoints, point)
}

func (tree *RtreeSetOfPoints) RegionQuery(point Point, eps float64) map[Point]bool {
	if rtreePoint, ok := point.(*RtreePoint); !ok {
		panic(fmt.Sprintf("Cannot handle Point implementation %T: %v", point, point))
	} else {

		regionQueryNr++
		if rtreePoint.regionQueried > 0 {
			log.Println("QUERYING AGAIN FOR", rtreePoint.regionQueried, "now at", regionQueryNr)
		}
		rtreePoint.regionQueried = regionQueryNr

		bounds := rtreePoint.point.ToRect(eps)
		spatialPoints := tree.tree.SearchIntersect(bounds)

		log.Println("Query for", regionQueryNr, "returned", len(spatialPoints), "results")

		result := make(map[Point]bool, len(spatialPoints))
		for _, spatial := range spatialPoints {
			if rtreePoint, ok := spatial.(*RtreePoint); !ok {
				panic(fmt.Sprintf("Cannot handle Point implementation %T: %v", spatial, spatial))
			} else {
				result[rtreePoint] = true
			}
		}
		return result
	}
}

func (tree *RtreeSetOfPoints) AllPoints() []Point {
	return tree.allPoints
}

func (tree *RtreeSetOfPoints) Cluster(d *Dbscan) map[string][]*bitflow.Sample {
	result := make(map[string][]*bitflow.Sample, len(tree.allPoints))
	d.Cluster(tree)
	for _, point := range tree.allPoints {
		rtreePoint, ok := point.(*RtreePoint)
		if !ok {
			panic(fmt.Sprintf("Unexpected Point implementation %T: %v", point, point))
		}
		clusterName := pipeline.ClusterName(rtreePoint.cluster)
		rtreePoint.sample.SetTag(pipeline.ClusterTag, clusterName)
		result[clusterName] = append(result[clusterName], rtreePoint.sample)
	}
	return result
}

var regionQueryNr = 0

type RtreePoint struct {
	sample  *bitflow.Sample
	point   rtreego.Point
	rect    *rtreego.Rect
	cluster int

	regionQueried int
}

func NewRtreePoint(s *bitflow.Sample, width float64) *RtreePoint {
	point := make(rtreego.Point, len(s.Values))
	for i, val := range s.Values {
		point[i] = float64(val)
	}
	return &RtreePoint{
		sample:  s,
		cluster: pipeline.ClusterUnclassified,
		point:   point,
		rect:    point.ToRect(width),
	}
}

func (point *RtreePoint) SetCluster(cluster int) {

	log.Println("Setting cluster to", cluster)

	point.cluster = cluster
}

func (point *RtreePoint) GetCluster() int {
	return point.cluster
}

func (point *RtreePoint) Bounds() *rtreego.Rect {
	return point.rect
}
