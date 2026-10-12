import { createI18n } from "vue-i18n"
import enUS from "./messages/en-US"
import zhCN from "./messages/zh-CN"

const LOCALE_KEY = "nt_locale"

export type AppLocale = "zh-CN" | "en-US"

export function getStoredLocale(): AppLocale {
  const v = localStorage.getItem(LOCALE_KEY)
  return v === "en-US" ? "en-US" : "zh-CN"
}

export function setStoredLocale(locale: AppLocale) {
  localStorage.setItem(LOCALE_KEY, locale)
}

export const i18n = createI18n({
  legacy: false,
  locale: getStoredLocale(),
  fallbackLocale: "zh-CN",
  messages: { "zh-CN": zhCN, "en-US": enUS },
})
