<template>
  <div class="page">
    <PageHeader title="市民报修" description="受理市民依据灯杆编号或位置描述提交的报修, 核实后转正式故障或注明无效原因, 重复上报自动合并">
      <el-button :icon="Refresh" @click="loadAll">刷新</el-button>
      <el-button type="primary" :icon="Plus" @click="submitVisible = true">市民报修登记</el-button>
    </PageHeader>

    <div class="summary-grid">
      <StatCard label="待核实" :value="summary.pending_total" suffix="单" icon="Bell" color="#f56c6c" :hint="`今日新增 ${summary.today_new} 单`" />
      <StatCard label="累计受理" :value="summary.total" suffix="单" icon="Document" color="#409eff" hint="含有效 / 无效 / 合并" />
      <StatCard label="核实有效" :value="summary.confirmed_total" suffix="单" icon="CircleCheck" color="#67c23a" hint="已转正式故障" />
      <StatCard label="核实无效" :value="summary.invalid_total" suffix="单" icon="CircleClose" color="#909399" hint="已注明原因" />
      <StatCard label="重复合并" :value="summary.merged_total" suffix="单" icon="Connection" color="#e6a23c" hint="自动并入主报修单" />
    </div>

    <el-card shadow="never">
      <el-tabs v-model="activeStatus" class="status-tabs" @tab-change="handleTabChange">
        <el-tab-pane v-for="tab in statusTabs" :key="tab.value" :name="tab.value">
          <template #label>
            <span>{{ tab.label }}<span v-if="tab.count !== null" class="tab-count">{{ tab.count }}</span></span>
          </template>
        </el-tab-pane>
      </el-tabs>

      <div class="filter-bar">
        <el-input v-model="query.keyword" placeholder="报修单号 / 灯杆编号 / 道路 / 位置 / 描述 / 报修人" clearable @keyup.enter="handleSearch" />
        <el-select v-model="query.fault_type" placeholder="故障类型" clearable @change="handleSearch">
          <el-option v-for="item in faultTypeOptions" :key="item" :label="item" :value="item" />
        </el-select>
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          value-format="YYYY-MM-DD"
          range-separator="至"
          start-placeholder="上报开始日期"
          end-placeholder="上报结束日期"
          @change="handleSearch"
        />
        <el-button type="primary" :icon="Search" @click="handleSearch">查询</el-button>
        <el-button :icon="RefreshLeft" @click="handleReset">重置</el-button>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="report_no" label="报修单号" width="150" fixed="left" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }"><StatusTag :dict="REPORT_STATUS" :value="row.status" /></template>
        </el-table-column>
        <el-table-column label="路灯定位" min-width="180">
          <template #default="{ row }">
            <div class="loc-cell">
              <span v-if="row.lamp_code" class="loc-code">{{ row.lamp_code }}</span>
              <el-tag v-else type="info" size="small" effect="plain">无编号</el-tag>
              <span class="text-muted">{{ row.road_name || '道路待核实' }}</span>
            </div>
            <div v-if="row.location_desc" class="loc-desc text-muted">{{ row.location_desc }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="fault_type" label="故障类型" width="100" />
        <el-table-column label="市民原始描述" min-width="220" show-overflow-tooltip>
          <template #default="{ row }">
            <span :title="row.content">{{ row.content }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="reporter" label="报修人" width="100">
          <template #default="{ row }">{{ row.reporter || '匿名' }}</template>
        </el-table-column>
        <el-table-column label="上报时间" width="150">
          <template #default="{ row }">{{ formatDateTime(row.reported_at) }}</template>
        </el-table-column>
        <el-table-column label="重复" width="70" align="center">
          <template #default="{ row }">
            <el-badge v-if="row.duplicate_count" :value="row.duplicate_count" type="warning" />
            <span v-else class="text-muted">0</span>
          </template>
        </el-table-column>
        <el-table-column label="核实结果" width="160">
          <template #default="{ row }">
            <el-link
              v-if="row.fault_no"
              type="success"
              :underline="false"
              @click="goTrack(row.fault_no)"
            >{{ row.fault_no }}</el-link>
            <el-link v-else-if="row.merged_into_no" type="warning" :underline="false" @click="openDetail(row.merged_into_id)">
              并入 {{ row.merged_into_no }}
            </el-link>
            <span v-else-if="row.invalid_reason" class="invalid-text" :title="row.invalid_reason">无效</span>
            <span v-else class="text-muted">待核实</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row.id)">详情</el-button>
            <template v-if="row.status === 'pending'">
              <el-button link type="success" @click="openVerify(row)">核实有效</el-button>
              <el-button link type="danger" @click="openInvalid(row)">无效</el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>
      <DataPagination
        :page="query.page"
        :page-size="query.page_size"
        :total="total"
        @page-change="changePage"
        @size-change="changePageSize"
      />
    </el-card>

    <ReportSubmitDialog
      v-model="submitVisible"
      :fault-type-options="faultTypeOptions"
      @saved="handleChanged"
    />
    <ReportVerifyDialog
      v-model="verifyVisible"
      :report="verifyTarget"
      :fault-type-options="faultTypeOptions"
      @verified="handleVerified"
    />
    <ReportInvalidDialog
      v-model="invalidVisible"
      :report="verifyTarget"
      @verified="handleVerified"
    />
    <ReportDetailDrawer
      v-model="detailVisible"
      :report-id="activeReportId"
      @verify="handleDrawerVerify"
      @invalid="handleDrawerInvalid"
      @navigate="openDetail"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Plus, Refresh, RefreshLeft, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/common/PageHeader.vue'
import StatusTag from '@/components/common/StatusTag.vue'
import StatCard from '@/components/common/StatCard.vue'
import DataPagination from '@/components/common/DataPagination.vue'
import ReportSubmitDialog from './components/ReportSubmitDialog.vue'
import ReportVerifyDialog from './components/ReportVerifyDialog.vue'
import ReportInvalidDialog from './components/ReportInvalidDialog.vue'
import ReportDetailDrawer from './components/ReportDetailDrawer.vue'
import { reportApi } from '@/api/report'
import { useDictStore } from '@/stores/dict'
import { REPORT_STATUS } from '@/constants/dict'
import { formatDateTime } from '@/utils/format'
import { useListPage } from '@/composables/useListPage'

const route = useRoute()
const router = useRouter()
const dictStore = useDictStore()

const activeStatus = ref('pending')

const { loading, rows, total, query, load, search, reset, changePage, changePageSize } = useListPage(reportApi.list, {
  status: 'pending',
  keyword: '',
  fault_type: '',
  start_date: '',
  end_date: '',
})

const emptySummary = () => ({
  total: 0,
  pending_total: 0,
  confirmed_total: 0,
  invalid_total: 0,
  merged_total: 0,
  today_new: 0,
})

const summary = ref(emptySummary())
const faultTypeOptions = computed(() => dictStore.faultMeta.fault_types)

const dateRange = ref([])
const submitVisible = ref(false)
const verifyVisible = ref(false)
const invalidVisible = ref(false)
const detailVisible = ref(false)
const verifyTarget = ref(null)
const activeReportId = ref(null)

const statusTabs = computed(() => [
  { label: '待核实', value: 'pending', count: summary.value.pending_total },
  { label: '核实有效', value: 'confirmed', count: summary.value.confirmed_total },
  { label: '核实无效', value: 'invalid', count: summary.value.invalid_total },
  { label: '重复合并', value: 'merged', count: summary.value.merged_total },
  { label: '全部', value: '', count: null },
])

async function loadSummary() {
  try {
    summary.value = await reportApi.summary()
  } catch (error) {
    summary.value = emptySummary()
  }
}

function loadAll() {
  load()
  loadSummary()
}

function handleTabChange(name) {
  query.status = name
  search()
}

function applyDateRange() {
  query.start_date = dateRange.value?.[0] ?? ''
  query.end_date = dateRange.value?.[1] ?? ''
}

function handleSearch() {
  applyDateRange()
  search()
}

function handleReset() {
  dateRange.value = []
  activeStatus.value = 'pending'
  reset()
}

function openDetail(id) {
  activeReportId.value = id
  detailVisible.value = true
}

function openVerify(row) {
  verifyTarget.value = { ...row }
  verifyVisible.value = true
}

function openInvalid(row) {
  verifyTarget.value = { ...row }
  invalidVisible.value = true
}

function handleDrawerVerify(detail) {
  detailVisible.value = false
  verifyTarget.value = { ...detail }
  verifyVisible.value = true
}

function handleDrawerInvalid(detail) {
  detailVisible.value = false
  verifyTarget.value = { ...detail }
  invalidVisible.value = true
}

function handleVerified() {
  ElMessage.success('核实完成')
  handleChanged()
}

function handleChanged() {
  loadAll()
  dictStore.refreshAll().catch(() => {})
}

function goTrack(faultNo) {
  router.push({ path: '/status/track', query: { fault_no } })
}

// 支持从其它页面携带 focus 参数直接打开某条报修详情。
async function applyRouteQuery() {
  const focusId = Number(route.query.focus)
  if (!focusId) return
  activeReportId.value = focusId
  detailVisible.value = true
  router.replace({ path: '/reports' })
}

onMounted(async () => {
  dictStore.ensureLoaded().catch(() => {})
  await loadSummary()
  applyRouteQuery()
})
</script>

<style scoped>
.summary-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
  margin: 16px 0;
}

.status-tabs :deep(.el-tabs__header) {
  margin-bottom: 12px;
}

.tab-count {
  margin-left: 6px;
  font-size: 12px;
  color: #909399;
}

.loc-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.loc-code {
  font-weight: 600;
}

.loc-desc {
  font-size: 12px;
  margin-top: 2px;
}

.invalid-text {
  color: #909399;
}

@media (max-width: 1280px) {
  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
