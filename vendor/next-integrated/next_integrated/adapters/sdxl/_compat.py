"""transformers 5.x 兼容 shim：种子代码仍引用被移除的旧名。

必须在 import 任何 library/ 模块之前执行（lpw_stable_diffusion 等采样
管线 from transformers import CLIPFeatureExtractor，5.x 已删除，现名
CLIPImageProcessor）。不改 vendor 种子源码，只在 import 期补别名。

transformers 5.5 的 _LazyModule 有两个坑：
- 已删除符号 getattr 抛 AttributeError/ImportError 混用；
- __init__ 会**替换** sys.modules["transformers"]，且替换发生在首次
  解析符号之后——必须先触发一次 getattr 拿到最终模块对象，再把别名
  打在 sys.modules 里的对象上。
"""

import sys

import transformers

_image_processor = transformers.CLIPImageProcessor  # 触发 sys.modules 替换
_lazy = sys.modules["transformers"]
try:
    _lazy.CLIPFeatureExtractor
except (AttributeError, ImportError):
    _lazy.CLIPFeatureExtractor = _image_processor
