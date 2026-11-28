<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";
import { api, postJson } from "../api";
import { toast } from "../composables/useToast";
import FxSvg from "./FxSvg.vue";
import FxToggle from "./primitives/FxToggle.vue";
import FxAlert from "./primitives/FxAlert.vue";

type McpSettings = {
  configured: boolean;
  scopes: string[];
  available: string[];
  all_scopes: string[];
  url: string;
  token?: string;
  token_created_at?: string;
};

const ACTIONS = ["read", "create", "update", "delete"] as const;

const { t } = useI18n();
const state = ref<McpSettings | null>(null);
const token = ref("");
const err = ref("");
const busy = ref(false);

const resources = computed(() => [...new Set((state.value?.all_scopes ?? []).map((s) => s.split(".")[0]!))]);
const clientCommand = computed(
  () =>
    `claude mcp add --transport http findus ${state.value?.url ?? ""} --header "X-API-Key: ${token.value}"`,
);

async function run(request: () => Promise<McpSettings>, success?: string) {
  busy.value = true;
  err.value = "";
  try {
    const r = await request();
    state.value = r;
    // The token is only returned right after it was generated.
    if (r.token) token.value = r.token;
    if (!r.configured) token.value = "";
    if (success) toast.success(success);
  } catch (e) {
    err.value = e instanceof Error ? e.message : t("common.saveFailed");
    toast.error(err.value);
  } finally {
    busy.value = false;
  }
}

onMounted(() => run(() => api<McpSettings>("/api/mcp")));

function generate() {
  if (state.value?.configured && !window.confirm(t("mcp.confirmRegenerate"))) return;
  void run(() => postJson<McpSettings>("/api/mcp/token", {}), t("mcp.tokenCreated"));
}

function revoke() {
  if (!window.confirm(t("mcp.confirmRevoke"))) return;
  void run(() => postJson<McpSettings>("/api/mcp/revoke", {}), t("mcp.revoked"));
}

function setScope(scope: string, on: boolean) {
  const rest = (state.value?.scopes ?? []).filter((s) => s !== scope);
  const scopes = on ? [...rest, scope] : rest;
  void run(() => postJson<McpSettings>("/api/mcp/scopes", { scopes }), t("mcp.saved"));
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(t("mcp.copied"));
  } catch {
    // The clipboard API is unavailable on plain http; the text stays selectable.
    toast.error(t("mcp.copyFailed"));
  }
}
</script>

<template>
  <section class="fx-card overflow-hidden p-0" aria-labelledby="profile-mcp-heading">
    <div class="flex items-center gap-3 border-b border-zinc-100 px-5 py-4">
      <span class="fx-home-stat-icon" aria-hidden="true"><FxSvg name="gear" class="fx-icon" /></span>
      <div>
        <h2 id="profile-mcp-heading" class="text-sm font-semibold uppercase tracking-wide text-zinc-500">{{ $t("mcp.title") }}</h2>
        <p class="text-xs text-zinc-400">{{ $t("mcp.help") }}</p>
      </div>
    </div>
    <div class="space-y-4 p-5 sm:p-6">
      <FxAlert v-if="err">{{ err }}</FxAlert>
      <template v-if="state">
        <div>
          <p class="fx-label">{{ $t("mcp.endpoint") }}</p>
          <code class="block break-all text-sm text-zinc-800">{{ state.url }}</code>
        </div>

        <div v-if="token" class="space-y-2 rounded-xl border border-zinc-200 bg-zinc-50 p-3">
          <p class="text-sm font-medium text-zinc-800">{{ $t("mcp.tokenOnce") }}</p>
          <code class="block select-all break-all text-xs text-zinc-800">{{ token }}</code>
          <code class="block select-all break-all text-xs text-zinc-600">{{ clientCommand }}</code>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="fx-btn-secondary text-xs" @click="copy(token)">{{ $t("mcp.copyToken") }}</button>
            <button type="button" class="fx-btn-secondary text-xs" @click="copy(clientCommand)">{{ $t("mcp.copyCommand") }}</button>
          </div>
        </div>

        <div v-if="state.configured" class="overflow-x-auto">
          <p class="mb-2 text-xs text-zinc-500">{{ $t("mcp.permissionsHelp") }}</p>
          <table class="w-full text-sm">
            <thead>
              <tr class="text-left text-xs uppercase tracking-wide text-zinc-400">
                <th class="py-2 pr-3 font-semibold" scope="col">{{ $t("mcp.resource") }}</th>
                <th v-for="a in ACTIONS" :key="a" class="px-2 py-2 text-center font-semibold" scope="col">{{ $t("mcp.actions." + a) }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="res in resources" :key="res" class="border-t border-zinc-100">
                <th class="py-2.5 pr-3 text-left font-medium text-zinc-800" scope="row">{{ $t("mcp.resources." + res) }}</th>
                <td v-for="a in ACTIONS" :key="a" class="px-2 py-2.5 text-center">
                  <FxToggle
                    :model-value="state.scopes.includes(res + '.' + a) && state.available.includes(res + '.' + a)"
                    :disabled="busy || !state.available.includes(res + '.' + a)"
                    :aria-label="$t('mcp.resources.' + res) + ': ' + $t('mcp.actions.' + a)"
                    @update:model-value="(v: boolean) => setScope(res + '.' + a, v)"
                  />
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div class="flex flex-wrap gap-3">
          <button type="button" :class="state.configured ? 'fx-btn-secondary text-sm' : 'fx-btn-primary text-sm'" :disabled="busy" @click="generate">
            {{ state.configured ? $t("mcp.regenerate") : $t("mcp.generate") }}
          </button>
          <button v-if="state.configured" type="button" class="fx-btn-secondary text-sm" :disabled="busy" @click="revoke">
            {{ $t("mcp.revoke") }}
          </button>
        </div>
      </template>
    </div>
  </section>
</template>
