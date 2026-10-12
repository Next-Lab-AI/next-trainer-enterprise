<script setup lang="ts">
import { ElMessage } from "element-plus"
import { computed, onMounted, onUnmounted, ref } from "vue"
import { useI18n } from "vue-i18n"
import { useRoute } from "vue-router"

import { jobsApi } from "../api/resources"
import type { Job, JobEvent } from "../api/types"
import { formatTime, statusTagType } from "../utils/format"

const { t } = useI18n()
const route = useRoute()

const job = ref<Job | null>(null)
const events = ref<JobEvent[]>([])
const logLines = ref<string[]>([])
const autoFollow = ref(true)
const logBox = ref<HTMLElement | null>(null)
let eventSource: EventSource | undefined
let pollTimer: ReturnType<typeof setInterval> | undefined

const id = computed(() => String(route.params.id))

function isActive(j: Job) {
  return ["QUEUED", "CREATED", "WAITING", "RUNNING"].includes(j.status)
}

async function refresh() {
  try {
    job.value = await jobsApi.get(id.value)
    events.value = await jobsApi.events(id.value)
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  }
}

function openStream() {
  eventSource?.close()
  eventSource = new EventSource(jobsApi.logsUrl(id.value))
  eventSource.onmessage = (ev) => {
    logLines.value.push(ev.data)
    if (logLines.value.length > 5000) {
      logLines.value.splice(0, logLines.value.length - 5000)
    }
    if (autoFollow.value) {
      requestAnimationFrame(() => {
        logBox.value?.scrollTo({ top: logBox.value.scrollHeight })
      })
    }
  }
  eventSource.addEventListener("done", () => {
    eventSource?.close()
  })
  eventSource.onerror = () => {
    eventSource?.close()
  }
}

onMounted(async () => {
  await refresh()
  openStream()
  pollTimer = setInterval(async () => {
    await refresh()
    if (job.value && !isActive(job.value) && pollTimer) {
      clearInterval(pollTimer)
      pollTimer = undefined
    }
  }, 4000)
})

onUnmounted(() => {
  eventSource?.close()
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<template>
  <div v-if="job">
    <h2>{{ t("jobs.detail") }} <el-tag :type="statusTagType(job.status)">{{ job.status }}</el-tag></h2>
    <el-descriptions :column="2" border>
      <el-descriptions-item label="job_id">{{ job.job_id }}</el-descriptions-item>
      <el-descriptions-item :label="t('jobs.engine')">{{ job.engine }} {{ job.engine_version }}</el-descriptions-item>
      <el-descriptions-item :label="t('jobs.createdAt')">{{ formatTime(job.created_at) }}</el-descriptions-item>
      <el-descriptions-item :label="t('jobs.startedAt')">{{ formatTime(job.started_at) }}</el-descriptions-item>
      <el-descriptions-item :label="t('jobs.endedAt')">{{ formatTime(job.ended_at) }}</el-descriptions-item>
      <el-descriptions-item :label="t('jobs.exitCode')">{{ job.exit_code ?? "-" }}</el-descriptions-item>
    </el-descriptions>

    <h3>{{ t("jobs.logs") }}</h3>
    <div ref="logBox" class="log-box">
      <div v-for="(line, i) in logLines" :key="i" class="log-line">{{ line }}</div>
      <div v-if="logLines.length === 0" class="log-line muted">{{ t("app.empty") }}</div>
    </div>

    <h3>{{ t("jobs.events") }}</h3>
    <el-table :data="[...events].reverse()">
      <el-table-column prop="seq" label="#" width="80" />
      <el-table-column prop="type" label="Type" width="180" />
      <el-table-column :label="t('jobs.createdAt')" width="170">
        <template #default="{ row }">{{ formatTime(row.server_ts) }}</template>
      </el-table-column>
      <el-table-column label="Payload">
        <template #default="{ row }">
          <code class="payload">{{ row.payload }}</code>
        </template>
      </el-table-column>
    </el-table>

    <h3>config.toml</h3>
    <pre class="config">{{ job.config_toml }}</pre>
  </div>
</template>

<style scoped>
.log-box {
  background: #111;
  color: #d4d4d4;
  padding: 12px;
  border-radius: 4px;
  height: 320px;
  overflow-y: auto;
  font-family: monospace;
  font-size: 12px;
}
.log-line {
  white-space: pre-wrap;
  word-break: break-all;
}
.muted {
  color: #666;
}
.payload {
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-all;
}
.config {
  background: var(--el-fill-color-light);
  padding: 12px;
  border-radius: 4px;
  overflow-x: auto;
  font-size: 12px;
}
</style>
