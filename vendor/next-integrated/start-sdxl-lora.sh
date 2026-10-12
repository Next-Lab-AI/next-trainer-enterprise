accelerate launch --num_cpu_threads_per_process 1 sdxl_train_network.py  \
 --pretrained_model_name_or_path="/home/brian/ComfyUI/models/checkpoints/tIllunai3_v8.safetensors" \
 --dataset_config="train_data/test1/config.toml" \
 --output_dir="result" \
 --output_name="body_line_v1.3" \
 --save_model_as=safetensors \
 --network_module=networks.lora \
 --network_dim=32 \
 --network_alpha=16 \
 --learning_rate=1e-4 \
 --optimizer_type="AdamW8bit" \
 --lr_scheduler="cosine" \
 --sdpa  \
 --max_train_epochs=50 \
 --save_every_n_epochs=10 \
 --mixed_precision="fp16" \
 --gradient_checkpointing \
 --no_half_vae \
 --enable_bucket --min_bucket_reso=512 --max_bucket_reso=2048
mv result/* /home/brian/ComfyUI/models/loras/
