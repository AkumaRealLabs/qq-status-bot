<script setup lang="ts">
import type { ModelCardPayload, StatusRow, StatusSummary, SummaryKey, UpstreamRecord } from '#/api';

import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';

import { Page } from '@vben/common-ui';

import {
  Button,
  Card,
  Form,
  Modal,
  Popconfirm,
  Radio,
  Select,
  Space,
  Statistic,
  Switch,
  Tag,
  Tooltip,
  message,
} from 'ant-design-vue';

import {
  checkModelCard,
  createModelCard,
  deleteModelCard,
  getStatus,
  getUpstreamKeys,
  listUpstreams,
  syncKeys,
  updateModelCard,
} from '#/api';

const windows = ['1h', '3h', '5h', '1d', '7d', '15d'];
const fixedModel = 'gpt-5.5';
const activeWindow = ref('1h');
const acting = ref('');
const loading = ref(false);
const saving = ref(false);
const open = ref(false);
const editingID = ref('');
const upstreams = ref<UpstreamRecord[]>([]);
const keyOptions = ref<SummaryKey[]>([]);
const form = reactive<ModelCardPayload>({
  enabled: true,
  key: '',
  model: fixedModel,
  upstream: '',
});
const data = ref<StatusSummary>({
  avg_latency: 0,
  failed: 0,
  requests: 0,
  rows: [],
  success: 0,
  success_rate: 0,
  window: '1h',
});
let refreshTimer: ReturnType<typeof setInterval> | undefined;

const rows = computed(() => data.value.rows);
const healthy = computed(() => rows.value.filter((row) => row.last_success).length);
const dialogTitle = computed(() => (editingID.value ? '编辑模型卡片' : '新增模型卡片'));

function text(value: unknown) {
  return value === undefined || value === null || value === '' ? '-' : value;
}

function percent(value: number) {
  return `${value.toFixed(2)}%`;
}

function formatTime(value?: string) {
  if (!value) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  const pad = (num: number) => String(num).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

function historyTooltip(item: NonNullable<StatusRow['history']>[number]) {
  return `${item.success ? '成功' : '失败'} · 对话延迟 ${item.latency_ms || '-'}ms · 时间 ${formatTime(
    item.checked_at,
  )}`;
}

function formatRatio(value?: string) {
  if (!value) {
    return '';
  }
  const ratio = value.trim();
  return ratio.endsWith('x') ? ratio : `${ratio}x`;
}

function keyMeta(key: Pick<SummaryKey, 'description' | 'group' | 'group_ratio'>) {
  const parts = [];
  if (key.description) {
    parts.push(key.description);
  }
  if (key.group && key.group !== key.description) {
    parts.push(key.group);
  }
  if (key.group_ratio) {
    parts.push(`${formatRatio(key.group_ratio)} 倍率`);
  }
  return parts.join(' · ');
}

function recordKeyMeta(record: StatusRow) {
  return keyMeta({
    description: record.key_description,
    group: record.key_group,
    group_ratio: record.key_group_ratio,
  });
}

function resetForm(record?: StatusRow) {
  editingID.value = record?.id || '';
  Object.assign(form, {
    enabled: record?.enabled ?? true,
    key: record?.key || '',
    model: fixedModel,
    upstream: record?.upstream || upstreams.value[0]?.id || '',
  });
}

async function load() {
  loading.value = true;
  try {
    const [status, upstreamList] = await Promise.all([
      getStatus(activeWindow.value),
      listUpstreams(),
    ]);
    data.value = status;
    upstreams.value = upstreamList.items;
  } finally {
    loading.value = false;
  }
}

async function loadKeys(upstreamID: string) {
  if (!upstreamID) {
    keyOptions.value = [];
    return;
  }
  keyOptions.value = await getUpstreamKeys(upstreamID);
}

async function refreshData() {
  await load();
  if (open.value && form.upstream) {
    await loadKeys(form.upstream);
  }
}

async function changeWindow(value: unknown) {
  activeWindow.value = typeof value === 'string' ? value : '1h';
  await load();
}

async function addCard() {
  resetForm();
  await loadKeys(form.upstream);
  open.value = true;
}

async function editCard(record: StatusRow) {
  resetForm(record);
  await loadKeys(form.upstream);
  open.value = true;
}

async function saveCard() {
  if (!form.upstream) {
    message.error('请选择上游');
    return;
  }
  form.model = fixedModel;
  saving.value = true;
  try {
    if (editingID.value) {
      await updateModelCard(editingID.value, form);
    } else {
      await createModelCard(form);
    }
    message.success('卡片已保存');
    open.value = false;
    await load();
  } finally {
    saving.value = false;
  }
}

async function removeCard(id: string) {
  await deleteModelCard(id);
  message.success('卡片已删除');
  await load();
}

async function runCard(id: string) {
  acting.value = `check:${id}`;
  try {
    await checkModelCard(id);
    message.success('检查完成');
    await load();
  } finally {
    acting.value = '';
  }
}

async function syncCurrentKeys() {
  if (!form.upstream) {
    return;
  }
  acting.value = `sync:${form.upstream}`;
  try {
    await syncKeys(form.upstream);
    await loadKeys(form.upstream);
    message.success('Key 已同步');
  } finally {
    acting.value = '';
  }
}

watch(
  () => form.upstream,
  async (value, oldValue) => {
    if (value && value !== oldValue) {
      form.key = '';
      await loadKeys(value);
    }
  },
);

onMounted(() => {
  void load();
  refreshTimer = setInterval(() => {
    void refreshData();
  }, 60_000);
});

onBeforeUnmount(() => {
  if (refreshTimer) {
    clearInterval(refreshTimer);
  }
});
</script>

<template>
  <Page title="状态监控">
    <div class="p-4">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
        <Radio.Group
          :value="activeWindow"
          button-style="solid"
          @change="(event) => changeWindow(event.target.value)"
        >
          <Radio.Button v-for="item in windows" :key="item" :value="item">
            {{ item }}
          </Radio.Button>
        </Radio.Group>
        <Space wrap>
          <Tag color="success">{{ healthy }} / {{ rows.length }} 正常</Tag>
          <Button :loading="loading" @click="load">刷新</Button>
          <Button type="primary" @click="addCard">新增卡片</Button>
        </Space>
      </div>

      <div class="mb-4 grid gap-4 md:grid-cols-4">
        <Card>
          <Statistic title="请求数" :value="data.requests" />
        </Card>
        <Card>
          <Statistic title="成功" :value="data.success" />
        </Card>
        <Card>
          <Statistic title="失败" :value="data.failed" />
        </Card>
        <Card>
          <Statistic title="成功率" :precision="1" suffix="%" :value="data.success_rate" />
        </Card>
      </div>

      <div class="grid gap-4 xl:grid-cols-3 md:grid-cols-2">
        <Card v-for="record in rows" :key="record.id" class="monitor-card">
          <template #title>
            <div class="flex min-w-0 items-center gap-3">
              <div class="min-w-0 flex-1">
                <div class="truncate text-base font-semibold">{{ record.name }}</div>
                <div class="truncate text-xs text-gray-500">
                  {{ record.upstream_name }} · {{ record.model }}
                </div>
              </div>
              <Tag v-if="record.last_success" color="success">正常</Tag>
              <Tag v-else-if="record.last_probe_error || record.last_error" color="error">
                异常
              </Tag>
              <Tag v-else>无数据</Tag>
            </div>
          </template>
          <template #extra>
            <Space>
              <Button
                :loading="acting === `check:${record.id}`"
                size="small"
                @click="runCard(record.id)"
              >
                检查
              </Button>
              <Button size="small" @click="editCard(record)">编辑</Button>
              <Popconfirm title="删除这个监控卡片？" @confirm="removeCard(record.id)">
                <Button danger size="small">删除</Button>
              </Popconfirm>
            </Space>
          </template>

          <div class="grid grid-cols-2 gap-2">
            <div class="rounded border border-gray-200 p-3">
              <div class="text-xs text-gray-500">对话延迟</div>
              <div class="mt-2 text-xl font-semibold">
                {{ text(record.last_latency) }}<span class="ml-1 text-xs">ms</span>
              </div>
            </div>
            <div class="rounded border border-gray-200 p-3">
              <div class="text-xs text-gray-500">端点 PING</div>
              <div class="mt-2 text-xl font-semibold">
                {{ text(record.last_http_status) }}
              </div>
            </div>
          </div>

          <div class="mt-4 border-t border-gray-100 pt-4">
            <div class="flex items-end justify-between">
              <div class="text-xs text-gray-500">可用性 · {{ activeWindow }}</div>
              <div
                :class="record.success_rate >= 90 ? 'text-green-600' : 'text-red-600'"
                class="text-3xl font-semibold"
              >
                {{ percent(record.success_rate) }}
              </div>
            </div>
            <div class="mt-3 flex h-7 items-end gap-1">
              <Tooltip
                v-for="(item, index) in record.history || []"
                :key="`${record.id}:${index}`"
                :title="historyTooltip(item)"
              >
                <span
                  :class="item.success ? 'bg-green-500' : 'bg-red-500'"
                  class="inline-block w-1 flex-1 rounded-sm"
                  :style="{ height: `${item.success ? 20 : 8}px` }"
                />
              </Tooltip>
            </div>
            <div class="mt-2 flex justify-between text-xs text-gray-400">
              <span>PAST</span>
              <span>{{ record.requests }} 次记录</span>
              <span>NOW</span>
            </div>
          </div>

          <div class="mt-3 text-xs text-gray-500">
            Key: {{ text(record.key_name || record.key) }}
          </div>
          <div v-if="recordKeyMeta(record)" class="mt-1 break-all text-xs text-gray-500">
            {{ recordKeyMeta(record) }}
          </div>
          <div v-if="record.last_probe_error || record.last_error" class="mt-2 break-all text-xs text-red-600">
            {{ record.last_probe_error || record.last_error }}
          </div>
        </Card>
      </div>

      <Modal
        v-model:open="open"
        :confirm-loading="saving"
        :title="dialogTitle"
        width="680px"
        @ok="saveCard"
      >
        <Form :label-col="{ span: 5 }" :model="form" :wrapper-col="{ span: 17 }">
          <Form.Item label="上游" required>
            <Select v-model:value="form.upstream" placeholder="选择上游">
              <Select.Option v-for="item in upstreams" :key="item.id" :value="item.id">
                {{ item.name }} ({{ item.type }})
              </Select.Option>
            </Select>
          </Form.Item>
          <Form.Item label="Key">
            <Space.Compact class="w-full">
              <Select v-model:value="form.key" allow-clear class="w-full" placeholder="选择 Key">
                <Select.Option v-for="item in keyOptions" :key="item.id" :value="item.id">
                  <div class="flex flex-col leading-tight">
                    <span>
                      {{ item.name || item.id }} {{ item.status ? `(${item.status})` : '' }}
                    </span>
                    <span v-if="keyMeta(item)" class="text-xs text-gray-500">
                      {{ keyMeta(item) }}
                    </span>
                  </div>
                </Select.Option>
              </Select>
              <Button :loading="acting === `sync:${form.upstream}`" @click="syncCurrentKeys">
                同步
              </Button>
            </Space.Compact>
          </Form.Item>
          <Form.Item label="启用">
            <Switch v-model:checked="form.enabled" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  </Page>
</template>
