package exec

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

type LineFunc func(stream, line string)

type Options struct {
	Name string
	Args []string
	Dir  string
	Env  []string
}

type Process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
	wg   sync.WaitGroup
}

var sensitiveKeyRe = regexp.MustCompile(`(?i)(TOKEN|KEY|SECRET|PASSWORD)`)

func BuildEnv(base []string, extra map[string]string) []string {
	var out []string
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || sensitiveKeyRe.MatchString(key) {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}

func Start(ctx context.Context, opts Options, onLine LineFunc) (*Process, error) {
	cmd := exec.Command(opts.Name, opts.Args...)
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("exec: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("exec: stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("exec: start %s: %w", opts.Name, err)
	}

	p := &Process{cmd: cmd, done: make(chan struct{})}
	p.wg.Add(2)
	go p.scan(&p.wg, stdout, "stdout", onLine)
	go p.scan(&p.wg, stderr, "stderr", onLine)
	go func() {
		p.wg.Wait()
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *Process) scan(wg *sync.WaitGroup, r io.Reader, stream string, onLine LineFunc) {
	defer wg.Done()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		onLine(stream, sc.Text())
	}
}

func (p *Process) PID() int { return p.cmd.Process.Pid }

func (p *Process) Wait() error {
	<-p.done
	return p.err
}

func (p *Process) Done() <-chan struct{} { return p.done }

func (p *Process) Stop(grace time.Duration) error {
	pgid := p.PID()
	select {
	case <-p.done:
		return nil
	default:
	}

	syscall.Kill(-pgid, syscall.SIGTERM)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-p.done:
		return nil
	case <-timer.C:
	}

	killTree(pgid)
	<-p.done
	return p.err
}

func (p *Process) Kill() error {
	select {
	case <-p.done:
		return nil
	default:
	}
	killTree(p.PID())
	<-p.done
	return p.err
}
