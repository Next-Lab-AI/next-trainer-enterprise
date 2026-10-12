package engines

import (
	"strings"
	"testing"
)

func TestRenderNextIntegratedSection(t *testing.T) {
	cfg, err := Render("next-integrated", "0.1.0", Params{
		"model_type":       "sd15",
		"pretrained_model": "/models/sd15.safetensors",
		"resolution":       512,
		"batch_size":       2,
		"max_steps":        20,
		"learning_rate":    1e-4,
		"network_dim":      8,
	}, "job1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg, "[sd15]\n") {
		t.Fatalf("missing [sd15] section:\n%s", cfg)
	}
	if strings.Contains(cfg, "model_type") {
		t.Fatalf("model_type should be consumed as section name:\n%s", cfg)
	}
	for _, want := range []string{`pretrained_model = "/models/sd15.safetensors"`, "resolution = 512", "learning_rate = 0.0001"} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q:\n%s", want, cfg)
		}
	}
}

func TestRenderNextIntegratedDefaultSDXL(t *testing.T) {
	cfg, err := Render("next-integrated", "0.1.0", Params{"max_steps": 5}, "job1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg, "[sdxl]\n") {
		t.Fatalf("default section should be [sdxl]:\n%s", cfg)
	}
}

func TestRenderNextIntegratedBadModelType(t *testing.T) {
	if _, err := Render("next-integrated", "0.1.0", Params{"model_type": "flux"}, "job1"); err == nil {
		t.Fatal("expected error for unsupported model_type")
	}
}

func TestRenderSDScriptsUnchanged(t *testing.T) {
	cfg, err := Render("sd-scripts", "", Params{"max_train_epochs": 10}, "job1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg, `job_id = "job1"`) || strings.Contains(cfg, "[") {
		t.Fatalf("sd-scripts render should stay flat:\n%s", cfg)
	}
}
