# 环境安装与环境管理（Provisioning）

## 问题

企业版放弃"单一大 Python 环境"幻想：训练栈依赖冲突是常态，
venv 照旧，**按引擎（必要时按任务）独立环境**。
要解决的是社区版遗留的"1 个主环境 + N 个副环境，没人说得清哪个 python 在跑"：

- 环境的创建、审计、复用、回收全部由 **server 下发指令、agent 幂等执行**；
- 每个任务跑在哪个 venv 是显式数据，不是隐式约定。

## 环境布局（agent 节点）

```
<agent_root>/
├── engines/<engine>/<version>/      # vendor 引擎源码（server 下发或随包预装）
├── envs/<engine>-<deps_hash>/       # venv，按依赖指纹命名可复用
└── jobs/<job_id>/                   # 任务工作目录
```

- venv 命名含依赖指纹（requirements/constraints 哈希）：
  同指纹环境直接复用，避免每个任务重建。
- **agent 无状态**：不引入本地状态库；环境清单、指纹、健康状态
  由扫描 `envs/` 目录与冒烟审计**实时推导**（磁盘即事实），
  来源指令 `command_id` 记在环境目录内的元数据文件里（幂等追溯用）。

## provision 指令执行流程

0. 触发方式：admin 在 web 节点详情发起"安装引擎"（运维操作），
   server 下发指令；引擎包作为资产纳入 server 存储层
   （随全家桶预装或 admin 上传），agent 拉取 + sha256 校验。
1. server 下发 `provision { engine, version, constraints, command_id }`。
2. agent 计算指纹：已存在且健康 → 直接回报 ready（幂等）。
3. 否则创建 venv → 安装依赖 → **审计**（import 冒烟、torch/CUDA 自检、
   GPU 可见性）→ 回报 ready/broken + 审计日志。
4. broken 环境可修复重试；连续失败标记并上报，不再自动重试。

## 网络与私有部署

- 客户环境常有网闸：provision 的所有下载源可配置
  （pip index、HF endpoint、GitHub 前缀），支持指向客户内网镜像。
- 交付包提供"全离线介质"选项：vendor 源码 + wheelhouse + 底座模型
  打包，agent 全程不出内网。

## 回收

- 引用计数：环境被 N 个历史任务引用；server 下发 `env.gc` 时
  回收零引用且超龄的环境。
- 引擎版本升级 = 新指纹新目录，旧环境自然衰亡，不做原地升级。

## 不采用

- conda/系统级 Python 管理（venv + uv 足够，少一层是一个）。
- 容器作为唯一隔离手段（客户 GPU 栈千差万别，首版进程隔离 + venv；
  个别硬冲突依赖后续再引入容器 worker）。
