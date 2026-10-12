package engines

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Params map[string]any

var known = map[string][]string{
	"sd-scripts": {
		"pretrained_model_name_or_path", "train_data_dir", "reg_data_dir",
		"output_name", "resolution", "train_batch_size", "max_train_epochs",
		"learning_rate", "network_dim", "network_alpha", "optimizer_type",
		"lr_scheduler", "network_module", "clip_skip", "mixed_precision",
		"save_every_n_epochs", "seed", "max_token_length", "xformers",
		"cache_latents", "shuffle_caption", "keep_tokens", "min_snr_gamma",
	},
	"next-integrated": {
		"model_type", "pretrained_model", "train_data_dir", "output_name",
		"resolution", "batch_size", "max_steps", "learning_rate", "optimizer",
		"network_dim", "network_alpha", "seed", "weight_dtype",
		"train_text_encoder", "gradient_checkpointing", "max_grad_norm",
		"tokenizer", "tokenizer1", "tokenizer2", "vae",
	},
}

func Supported(engine string) bool {
	_, ok := known[engine]
	return ok
}

func Render(engine, version string, p Params, jobID string) (string, error) {
	keys, ok := known[engine]
	if !ok {
		return "", fmt.Errorf("engines: unsupported engine %q", engine)
	}
	var b strings.Builder
	b.WriteString("# rendered by next-trainer server\n")
	b.WriteString("# engine: " + engine + " " + version + "\n")

	// next-integrated 的 train.py 按模型 section（[sdxl]/[sd15]）读取配置
	skip := map[string]bool{}
	if engine == "next-integrated" {
		modelType, _ := p["model_type"].(string)
		if modelType == "" {
			modelType = "sdxl"
		}
		if modelType != "sdxl" && modelType != "sd15" {
			return "", fmt.Errorf("engines: unsupported model_type %q", modelType)
		}
		b.WriteString("[" + modelType + "]\n")
		skip["model_type"] = true
	} else {
		b.WriteString("output_dir = \"output\"\n")
		b.WriteString("logging_dir = \"logs\"\n")
		b.WriteString("job_id = " + strconv.Quote(jobID) + "\n")
	}

	seen := map[string]bool{}
	write := func(k string, v any) {
		if skip[k] {
			return
		}
		seen[k] = true
		b.WriteString(k + " = " + tomlValue(v) + "\n")
	}
	for _, k := range keys {
		if v, ok := p[k]; ok {
			write(k, v)
		}
	}
	var extra []string
	for k := range p {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		write(k, p[k])
	}
	return b.String(), nil
}

func tomlValue(v any) string {
	switch t := v.(type) {
	case string:
		return strconv.Quote(t)
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = tomlValue(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return strconv.Quote(fmt.Sprint(v))
	}
}
