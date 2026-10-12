package gpu

import (
	"context"
	"testing"
)

func TestStub(t *testing.T) {
	s := &Stub{
		GPUs:     []GPU{{Index: 0, UUID: "GPU-x", Model: "GB10", Vendor: "nvidia", MemTotalMB: 128000}},
		Samples:  []Sample{{GPUIndex: 0, Util: 50, MemUsedMB: 1024}},
		ProcList: []Process{{PID: 123, GPUIndex: 0, MemUsedMB: 1024}},
	}
	ctx := context.Background()
	topo, err := s.Topology(ctx)
	if err != nil || len(topo) != 1 {
		t.Fatalf("topology: %v %v", topo, err)
	}
	ps := ToProtocol(topo)
	if ps[0].Vendor != "nvidia" || ps[0].MemTotalMB != 128000 {
		t.Fatalf("protocol mapping: %+v", ps[0])
	}
	samples, _ := s.Sample(ctx)
	ms := ToSamples(samples, 5)
	if ms[0].TsOffsetS != 5 || ms[0].Util != 50 {
		t.Fatalf("sample mapping: %+v", ms[0])
	}
	procs, _ := s.Processes(ctx)
	if len(procs) != 1 || procs[0].PID != 123 {
		t.Fatalf("processes: %+v", procs)
	}
}

func TestNVML(t *testing.T) {
	n := &NVML{}
	ctx := context.Background()
	topo, err := n.Topology(ctx)
	if err != nil {
		t.Skipf("nvml unavailable: %v", err)
	}
	if len(topo) == 0 {
		t.Fatal("no GPUs")
	}
	for _, g := range topo {
		if g.UUID == "" || g.Model == "" {
			t.Fatalf("incomplete topology: %+v", g)
		}
		if g.MemTotalMB <= 0 {
			t.Fatalf("mem_total fallback missing: %+v", g)
		}
	}
	samples, err := n.Sample(ctx)
	if err != nil || len(samples) != len(topo) {
		t.Fatalf("sample: %v %v", samples, err)
	}
	if _, err := n.Processes(ctx); err != nil {
		t.Fatalf("processes: %v", err)
	}
	t.Logf("topology: %+v", topo)
	t.Logf("samples: %+v", samples)
}
