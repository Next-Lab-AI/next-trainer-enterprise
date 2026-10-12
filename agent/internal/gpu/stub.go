package gpu

import "context"

type Stub struct {
	GPUs      []GPU
	Samples   []Sample
	ProcList  []Process
}

func (s *Stub) Topology(ctx context.Context) ([]GPU, error)  { return s.GPUs, nil }
func (s *Stub) Sample(ctx context.Context) ([]Sample, error) { return s.Samples, nil }
func (s *Stub) Processes(ctx context.Context) ([]Process, error) {
	return s.ProcList, nil
}
