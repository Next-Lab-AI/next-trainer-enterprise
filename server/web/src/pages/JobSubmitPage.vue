<script setup lang="ts">
import { ElMessage } from "element-plus"
import { computed, onMounted, reactive, ref, watch } from "vue"
import { useI18n } from "vue-i18n"
import { useRouter } from "vue-router"

import { datasetsApi, jobsApi, nodesApi, poolsApi } from "../api/resources"
import type { Dataset, Node, Pool } from "../api/types"

const { t } = useI18n()
const router = useRouter()

const pools = ref<Pool[]>([])
const datasets = ref<Dataset[]>([])
const nodes = ref<Node[]>([])
const submitting = ref(false)

const form = reactive({
  poolId: "",
  engine: "",
  engineVersion: "",
  datasetIds: [] as string[],
  gpus: 1,
  gpuModel: "",
})

// 池内在线节点
const poolNodes = computed(() => nodes.value.filter((n) => n.online && n.pools.includes(form.poolId)))

// 引擎注册表：池内在线节点上报的 ready 引擎（name@version）
const engineOptions = computed(() => {
  const seen = new Map<string, { name: string; version: string }>()
  for (const n of poolNodes.value) {
    for (const e of n.engines ?? []) {
      if (e.ready) seen.set(`${e.name}@${e.version}`, { name: e.name, version: e.version })
    }
  }
  return [...seen.values()]
})

// 能跑当前引擎的节点
const engineNodes = computed(() =>
  poolNodes.value.filter((n) =>
    (n.engines ?? []).some((e) => e.ready && e.name === form.engine && (!form.engineVersion || e.version === form.engineVersion)),
  ),
)

// 当前引擎在池内节点上支持的能力（模型类型）并集
const capabilityOptions = computed(() => {
  const caps = new Set<string>()
  for (const n of engineNodes.value) {
    for (const e of n.engines ?? []) {
      if (e.ready && e.name === form.engine) for (const c of e.capabilities ?? []) caps.add(c)
    }
  }
  return [...caps]
})

// 选定模型类型后的可用节点
const eligibleNodes = computed(() =>
  engineNodes.value.filter(
    (n) =>
      !ni.modelType ||
      (n.engines ?? []).some((e) => e.ready && e.name === form.engine && (e.capabilities ?? []).includes(ni.modelType)),
  ),
)

const gpuModelOptions = computed(() => {
  const models = new Set<string>()
  for (const n of eligibleNodes.value) for (const g of n.gpus ?? []) models.add(g.model)
  return [...models]
})

const maxGpus = computed(() => Math.max(1, ...eligibleNodes.value.map((n) => (n.gpus ?? []).length)))

const ni = reactive({
  modelType: "",
  pretrainedModel: "",
  trainDataDir: "",
  resolution: 1024,
  batchSize: 1,
  maxSteps: 100,
  learningRate: 0.0001,
  optimizer: "adamw",
  networkDim: 8,
  networkAlpha: 8,
  seed: 42,
})

// 池变化时重置下游选择
watch(
  () => form.poolId,
  () => {
    form.engine = ""
    form.engineVersion = ""
  },
)
watch(engineOptions, (opts) => {
  if (!form.engine && opts.length > 0) {
    form.engine = opts[0].name
    form.engineVersion = opts[0].version
  }
})
watch(
  () => form.engine,
  (eng) => {
    const opt = engineOptions.value.find((o) => o.name === eng)
    form.engineVersion = opt?.version ?? ""
    if (eng !== "next-integrated") ni.modelType = ""
  },
)
watch(capabilityOptions, (caps) => {
  if (form.engine === "next-integrated" && !caps.includes(ni.modelType)) ni.modelType = caps[0] ?? ""
})

const params = ref<{ key: string; value: string }[]>([])

function addParam() {
  params.value.push({ key: "", value: "" })
}

function removeParam(i: number) {
  params.value.splice(i, 1)
}

function coerce(v: string): unknown {
  if (v === "true") return true
  if (v === "false") return false
  if (v !== "" && !Number.isNaN(Number(v))) return Number(v)
  return v
}

async function onSubmit() {
  submitting.value = true
  try {
    const p: Record<string, unknown> = {}
    if (form.engine === "next-integrated") {
      p.model_type = ni.modelType
      if (ni.pretrainedModel) p.pretrained_model = ni.pretrainedModel
      if (ni.trainDataDir) p.train_data_dir = ni.trainDataDir
      p.resolution = ni.resolution
      p.batch_size = ni.batchSize
      p.max_steps = ni.maxSteps
      p.learning_rate = ni.learningRate
      p.optimizer = ni.optimizer
      p.network_dim = ni.networkDim
      p.network_alpha = ni.networkAlpha
      p.seed = ni.seed
    }
    for (const { key, value } of params.value) {
      if (key) p[key] = coerce(value)
    }
    const job = await jobsApi.submit({
      pool_id: form.poolId,
      engine: form.engine,
      engine_version: form.engineVersion || undefined,
      dataset_ids: form.datasetIds,
      gpus: form.gpus,
      gpu_model: form.gpuModel || undefined,
      params: p,
    })
    ElMessage.success(job.job_id)
    router.push(`/jobs/${job.job_id}`)
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  try {
    pools.value = await poolsApi.list()
    datasets.value = await datasetsApi.list()
    nodes.value = await nodesApi.list()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  }
})
</script>

<template>
  <div>
    <h2>{{ t("jobs.submit") }}</h2>
    <el-form label-width="160px" style="max-width: 720px">
      <el-form-item :label="t('jobs.pool')">
        <el-select v-model="form.poolId">
          <el-option v-for="p in pools" :key="p.pool_id" :label="p.name" :value="p.pool_id" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('jobs.engine')">
        <el-select v-model="form.engine" :disabled="!form.poolId" :placeholder="t('jobs.enginePlaceholder')">
          <el-option
            v-for="o in engineOptions"
            :key="`${o.name}@${o.version}`"
            :label="`${o.name}@${o.version}`"
            :value="o.name"
          />
        </el-select>
        <span v-if="form.poolId && engineOptions.length === 0" class="hint">{{ t("jobs.noEngineInPool") }}</span>
      </el-form-item>
      <el-form-item v-if="form.engine" :label="t('jobs.eligibleNodes')">
        <span v-if="eligibleNodes.length">{{ eligibleNodes.map((n) => n.hostname).join(", ") }}</span>
        <span v-else class="hint">{{ t("jobs.noEligibleNodes") }}</span>
      </el-form-item>
      <el-form-item :label="t('jobs.datasets')">
        <el-select v-model="form.datasetIds" multiple>
          <el-option
            v-for="d in datasets.filter((d) => d.kind === 'dataset')"
            :key="d.dataset_id"
            :label="`${d.name}${d.version ? '@' + d.version : ''}`"
            :value="d.dataset_id"
          />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('jobs.gpus')">
        <el-input-number v-model="form.gpus" :min="1" :max="maxGpus" />
      </el-form-item>
      <el-form-item :label="t('jobs.gpuModel')">
        <el-select v-model="form.gpuModel" clearable style="width: 280px">
          <el-option v-for="m in gpuModelOptions" :key="m" :label="m" :value="m" />
        </el-select>
      </el-form-item>

      <template v-if="form.engine === 'next-integrated'">
        <el-form-item :label="t('jobs.modelType')">
          <el-select v-model="ni.modelType" style="width: 200px">
            <el-option v-for="c in capabilityOptions" :key="c" :label="c" :value="c" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('jobs.pretrainedModel')">
          <el-input v-model="ni.pretrainedModel" placeholder="/path/to/base.safetensors" />
        </el-form-item>
        <el-form-item :label="t('jobs.trainDataDir')">
          <el-input v-model="ni.trainDataDir" placeholder="/path/to/dataset" />
        </el-form-item>
        <el-form-item :label="t('jobs.resolution')">
          <el-input-number v-model="ni.resolution" :min="256" :max="2048" :step="64" />
        </el-form-item>
        <el-form-item :label="t('jobs.batchSize')">
          <el-input-number v-model="ni.batchSize" :min="1" :max="32" />
        </el-form-item>
        <el-form-item :label="t('jobs.maxSteps')">
          <el-input-number v-model="ni.maxSteps" :min="1" :max="1000000" />
        </el-form-item>
        <el-form-item :label="t('jobs.learningRate')">
          <el-input v-model="ni.learningRate" style="width: 200px" />
        </el-form-item>
        <el-form-item :label="t('jobs.optimizer')">
          <el-select v-model="ni.optimizer" style="width: 200px">
            <el-option label="adamw" value="adamw" />
            <el-option label="automagic" value="automagic" />
            <el-option label="automagic2" value="automagic2" />
            <el-option label="automagic3" value="automagic3" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('jobs.networkDim')">
          <el-input-number v-model="ni.networkDim" :min="1" :max="1024" />
        </el-form-item>
        <el-form-item :label="t('jobs.networkAlpha')">
          <el-input-number v-model="ni.networkAlpha" :min="1" :max="1024" />
        </el-form-item>
        <el-form-item :label="t('jobs.seed')">
          <el-input-number v-model="ni.seed" :min="0" />
        </el-form-item>
      </template>

      <el-form-item :label="form.engine === 'next-integrated' ? t('jobs.extraParams') : t('jobs.params')">
        <div class="params">
          <div v-for="(p, i) in params" :key="i" class="param-row">
            <el-input v-model="p.key" placeholder="learning_rate" style="width: 280px" />
            <el-input v-model="p.value" placeholder="0.0001" style="width: 280px" />
            <el-button size="small" @click="removeParam(i)">-</el-button>
          </div>
          <el-button size="small" @click="addParam">+</el-button>
        </div>
      </el-form-item>

      <el-form-item>
        <el-button
          type="primary"
          :loading="submitting"
          :disabled="!form.poolId || !form.engine || eligibleNodes.length === 0"
          @click="onSubmit"
        >
          {{ t("app.submit") }}
        </el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<style scoped>
.params {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.param-row {
  display: flex;
  gap: 8px;
}
.hint {
  margin-left: 12px;
  color: var(--el-color-warning);
  font-size: 12px;
}
</style>
