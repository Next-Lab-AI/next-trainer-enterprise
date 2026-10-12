<script setup lang="ts">
import { ElMessage, ElMessageBox } from "element-plus"
import { onMounted, reactive, ref } from "vue"
import { useI18n } from "vue-i18n"

import { adminApi } from "../api/resources"
import type { AccessTokenInfo, User } from "../api/types"
import { formatTime } from "../utils/format"

const { t } = useI18n()
const users = ref<User[]>([])
const tokens = ref<AccessTokenInfo[]>([])
const createVisible = ref(false)
const tokenVisible = ref(false)
const shownToken = ref("")
const createForm = reactive({ username: "", password: "", role: "user" })
const tokenForm = reactive({ label: "" })

async function refresh() {
  users.value = await adminApi.listUsers()
  tokens.value = await adminApi.listTokens()
}

async function onCreateUser() {
  try {
    await adminApi.createUser(createForm.username, createForm.password, createForm.role)
    createVisible.value = false
    createForm.username = ""
    createForm.password = ""
    await refresh()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  }
}

async function onToggleDisabled(u: User) {
  await adminApi.updateUser(u.id, { disabled: !u.disabled })
  await refresh()
}

async function onResetPassword(u: User) {
  const { value } = await ElMessageBox.prompt(t("admin.resetPassword"), u.username, {
    inputType: "password",
    inputPattern: /.+/,
  })
  await adminApi.updateUser(u.id, { password: value })
  ElMessage.success(t("app.confirm"))
}

async function onCreateToken() {
  try {
    const resp = await adminApi.createToken(tokenForm.label)
    shownToken.value = resp.token
    tokenVisible.value = false
    tokenForm.label = ""
    await refresh()
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  }
}

async function onRevokeToken(tok: AccessTokenInfo) {
  await adminApi.revokeToken(tok.id)
  await refresh()
}

onMounted(refresh)
</script>

<template>
  <div>
    <div class="header">
      <h2>{{ t("admin.users") }}</h2>
      <el-button type="primary" @click="createVisible = true">{{ t("admin.createUser") }}</el-button>
    </div>
    <el-table :data="users">
      <el-table-column :label="t('app.username')" prop="username" />
      <el-table-column :label="t('admin.role')" prop="role" width="100">
        <template #default="{ row }">
          <el-tag :type="row.role === 'admin' ? 'danger' : 'info'">{{ row.role }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('app.status')" width="100">
        <template #default="{ row }">
          <el-tag v-if="row.disabled" type="warning">{{ t("admin.disabled") }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('datasets.createdAt')" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('app.actions')" width="240">
        <template #default="{ row }">
          <el-button size="small" @click="onToggleDisabled(row)">
            {{ row.disabled ? t("admin.enable") : t("admin.disable") }}
          </el-button>
          <el-button size="small" @click="onResetPassword(row)">{{ t("admin.resetPassword") }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="header tokens-header">
      <h2>{{ t("admin.tokens") }}</h2>
      <el-button type="primary" @click="tokenVisible = true">{{ t("admin.createToken") }}</el-button>
    </div>
    <el-table :data="tokens">
      <el-table-column :label="t('admin.label')" prop="label" />
      <el-table-column :label="t('datasets.createdAt')" width="170">
        <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('admin.expiresAt')" width="170">
        <template #default="{ row }">{{ row.expires_at ? formatTime(row.expires_at) : t("admin.never") }}</template>
      </el-table-column>
      <el-table-column :label="t('app.status')" width="100">
        <template #default="{ row }">
          <el-tag v-if="row.revoked_at" type="warning">{{ t("admin.revoke") }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('app.actions')" width="100">
        <template #default="{ row }">
          <el-button v-if="!row.revoked_at" size="small" type="danger" @click="onRevokeToken(row)">
            {{ t("admin.revoke") }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="createVisible" :title="t('admin.createUser')" width="420px">
      <el-form label-width="100px">
        <el-form-item :label="t('app.username')">
          <el-input v-model="createForm.username" />
        </el-form-item>
        <el-form-item :label="t('app.password')">
          <el-input v-model="createForm.password" type="password" />
        </el-form-item>
        <el-form-item :label="t('admin.role')">
          <el-select v-model="createForm.role">
            <el-option label="user" value="user" />
            <el-option label="admin" value="admin" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">{{ t("app.cancel") }}</el-button>
        <el-button type="primary" @click="onCreateUser">{{ t("app.confirm") }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="tokenVisible" :title="t('admin.createToken')" width="420px">
      <el-form label-width="100px">
        <el-form-item :label="t('admin.label')">
          <el-input v-model="tokenForm.label" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="tokenVisible = false">{{ t("app.cancel") }}</el-button>
        <el-button type="primary" @click="onCreateToken">{{ t("app.confirm") }}</el-button>
      </template>
    </el-dialog>

    <el-dialog :model-value="shownToken !== ''" :title="t('admin.createToken')" width="560px" @close="shownToken = ''">
      <p>{{ t("admin.tokenShownOnce") }}</p>
      <code class="token-box">{{ shownToken }}</code>
      <template #footer>
        <el-button type="primary" @click="shownToken = ''">{{ t("app.confirm") }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.tokens-header {
  margin-top: 32px;
}
.token-box {
  display: block;
  padding: 12px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  word-break: break-all;
  user-select: all;
}
</style>
