package math

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"sync"

	"github.com/antongulenko/golib"
	"github.com/bitflow-stream/go-bitflow/bitflow"
	"github.com/bitflow-stream/go-bitflow/script/reg"
	log "github.com/sirupsen/logrus"
)

func RegisterSphere(b reg.ProcessorRegistry) {
	create := func(p *bitflow.SamplePipeline, params map[string]string) error {
		var err error
		points, err := strconv.Atoi(params["points"])
		if err != nil {
			return reg.ParameterError("points", err)
		}
		seed := int64(1)
		if seedStr, ok := params["seed"]; ok {
			seed, err = strconv.ParseInt(seedStr, 10, 64)
			if err != nil {
				return reg.ParameterError("seed", err)
			}
		}
		radiusStr, hasRadius := params["radius"]
		radiusMetricStr, hasRadiusMetric := params["radius_metric"]
		if hasRadius == hasRadiusMetric {
			return errors.New("Need either 'radius' or 'radius_metric' parameter")
		}

		sphere := &SpherePoints{
			RandomSeed: seed,
			NumPoints:  points,
		}
		if hasRadius {
			sphere.RadiusMetric = -1
			sphere.Radius, err = strconv.ParseFloat(radiusStr, 64)
			if err != nil {
				return reg.ParameterError("radius", err)
			}
		} else {
			sphere.RadiusMetric, err = strconv.Atoi(radiusMetricStr)
			if err != nil {
				return reg.ParameterError("radius_metric", err)
			}
		}
		p.Add(sphere)
		return nil
	}
	b.RegisterAnalysisParamsErr("sphere", create, "Treat every sample as the center of a multi-dimensional sphere, and output a number of random points on the hull of the resulting sphere. The radius can either be fixed or given as one of the metrics", reg.RequiredParams("points"), reg.OptionalParams("seed", "radius", "radius_metric"))
}

type SpherePoints struct {
	bitflow.NoopProcessor
	RandomSeed int64
	NumPoints  int

	RadiusMetric int // If >= 0, use to get radius. Otherwise, use Radius field.
	Radius       float64

	rand *rand.Rand
}

func (p *SpherePoints) Start(wg *sync.WaitGroup) golib.StopChan {
	p.rand = rand.New(rand.NewSource(p.RandomSeed))
	return p.NoopProcessor.Start(wg)
}

func (p *SpherePoints) Sample(sample *bitflow.Sample, header *bitflow.Header) error {
	if len(header.Fields) < 1 {
		return errors.New("Cannot calculate sphere points with 0 metrics")
	}
	if p.RadiusMetric < 0 || p.RadiusMetric >= len(sample.Values) {
		return fmt.Errorf("SpherePoints.RadiusMetrics = %v out of range, sample has %v metrics", p.RadiusMetric, len(sample.Values))
	}

	// If we use a metric as radius, remove it from the header
	values := sample.Values
	radius := p.Radius
	if p.RadiusMetric >= 0 {
		radius = float64(values[p.RadiusMetric])

		fields := header.Fields
		copy(fields[p.RadiusMetric:], fields[p.RadiusMetric+1:])
		fields = fields[:len(fields)-1]
		header = header.Clone(fields)

		copy(values[p.RadiusMetric:], values[p.RadiusMetric+1:])
		values = values[:len(values)-1]
	}

	for i := 0; i < p.NumPoints; i++ {
		out := sample.Clone()
		out.Values = p.randomSpherePoint(radius, values)
		if err := p.NoopProcessor.Sample(out, header); err != nil {
			return err
		}
	}
	return nil
}

// https://de.wikipedia.org/wiki/Kugelkoordinaten#Verallgemeinerung_auf_n-dimensionale_Kugelkoordinaten
func (p *SpherePoints) randomSpherePoint(radius float64, center []bitflow.Value) []bitflow.Value {
	sinValues := make([]float64, len(center))
	cosValues := make([]float64, len(center))
	for i := range center {
		angle := p.randomAngle()
		sinValues[i] = math.Sin(angle)
		cosValues[i] = math.Cos(angle)
	}

	// Calculate point for a sphere around the point (0, 0, 0, ...)
	result := make([]bitflow.Value, len(center), cap(center))
	for i := range center {
		coordinate := radius
		for j := 0; j < i; j++ {
			coordinate *= sinValues[j]
		}
		if i < len(center)-1 {
			coordinate *= cosValues[i]
		}
		result[i] = bitflow.Value(coordinate)
	}

	// Sanity check
	var sum float64
	for _, v := range result {
		sum += float64(v) * float64(v)
	}
	radSq := radius * radius
	if math.Abs(sum-radSq) > (sum * 0.0000000001) {
		log.Warnf("Illegal sphere point. Radius: %v. Diff: %v. Point: %v", radius, math.Abs(sum-radSq), result)
	}

	// Move the point so it is part of the sphere around the given center
	for i, val := range center {
		result[i] += val
	}
	return result
}

func (p *SpherePoints) randomAngle() float64 {
	return p.rand.Float64() * 2 * math.Pi // Random angle in 0..90 degrees
}
