import torch

from next_integrated.core.checkpoint import load_lora, load_metadata, save_lora
from next_integrated.core.network import LoRANetwork


class Transformer2DModel(torch.nn.Module):
    def __init__(self):
        super().__init__()
        self.attn1 = torch.nn.ModuleDict(
            {"to_q": torch.nn.Linear(8, 8), "to_k": torch.nn.Linear(8, 8), "to_v": torch.nn.Linear(8, 8)}
        )


class CLIPAttention(torch.nn.Module):
    def __init__(self):
        super().__init__()
        self.q_proj = torch.nn.Linear(8, 8)
        self.k_proj = torch.nn.Linear(8, 8)


class CLIPMLP(torch.nn.Module):
    def __init__(self):
        super().__init__()
        self.fc1 = torch.nn.Linear(8, 16)


class ToyUnet(torch.nn.Module):
    def __init__(self):
        super().__init__()
        self.down_blocks = torch.nn.ModuleList(
            [torch.nn.ModuleDict({"attentions": torch.nn.ModuleList([Transformer2DModel()])})]
        )


class ToyTextEncoder(torch.nn.Module):
    def __init__(self):
        super().__init__()
        self.layers = torch.nn.ModuleList(
            [torch.nn.ModuleDict({"self_attn": CLIPAttention(), "mlp": CLIPMLP()})]
        )


def build_network(alpha, lora_dim=4):
    torch.manual_seed(42)
    unet = ToyUnet()
    te = ToyTextEncoder()
    net = LoRANetwork(te, unet, lora_dim=lora_dim, alpha=alpha)
    return net


def randomize_lora(net):
    with torch.no_grad():
        for _, lora in net.named_lora_modules():
            lora.lora_down.weight.normal_(0, 0.1)
            lora.lora_up.weight.normal_(0, 0.1)


def test_module_discovery():
    net = build_network(alpha=4)
    names = [n for n, _ in net.named_lora_modules()]
    # unet: Transformer2DModel 下 3 个 Linear；TE: CLIPAttention 2 个 + CLIPMLP 1 个
    assert len(net.unet_loras) == 3
    assert len(net.text_encoder_loras) == 3
    assert all(n.startswith(("unet.", "text_encoder.")) for n in names)
    assert "unet.down_blocks.0.attentions.0.attn1.to_q" in names
    assert "text_encoder.layers.0.self_attn.q_proj" in names


def test_zero_init_and_forward(tmp_path):
    torch.manual_seed(0)
    unet = ToyUnet()
    net = LoRANetwork(None, unet, lora_dim=4, alpha=4, train_text_encoder=False)
    x = torch.randn(2, 8)
    ref = unet.down_blocks[0]["attentions"][0].attn1["to_q"](x)
    net.apply_to(apply_text_encoder=False)
    out = unet.down_blocks[0]["attentions"][0].attn1["to_q"](x)
    assert torch.equal(ref, out)  # up 零初始化，初始 delta 为零


def test_save_load_roundtrip(tmp_path):
    net = build_network(alpha=2)  # scale = 0.5，故意 != 1 验证烘焙
    randomize_lora(net)
    deltas_before = {n: lora.get_delta_weight() for n, lora in net.named_lora_modules()}

    ckpt = tmp_path / "lora.safetensors"
    save_lora(net, ckpt, metadata={"base_model": "toy", "steps": 10})

    from safetensors import safe_open

    with safe_open(str(ckpt), framework="pt") as f:
        keys = list(f.keys())
    assert all(k.endswith((".lora_A.weight", ".lora_B.weight")) for k in keys)
    assert not any(k.startswith("lora_unet") or k.startswith("lora_te") for k in keys)

    md = load_metadata(ckpt)
    assert md["format"] == "next-integrated.lora"
    assert md["rank"] == "4" and md["alpha"] == "2.0"
    assert md["base_model"] == "toy"

    # resume：以 alpha == rank（scale=1）构建，加载后 delta 应与保存前一致
    net2 = build_network(alpha=4)
    load_lora(net2, ckpt)
    for name, lora in net2.named_lora_modules():
        assert torch.allclose(lora.get_delta_weight(), deltas_before[name], atol=1e-6), name


def test_load_rejects_scaled_network(tmp_path):
    net = build_network(alpha=2)
    ckpt = tmp_path / "lora.safetensors"
    save_lora(net, ckpt)
    net2 = build_network(alpha=2)  # scale != 1，必须拒绝
    try:
        load_lora(net2, ckpt)
        raise AssertionError("should have raised")
    except ValueError:
        pass
