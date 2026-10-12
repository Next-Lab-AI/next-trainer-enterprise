<script setup lang="ts">
import { ElMessage } from "element-plus"
import { onMounted, reactive, ref } from "vue"
import { useI18n } from "vue-i18n"
import { useRouter } from "vue-router"

import { datasetsApi, jobsApi, poolsApi } from "../api/resources"
import type { Dataset, Pool } from "../api/types"

const { t } = useI18n()
const router = useRouter()

const pools = ref<Pool[]>([])
const datasets = ref<Dataset[]>([])
const submitting = ref(false)

const form = reactive({
  poolId: "",
  engine: "next-integrated",
  engineVersion: "",
  datasetIds: [] as string[],
  gpus: 1,
  gpuModel: "",
})

const ni = reactive({
  modelType: "sdxl",
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
        <el-select v-model="form.engine">
          <el-option label="next-integrated" value="next-integrated" />
          <el-option label="sd-scripts" value="sd-scripts" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('jobs.engineVersion')">
        <el-input v-model="form.engineVersion" />
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
        <el-input-number v-model="form.gpus" :min="1" :max="8" />
      </el-form-item>
      <el-form-item :label="t('jobs.gpuModel')">
        <el-input v-model="form.gpuModel" />
      </el-form-item>

      <template v-if="form.engine === 'next-integrated'">
        <el-form-item :label="t('jobs.modelType')">
          <el-select v-model="ni.modelType" style="width: 200px">
            <el-option label="SDXL" value="sdxl" />
            <el-option label="SD1.5" value="sd15" />
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
        <el-button type="primary" :loading="submitting" :disabled="!form.poolId" @click="onSubmit">
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
</style>
