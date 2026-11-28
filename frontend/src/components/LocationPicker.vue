<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, useId, watch } from "vue";
import { api } from "../api";
import { locationChain, locationRows, type LocationNode } from "../utils/locationTree";
import FxSvg from "./FxSvg.vue";

const props = defineProps<{
  modelValue: string;
  id: string;
  /** Location that is shown but cannot be picked. */
  disabledId?: string;
  /** Also blocks everything below disabledId, e.g. when choosing a parent for it. */
  disableSubtree?: boolean;
  /** Adds a first entry with this label and an empty value, e.g. "Top level". */
  noneLabel?: string;
}>();
const emit = defineEmits<{ "update:modelValue": [string] }>();

const valueId = useId();
const root = ref<HTMLElement | null>(null);
const trigger = ref<HTMLButtonElement | null>(null);
const searchInput = ref<HTMLInputElement | null>(null);
const list = ref<HTMLElement | null>(null);
const tree = ref<LocationNode[]>([]);
const loadFailed = ref(false);
const open = ref(false);
const query = ref("");
const expanded = ref(new Set<string>());

const selectedPath = computed(() => {
  if (!props.modelValue) return props.noneLabel ?? "";
  return locationChain(tree.value, props.modelValue).map((n) => n.Name).join(" / ");
});
const searching = computed(() => query.value.trim() !== "");
const rows = computed(() => locationRows(tree.value, expanded.value, query.value));

// shortcut: /api/locations returns at most 500 locations; raise the limit there if an inventory outgrows it.
onMounted(async () => {
  try {
    tree.value = (await api<{ tree: LocationNode[] | null }>("/api/locations")).tree ?? [];
  } catch {
    loadFailed.value = true;
  }
});

function toggleExpanded(id: string) {
  const next = new Set(expanded.value);
  if (!next.delete(id)) next.add(id);
  expanded.value = next;
}

async function show() {
  query.value = "";
  // Open the branches leading to the current choice.
  expanded.value = new Set(locationChain(tree.value, props.modelValue).slice(0, -1).map((n) => n.ID));
  open.value = true;
  await nextTick();
  list.value?.querySelector('[aria-current="true"]')?.scrollIntoView({ block: "nearest" });
  // No autofocus on touch devices: the on-screen keyboard would cover the tree.
  if (window.matchMedia("(pointer: fine)").matches) searchInput.value?.focus({ preventScroll: true });
}

function hide() {
  open.value = false;
  trigger.value?.focus();
}

function isDisabled(id: string) {
  if (!props.disableSubtree) return id === props.disabledId;
  return locationChain(tree.value, id).some((n) => n.ID === props.disabledId);
}

function pick(id: string) {
  if (isDisabled(id)) return;
  emit("update:modelValue", id);
  hide();
}

function pickFirstMatch() {
  const first = rows.value.find((r) => !isDisabled(r.id));
  if (searching.value && first) pick(first.id);
}

// Window capture runs before the modal's document listener, so Escape closes only the picker.
function onEscape(e: KeyboardEvent) {
  if (e.key !== "Escape") return;
  e.preventDefault();
  e.stopPropagation();
  hide();
}

function onOutsidePointer(e: PointerEvent) {
  if (!root.value?.contains(e.target as Node)) open.value = false;
}

function unlisten() {
  window.removeEventListener("keydown", onEscape, true);
  document.removeEventListener("pointerdown", onOutsidePointer, true);
}

watch(open, (isOpen) => {
  if (!isOpen) return unlisten();
  window.addEventListener("keydown", onEscape, true);
  document.addEventListener("pointerdown", onOutsidePointer, true);
});

onUnmounted(unlisten);
</script>

<template>
  <div ref="root" class="min-w-0">
    <button
      :id="id"
      ref="trigger"
      type="button"
      class="fx-input flex items-center justify-between gap-2 text-left"
      :aria-expanded="open ? 'true' : 'false'"
      :aria-describedby="valueId"
      @click="open ? hide() : show()"
    >
      <span :id="valueId" class="min-w-0 break-words" :class="selectedPath ? '' : 'text-zinc-400'">
        {{ selectedPath || $t("locationPicker.placeholder") }}
      </span>
      <FxSvg name="chevronDown" class="h-4 w-4 shrink-0 text-zinc-400 transition-transform" :class="open ? 'rotate-180' : ''" />
    </button>

    <!-- In the page flow, not floating, so the modal's scroll area cannot clip it. -->
    <div v-if="open" class="mt-1 overflow-hidden rounded-xl border border-zinc-200/90 bg-white shadow-lg shadow-zinc-900/10">
      <div class="border-b border-zinc-100 p-2">
        <input
          ref="searchInput"
          v-model="query"
          type="search"
          class="fx-input mt-0"
          :placeholder="$t('locationPicker.search')"
          :aria-label="$t('locationPicker.search')"
          @keydown.enter.prevent="pickFirstMatch"
        />
      </div>
      <ul ref="list" class="max-h-64 overflow-y-auto overscroll-contain py-1">
        <li v-if="noneLabel && !searching" class="px-1">
          <button
            type="button"
            class="flex min-h-9 w-full items-center justify-between gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-zinc-800 hover:bg-zinc-100"
            :class="modelValue ? '' : 'font-semibold'"
            :aria-current="modelValue ? undefined : 'true'"
            @click="pick('')"
          >
            <span>{{ noneLabel }}</span>
            <FxSvg v-if="!modelValue" name="check" class="h-4 w-4 shrink-0 text-sky-600" />
          </button>
        </li>
        <li v-for="r in rows" :key="r.id" class="flex items-center gap-1 px-1" :style="{ paddingLeft: searching ? undefined : `${r.depth * 1.25 + 0.25}rem` }">
          <button
            v-if="!searching && r.hasKids"
            type="button"
            class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-zinc-500 hover:bg-zinc-100 hover:text-zinc-800"
            :aria-expanded="expanded.has(r.id) ? 'true' : 'false'"
            :aria-label="$t('locationPicker.subLocations', { name: r.name })"
            @click="toggleExpanded(r.id)"
          >
            <FxSvg name="chevronRight" class="h-4 w-4 transition-transform motion-reduce:transition-none" :class="expanded.has(r.id) ? 'rotate-90' : ''" />
          </button>
          <span v-else-if="!searching" class="w-9 shrink-0" aria-hidden="true" />
          <button
            type="button"
            class="flex min-h-9 min-w-0 flex-1 items-center justify-between gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-zinc-800 hover:bg-zinc-100 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
            :class="r.id === modelValue ? 'font-semibold' : ''"
            :aria-current="r.id === modelValue ? 'true' : undefined"
            :disabled="isDisabled(r.id)"
            @click="pick(r.id)"
          >
            <span class="min-w-0 break-words">{{ searching ? r.path : r.name }}</span>
            <FxSvg v-if="r.id === modelValue" name="check" class="h-4 w-4 shrink-0 text-sky-600" />
          </button>
        </li>
        <li v-if="loadFailed" class="px-3 py-2 text-sm text-red-700">{{ $t("common.loadFailed") }}</li>
        <li v-else-if="searching && !rows.length" class="px-3 py-2 text-sm text-zinc-500">{{ $t("locationPicker.noMatch") }}</li>
      </ul>
    </div>
  </div>
</template>
