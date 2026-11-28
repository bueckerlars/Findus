<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";
import jsQR from "jsqr";
import { api, postJson } from "../api";
import { toast } from "../composables/useToast";
import { useCreateModals } from "../composables/useCreateModals";
import { useSession } from "../session";
import { PERM_ITEMS_WRITE } from "../permissions";
import FxModal from "./FxModal.vue";
import FxAlert from "./primitives/FxAlert.vue";

const props = defineProps<{ locationId: string; locationName: string }>();
const emit = defineEmits<{ close: []; moved: [] }>();

const router = useRouter();
const { t } = useI18n();
const { isAdmin, can } = useSession();
const canWrite = computed(() => isAdmin.value || can(PERM_ITEMS_WRITE));
const { openCreateItem } = useCreateModals();

type Result =
  | { kind: "here" | "elsewhere"; id: string; name: string; from: string }
  | { kind: "location"; path: string }
  | { kind: "message"; text: string };

const video = ref<HTMLVideoElement | null>(null);
const scanning = ref(false);
const loading = ref(false);
const result = ref<Result | null>(null);
const busy = ref(false);

const canvas = document.createElement("canvas");
let stream: MediaStream | null = null;
let timer: number | undefined;
// Bumped by stop(), so a camera or request that resolves late is ignored.
let run = 0;

function stop() {
  run++;
  window.clearTimeout(timer);
  stream?.getTracks().forEach((track) => track.stop());
  stream = null;
  scanning.value = false;
}

async function start() {
  stop();
  const mine = run;
  result.value = null;
  scanning.value = true;
  try {
    const s = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" } });
    if (mine !== run) {
      s.getTracks().forEach((track) => track.stop());
      return;
    }
    stream = s;
    await nextTick();
    video.value!.srcObject = s;
    await video.value!.play();
    tick(mine);
  } catch {
    // Also covers plain HTTP, where navigator.mediaDevices does not exist.
    if (mine !== run) return;
    stop();
    result.value = { kind: "message", text: t("qrScan.noCamera") };
  }
}

// jsQR instead of BarcodeDetector, which Safari and Firefox lack.
function tick(mine: number) {
  if (mine !== run) return;
  const v = video.value;
  if (v && v.videoWidth) {
    const scale = Math.min(1, 800 / v.videoWidth);
    canvas.width = Math.round(v.videoWidth * scale);
    canvas.height = Math.round(v.videoHeight * scale);
    const ctx = canvas.getContext("2d", { willReadFrequently: true })!;
    ctx.drawImage(v, 0, 0, canvas.width, canvas.height);
    const code = jsQR(ctx.getImageData(0, 0, canvas.width, canvas.height).data, canvas.width, canvas.height);
    if (code?.data) {
      void handle(code.data);
      return;
    }
  }
  timer = window.setTimeout(() => tick(mine), 150);
}

async function handle(text: string) {
  stop();
  const mine = run;
  // Only the token counts; the host on a printed label may differ from the one in use.
  const token = text.trim().match(/\/q\/([A-Za-z0-9_-]+)\/?$/)?.[1];
  if (!token) {
    result.value = { kind: "message", text: t("qrScan.unknown") };
    return;
  }
  loading.value = true;
  try {
    const { target } = await api<{ target: string }>("/api/qr/" + token);
    if (mine !== run) return;
    const url = new URL(target, window.location.origin);
    if (url.pathname === "/items/new") {
      if (!canWrite.value) {
        result.value = { kind: "message", text: t("qrScan.noCreate") };
        return;
      }
      emit("close");
      openCreateItem({ locationId: props.locationId, qrToken: url.searchParams.get("qr") ?? token });
    } else if (url.pathname.startsWith("/locations/")) {
      result.value = { kind: "location", path: url.pathname };
    } else {
      const r = await api<{ item: { ID: string; Name: string; LocationID: string }; location_path: { Name: string }[] }>(
        "/api" + url.pathname,
      );
      if (mine !== run) return;
      result.value = {
        kind: r.item.LocationID === props.locationId ? "here" : "elsewhere",
        id: r.item.ID,
        name: r.item.Name,
        from: r.location_path.map((p) => p.Name).join(" / "),
      };
    }
  } catch (e) {
    if (mine !== run) return;
    const msg = e instanceof Error ? e.message : "";
    result.value = { kind: "message", text: !msg || msg === "not found" ? t("qrScan.unknown") : msg };
  } finally {
    loading.value = false;
  }
}

async function move(id: string) {
  busy.value = true;
  try {
    await postJson(`/api/items/${id}/move`, { location_id: props.locationId });
    toast.success(t("qrScan.moved"));
    emit("moved");
    if (result.value?.kind === "elsewhere") result.value = { ...result.value, kind: "here" };
  } catch (e) {
    toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
  } finally {
    busy.value = false;
  }
}

function open(path: string) {
  emit("close");
  void router.push(path);
}

onMounted(start);
onUnmounted(stop);
</script>

<template>
  <FxModal :model-value="true" :title="t('qrScan.title')" @update:model-value="emit('close')">
    <div v-if="scanning">
      <video ref="video" class="aspect-square w-full rounded-xl bg-zinc-900 object-cover" muted playsinline />
      <p class="mt-3 text-sm text-zinc-500">{{ $t("qrScan.aim") }}</p>
    </div>
    <div aria-live="polite">
      <p v-if="loading" class="text-sm text-zinc-500">{{ $t("qrScan.loading") }}</p>
      <FxAlert v-else-if="result?.kind === 'message'" tone="warning">{{ result.text }}</FxAlert>
      <p v-else-if="result?.kind === 'location'" class="text-sm text-zinc-700">{{ $t("qrScan.isLocation") }}</p>
      <p v-else-if="result?.kind === 'here'" class="text-sm text-zinc-700">{{ $t("qrScan.here", { name: result.name }) }}</p>
      <p v-else-if="result?.kind === 'elsewhere'" class="text-sm text-zinc-700">
        {{ $t("qrScan.elsewhere", { name: result.name, from: result.from, to: locationName }) }}
      </p>
    </div>

    <template #footer>
      <div class="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <button type="button" class="fx-btn-secondary w-full sm:w-auto" @click="emit('close')">{{ $t("common.cancel") }}</button>
        <button v-if="result" type="button" class="fx-btn-secondary w-full sm:w-auto" :disabled="busy" @click="start">
          {{ $t("qrScan.again") }}
        </button>
        <button v-if="result?.kind === 'location'" type="button" class="fx-btn-primary w-full sm:w-auto" @click="open(result.path)">
          {{ $t("qrScan.openLocation") }}
        </button>
        <template v-else-if="result?.kind === 'here' || result?.kind === 'elsewhere'">
          <button
            type="button"
            class="w-full sm:w-auto"
            :class="result.kind === 'here' || !canWrite ? 'fx-btn-primary' : 'fx-btn-secondary'"
            :disabled="busy"
            @click="open('/items/' + result.id)"
          >
            {{ $t("qrScan.openItem") }}
          </button>
          <button
            v-if="result.kind === 'elsewhere' && canWrite"
            type="button"
            class="fx-btn-primary w-full sm:w-auto"
            :disabled="busy"
            @click="move(result.id)"
          >
            {{ $t("qrScan.move") }}
          </button>
        </template>
      </div>
    </template>
  </FxModal>
</template>
