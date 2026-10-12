<script setup lang="ts">
import { useI18n } from "vue-i18n"

const { t } = useI18n()
</script>

<template>
  <div class="nf">
    <h2>404</h2>
    <p>{{ t("notFound.title") }}</p>
  </div>
</template>

<style scoped>
.nf {
  text-align: center;
  padding-top: 20vh;
}
</style>
