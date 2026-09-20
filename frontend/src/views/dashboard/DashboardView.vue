<template>
  <div v-loading="loading" class="page">
    <PageHeader title="运行看板" description="路灯台账、故障登记与维修进展的整体概览">
      <el-button :icon="Refresh" @click="load">刷新</el-button>
      <el-button type="primary" :icon="Plus" @click="$router.push('/faults')">去登记故障</el-button>
    </PageHeader>

    <div class="card-grid">
      <StatCard
        label="路灯总数"
        :value="overview.lamp.total"
        suffix="盏"
        icon="Postcard"
        color="#409eff"
        :hint="`覆盖 ${overview.lamp.road_count} 条道路`"
      />
      <StatCard
        label="未闭环故障"
        :value="overview.fault.open_total"
        suffix="条"
        icon="Warning"
        color="#e6a23c"
        :hint="`待处理 ${pendingCount} 条 / 维修中 ${processingCount} 条`"
      />
      <StatCard
        label="超期未处理"
        :value="overview.fault.overdue_total"
        suffix="条"
        icon="AlarmClock"
        color="#f56c6c"
        :hint="`超过 ${overview.overdue_threshold_hours} 小时仍未开工`"
      />
      <StatCard
        label="平均维修时长"
        :value="overview.repair.average_duration_hours"
        suffix="小时"
        icon="Timer"
        color="#67c23a"
        :hint="`今日完成维修 ${overview.repair.today_finished} 次`"
      />
      <StatCard
        label="维修费用合计"
        :value="overview.repair.total_cost"
        suffix="元"
        icon="Money"
        color="#909399"
        :hint="`累计维修记录 ${overview.repair.total} 条`"
      />
      <StatCard
        label="今日新增故障"
        :value="overview.fault.today_reported"
        suffix="条"
        icon="DataLine"
        color="#409eff"
        :hint="`故障累计 ${overview.fault.total} 条`"
      />
      <StatCard
        label="市民报修待核实"
        :value="overview.report.pending_total"
        suffix="单"
        icon="Bell"
        color="#e6a23c"
        :hint="`今日新增报修 ${overview.report.today_new} 单`"
      />
    </div>

    <el-row :gutter="16">
      <el-col :xs="24" :md="6">
        <el-card shadow="never">
          <div class="section-title">路灯运行状态</div>
          <BarList :items="runStatusItems" />
        </el-card>
      </el-col>
      <el-col :xs="24" :md="6">
        <el-card shadow="never">
          <div class="section-title">故障处理状态</div>
          <BarList :items="faultStatusItems" />
        </el-card>
      </el-col>
      <el-col :xs="24" :md="6">
        <el-card shadow="never">
          <div class="section-title">故障来源: 市民 vs 内部</div>
          <BarList :items="sourceGroupItems" />
          <div class="source-detail text-muted">
            市民上报 {{ overview.fault.citizen_total }} 条 · 内部发现 {{ overview.fault.internal_total }} 条
          </div>
        </el-card>
      </el-col>
      <el-col :xs="24" :md="6">
        <el-card shadow="never">
          <div class="section-title">故障类型分布</div>
          <BarList :items="overview.fault_by_type" />
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never" class="report-card">
      <div class="section-title">
        <span>市民报修队列</span>
        <el-button link type="primary" @click="$router.push('/reports')">前往核实</el-button>
      </div>
      <div class="report-channel">
        <div class="report-channel__item report-channel__item--citizen">
          <div class="report-channel__value">{{ overview.report.confirmed_total }}</div>
          <div class="report-channel__label">核实有效 · 已转故障</div>
        </div>
        <div class="report-channel__item report-channel__item--merged">
          <div class="report-channel__value">{{ overview.report.merged_total }}</div>
          <div class="report-channel__label">重复上报自动合并</div>
        </div>
        <div class="report-channel__item report-channel__item--invalid">
          <div class="report-channel__value">{{ overview.report.invalid_total }}</div>
          <div class="report-channel__label">核实无效 · 已注明原因</div>
        </div>
      </div>
    </el-card>

    <el-row :gutter="16">
      <el-col :xs="24" :md="12">
        <el-card shadow="never">
          <div class="section-title">
            <span>最近登记故障</span>
            <el-button link type="primary" @click="$router.push('/faults')">查看全部</el-button>
          </div>
          <el-table :data="overview.recent_faults" size="small" @row-click="goTrack">
            <el-table-column prop="fault_no" label="故障单号" width="140" />
            <el-table-column prop="lamp_code" label="路灯编号" width="110" />
            <el-table-column prop="road_name" label="道路" min-width="90" />
            <el-table-column prop="fault_type" label="类型" width="90" />
            <el-table-column label="来源" width="80">
              <template #default="{ row }">
                <el-tag :type="row.source === 'citizen' ? 'warning' : 'primary'" size="small" effect="plain">
                  {{ row.source === 'citizen' ? '市民' : '内部' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="90">
              <template #default="{ row }"><StatusTag :dict="FAULT_STATUS" :value="row.status" /></template>
            </el-table-column>
            <el-table-column label="已等待" width="90">
              <template #default="{ row }">{{ formatWaiting(row.waiting_hours) }}</template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
      <el-col :xs="24" :md="12">
        <el-card shadow="never">
          <div class="section-title">
            <span>超期未处理故障</span>
            <el-tag type="danger" effect="plain" size="small">超过 {{ overview.overdue_threshold_hours }} 小时</el-tag>
          </div>
          <el-table :data="overview.overdue_faults" size="small" @row-click="goTrack">
            <el-table-column prop="fault_no" label="故障单号" width="140" />
            <el-table-column prop="lamp_code" label="路灯编号" width="110" />
            <el-table-column prop="road_name" label="道路" min-width="100" />
            <el-table-column label="等级" width="90">
              <template #default="{ row }"><StatusTag :dict="FAULT_LEVEL" :value="row.fault_level" /></template>
            </el-table-column>
            <el-table-column label="已等待" width="90">
              <template #default="{ row }">{{ formatWaiting(row.waiting_hours) }}</template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <el-card shadow="never">
      <div class="section-title">故障高发道路 TOP5</div>
      <BarList :items="overview.top_roads" />
    </el-card>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Refresh } from '@element-plus/icons-vue'
import PageHeader from '@/components/common/PageHeader.vue'
import StatCard from '@/components/common/StatCard.vue'
import BarList from '@/components/common/BarList.vue'
import StatusTag from '@/components/common/StatusTag.vue'
import { statusApi } from '@/api/status'
import { FAULT_LEVEL, FAULT_STATUS, RUN_STATUS } from '@/constants/dict'
import { formatWaiting } from '@/utils/format'

const router = useRouter()
const loading = ref(false)

const emptyOverview = () => ({
  lamp: { total: 0, road_count: 0, by_run_status: {} },
  fault: {
    total: 0, open_total: 0, by_status: {}, by_source: {},
    citizen_total: 0, internal_total: 0, today_reported: 0, overdue_total: 0,
  },
  repair: { total: 0, ongoing_total: 0, finished_total: 0, today_finished: 0, average_duration_hours: 0, total_cost: 0 },
  report: { total: 0, pending_total: 0, confirmed_total: 0, invalid_total: 0, merged_total: 0, today_new: 0 },
  fault_by_type: [],
  fault_by_level: [],
  fault_by_source: [],
  top_roads: [],
  recent_faults: [],
  overdue_faults: [],
  overdue_threshold_hours: 24,
})

const overview = ref(emptyOverview())

const runStatusItems = computed(() =>
  Object.entries(RUN_STATUS).map(([key, item]) => ({
    label: item.label,
    count: overview.value.lamp.by_run_status?.[key] ?? 0,
  })),
)

const faultStatusItems = computed(() =>
  Object.entries(FAULT_STATUS).map(([key, item]) => ({
    label: item.label,
    count: overview.value.fault.by_status?.[key] ?? 0,
  })),
)

const pendingCount = computed(() => overview.value.fault.by_status?.pending ?? 0)
const processingCount = computed(() => overview.value.fault.by_status?.processing ?? 0)

const sourceGroupItems = computed(() => [
  { label: '市民上报', count: overview.value.fault.citizen_total },
  { label: '内部发现', count: overview.value.fault.internal_total },
])

async function load() {
  loading.value = true
  try {
    overview.value = await statusApi.overview()
  } catch (error) {
    overview.value = emptyOverview()
  } finally {
    loading.value = false
  }
}

function goTrack(row) {
  router.push({ path: '/status/track', query: { fault_no: row.fault_no } })
}

onMounted(load)
</script>

<style scoped>
.source-detail {
  margin-top: 10px;
  font-size: 12px;
}

.report-card {
  margin-top: 0;
}

.report-channel {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.report-channel__item {
  border-radius: 6px;
  padding: 16px;
  text-align: center;
  background-color: #f7f9fc;
}

.report-channel__item--citizen {
  background-color: #f0f9eb;
}

.report-channel__item--merged {
  background-color: #fdf6ec;
}

.report-channel__item--invalid {
  background-color: #f4f4f5;
}

.report-channel__value {
  font-size: 26px;
  font-weight: 600;
}

.report-channel__label {
  margin-top: 6px;
  font-size: 13px;
  color: #606266;
}
</style>
