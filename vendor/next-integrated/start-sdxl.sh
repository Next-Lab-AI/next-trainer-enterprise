accelerate launch --mixed_precision bf16 sdxl_train.py \
  --pretrained_model_name_or_path "/home/brian/ComfyUI/models/checkpoints/tIllunai3_v8.safetensors" \
  --dataset_config "train_data/test1/config.toml" \
  --output_dir "output" \
  --output_name "sdxl_finetuned" \
  --train_text_encoder \
  --learning_rate 1e-5 \
  --learning_rate_te1 5e-6 \
  --learning_rate_te2 2e-6
