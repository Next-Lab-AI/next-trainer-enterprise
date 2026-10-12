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
  engine: "sd-scripts",
  engineVersion: "",
  datasetIds: [] as string[],
  gpus: 1,
  gpuModel: "",
})

const params = ref<{ key: string; value: string }[]>([{ key: "max_train_epochs", value: "10" }])

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

      <el-form-item :label="t('jobs.params')">
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
