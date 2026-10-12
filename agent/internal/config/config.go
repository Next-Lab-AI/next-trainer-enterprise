package config

import (
	"flag"
	"fmt"
)

type stringList []string

func (s *stringList) String() string { return fmt.Sprint([]string(*s)) }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

type Config struct {
	Server      string
	Token       string
	Root        string
	Description string
	Pool        string
	BasePythons []string
	BufferMB    int
}

func ParseFlags(args []string) (Config, error) {
	var c Config
	var pythons stringList
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	fs.StringVar(&c.Server, "server", "", "server ws(s) URL")
	fs.StringVar(&c.Token, "token", "", "agent access token (empty allowed only in enrollment window)")
	fs.StringVar(&c.Root, "root", "", "agent root directory")
	fs.StringVar(&c.Description, "description", "", "node description (initial suggestion)")
	fs.StringVar(&c.Pool, "pool", "", "initial pool suggestion; empty = default")
	fs.Var(&pythons, "base-python", "registered base python path (repeatable)")
	fs.IntVar(&c.BufferMB, "buffer-mb", 8, "upload buffer size in MB")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if c.Server == "" || c.Root == "" {
		return Config{}, fmt.Errorf("--server and --root are required")
	}
	c.BasePythons = pythons
	return c, nil
}
