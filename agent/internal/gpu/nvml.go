package gpu

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

type NVML struct {
	mu      sync.Mutex
	started bool
}

func (n *NVML) ensureInit() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.started {
		return nil
	}
	if ret := nvml.Init(); ret != nvml.SUCCESS {
		return fmt.Errorf("gpu: nvml init: %s", nvml.ErrorString(ret))
	}
	n.started = true
	return nil
}

func (n *NVML) device(i int) (nvml.Device, error) {
	dev, ret := nvml.DeviceGetHandleByIndex(i)
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("gpu: device %d: %s", i, nvml.ErrorString(ret))
	}
	return dev, nil
}

func (n *NVML) count() (int, error) {
	c, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return 0, fmt.Errorf("gpu: device count: %s", nvml.ErrorString(ret))
	}
	return c, nil
}

func (n *NVML) Topology(ctx context.Context) ([]GPU, error) {
	if err := n.ensureInit(); err != nil {
		return nil, err
	}
	count, err := n.count()
	if err != nil {
		return nil, err
	}
	var out []GPU
	for i := 0; i < count; i++ {
		dev, err := n.device(i)
		if err != nil {
			return nil, err
		}
		g := GPU{Index: i, Vendor: "nvidia"}
		if name, ret := dev.GetName(); ret == nvml.SUCCESS {
			g.Model = name
		}
		if uuid, ret := dev.GetUUID(); ret == nvml.SUCCESS {
			g.UUID = uuid
		}
		if mem, ret := dev.GetMemoryInfo(); ret == nvml.SUCCESS {
			g.MemTotalMB = int64(mem.Total / 1024 / 1024)
		} else if ret == nvml.ERROR_NOT_SUPPORTED {
			g.MemTotalMB = hostMemTotalMB()
		}
		out = append(out, g)
	}
	return out, nil
}

func (n *NVML) Sample(ctx context.Context) ([]Sample, error) {
	if err := n.ensureInit(); err != nil {
		return nil, err
	}
	count, err := n.count()
	if err != nil {
		return nil, err
	}
	usedByGPU := n.memUsedByGPU()
	var out []Sample
	for i := 0; i < count; i++ {
		dev, err := n.device(i)
		if err != nil {
			return nil, err
		}
		s := Sample{GPUIndex: i}
		if util, ret := dev.GetUtilizationRates(); ret == nvml.SUCCESS {
			s.Util = int(util.Gpu)
		}
		if mem, ret := dev.GetMemoryInfo(); ret == nvml.SUCCESS {
			s.MemUsedMB = int64(mem.Used / 1024 / 1024)
		} else {
			s.MemUsedMB = usedByGPU[i]
		}
		if power, ret := dev.GetPowerUsage(); ret == nvml.SUCCESS {
			s.PowerW = float64(power) / 1000
		}
		if temp, ret := dev.GetTemperature(nvml.TEMPERATURE_GPU); ret == nvml.SUCCESS {
			s.TempC = float64(temp)
		}
		out = append(out, s)
	}
	return out, nil
}

func (n *NVML) Processes(ctx context.Context) ([]Process, error) {
	if err := n.ensureInit(); err != nil {
		return nil, err
	}
	count, err := n.count()
	if err != nil {
		return nil, err
	}
	var out []Process
	for i := 0; i < count; i++ {
		dev, err := n.device(i)
		if err != nil {
			return nil, err
		}
		procs, ret := dev.GetComputeRunningProcesses()
		if ret != nvml.SUCCESS {
			continue
		}
		for _, p := range procs {
			out = append(out, Process{
				PID:       int(p.Pid),
				GPUIndex:  i,
				MemUsedMB: int64(p.UsedGpuMemory / 1024 / 1024),
			})
		}
	}
	return out, nil
}

func (n *NVML) memUsedByGPU() map[int]int64 {
	procs, err := n.Processes(context.Background())
	if err != nil {
		return nil
	}
	out := map[int]int64{}
	for _, p := range procs {
		out[p.GPUIndex] += p.MemUsedMB
	}
	return out
}

func HostMemTotalMB() int64 { return hostMemTotalMB() }

func hostMemTotalMB() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					return kb / 1024
				}
			}
		}
	}
	return 0
}
