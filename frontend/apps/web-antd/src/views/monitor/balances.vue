<script setup lang="ts">
import type { BalanceRow } from '#/api';

import { computed, onMounted, ref } from 'vue';

import { Page } from '@vben/common-ui';

import { Button, Card, Statistic, Tag } from 'ant-design-vue';

import { checkUpstream, getBalances, listUpstreams } from '#/api';

const rows = ref<BalanceRow[]>([]);
const loading = ref(false);

const totals = computed(() => ({
  low: rows.value.filter((item) => item.low_balance).length,
  remain: rows.value.reduce((sum, item) => sum + Number(item.remain || 0), 0),
  sourceRemain: rows.value.reduce(
    (sum, item) => sum + Number(item.source_remain || 0),
    0,
  ),
  upstreams: rows.value.length,
  used: rows.value.reduce((sum, item) => sum + Number(item.used || 0), 0),
}));

function text(value: unknown) {
  return value === undefined || value === null || value === '' ? '-' : value;
}

function money(value: unknown) {
  return typeof value === 'number' ? value.toFixed(2) : text(value);
}

async function load(refreshUpstreams = false) {
  loading.value = true;
  try {
    if (refreshUpstreams) {
      const upstreams = await listUpstreams();
      await Promise.allSettled(
        upstreams.items
          .filter((item) => item.enabled)
          .map((item) => checkUpstream(item.id)),
      );
    }
    rows.value = await getBalances();
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>

<template>
  <Page title="余额监控">
    <div class="p-4">
      <div class="mb-4 grid gap-4 md:grid-cols-4">
        <Card>
          <Statistic title="上游数量" :value="totals.upstreams" />
        </Card>
        <Card>
          <Statistic title="总余额(RMB)" :precision="2" :value="totals.remain" />
        </Card>
        <Card>
          <Statistic title="站内余额(USD)" :precision="2" :value="totals.sourceRemain" />
        </Card>
        <Card>
          <Statistic title="低余额" :value="totals.low" />
        </Card>
      </div>

      <div class="mb-3 flex items-center justify-between">
        <div class="text-base font-semibold">上游余额</div>
        <Button :loading="loading" type="primary" @click="load(true)">刷新</Button>
      </div>

      <div class="grid gap-4 xl:grid-cols-3 md:grid-cols-2">
        <Card v-for="record in rows" :key="record.id" :loading="loading">
          <template #title>
            <div class="flex min-w-0 items-center gap-3">
              <div class="min-w-0 flex-1">
                <div class="truncate text-base font-semibold">{{ record.name }}</div>
                <div class="truncate text-xs text-gray-500">
                  {{ record.type }}
                  <Tag v-if="!record.enabled" class="ml-1">已停用</Tag>
                </div>
              </div>
              <Tag v-if="record.low_balance" color="error">低余额</Tag>
              <Tag v-else color="success">正常</Tag>
            </div>
          </template>

          <div class="grid grid-cols-2 gap-2">
            <div class="rounded border border-gray-200 p-3">
              <div class="text-xs text-gray-500">实际余额(RMB)</div>
              <div
                :class="record.low_balance ? 'text-red-600' : 'text-foreground'"
                class="mt-2 text-xl font-semibold"
              >
                {{ money(record.remain) }}
              </div>
            </div>
            <div class="rounded border border-gray-200 p-3">
              <div class="text-xs text-gray-500">站内余额(USD)</div>
              <div class="mt-2 text-xl font-semibold">
                {{ money(record.source_remain) }}
              </div>
            </div>
            <div class="rounded border border-gray-200 p-3">
              <div class="text-xs text-gray-500">余额倍率</div>
              <div class="mt-2 text-xl font-semibold">
                {{ text(record.balance_rate) }}
              </div>
              <div class="mt-1 text-xs text-gray-500">1 USD = N RMB</div>
            </div>
            <div class="rounded border border-gray-200 p-3">
              <div class="text-xs text-gray-500">低余额阈值(RMB)</div>
              <div class="mt-2 text-xl font-semibold">
                {{ money(record.low_balance_threshold) }}
              </div>
            </div>
            <div class="rounded border border-gray-200 p-3">
              <div class="text-xs text-gray-500">已用(RMB)</div>
              <div class="mt-2 text-xl font-semibold">{{ money(record.used) }}</div>
            </div>
            <div class="rounded border border-gray-200 p-3">
              <div class="text-xs text-gray-500">最后检查</div>
              <div class="mt-2 truncate text-sm font-medium">
                {{ text(record.last_check) }}
              </div>
            </div>
          </div>

          <div v-if="record.error" class="mt-3 break-all text-xs text-red-600">
            {{ record.error }}
          </div>
        </Card>
      </div>

      <div
        v-if="!loading && rows.length === 0"
        class="rounded border border-dashed border-gray-200 py-10 text-center text-gray-500"
      >
        暂无余额数据
      </div>
    </div>
  </Page>
</template>
