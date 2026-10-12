package gpu

import (
	"context"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
)

type GPU struct {
	Index      int
	UUID       string
	Model      string
	Vendor     string
	MemTotalMB int64
}

type Sample struct {
	GPUIndex  int
	Util      int
	MemUsedMB int64
	PowerW    float64
	TempC     float64
}

type Process struct {
	PID       int
	GPUIndex  int
	MemUsedMB int64
}

type Interface interface {
	Topology(ctx context.Context) ([]GPU, error)
	Sample(ctx context.Context) ([]Sample, error)
	Processes(ctx context.Context) ([]Process, error)
}

func ToProtocol(gs []GPU) []protocol.GPUInfo {
	out := make([]protocol.GPUInfo, 0, len(gs))
	for _, g := range gs {
		out = append(out, protocol.GPUInfo{
			Index:      g.Index,
			UUID:       g.UUID,
			Model:      g.Model,
			Vendor:     g.Vendor,
			MemTotalMB: g.MemTotalMB,
		})
	}
	return out
}

func ToSamples(ss []Sample, tsOffsetS int64) []protocol.MetricSample {
	out := make([]protocol.MetricSample, 0, len(ss))
	for _, s := range ss {
		out = append(out, protocol.MetricSample{
			GPUIndex:  s.GPUIndex,
			TsOffsetS: tsOffsetS,
			Util:      s.Util,
			MemUsedMB: s.MemUsedMB,
			PowerW:    s.PowerW,
			TempC:     s.TempC,
		})
	}
	return out
}
