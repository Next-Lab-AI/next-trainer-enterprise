<script setup lang="ts">
import { onMounted } from "vue"
import { useRoute } from "vue-router"

import AppShell from "./layouts/AppShell.vue"
import { useAuthStore } from "./stores/auth"

const route = useRoute()
const auth = useAuthStore()

onMounted(() => {
  void auth.fetchMe()
})
</script>

<template>
  <router-view v-if="route.meta.public" />
  <AppShell v-else />
</template>
