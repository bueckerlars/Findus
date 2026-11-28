import type { WritableComputedRef } from "vue";
import { createApp, watch } from "vue";
import App from "./App.vue";
import { i18n, persistLocale, syncHtmlLang } from "./i18n";
import type { SupportedLocale } from "./locale/constants";
import { hydrateFxThemeFromStorage } from "./composables/useFxTheme";
import { router } from "./router";
// Inter is bundled: loading it from Google would send every visitor's IP there.
import "@fontsource/inter/400.css";
import "@fontsource/inter/500.css";
import "@fontsource/inter/600.css";
import "@fontsource/inter/700.css";
import "./style.css";
import "./fx-dark-overrides.css";

hydrateFxThemeFromStorage();

const app = createApp(App);
app.use(i18n);
app.use(router);
watch(
  () => (i18n.global.locale as WritableComputedRef<string>).value,
  (loc) => {
    const s = String(loc);
    syncHtmlLang(s);
    persistLocale(s as SupportedLocale);
  },
  { immediate: true },
);
app.mount("#app");
