<template>
  <el-drawer
    :model-value="modelValue"
    title="市民报修详情"
    size="560px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="load"
  >
    <div v-loading="loading" class="detail">
      <template v-if="detail">
        <div class="detail-head">
          <span class="report-no">{{ detail.report_no }}</span>
          <StatusTag :dict="REPORT_STATUS" :value="detail.status" />
          <el-tag
            v-if="detail.duplicate_count"
            type="warning"
            size="small"
            effect="plain"
          >合并 {{ detail.duplicate_count }} 条重复</el-tag>
        </div>

        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="故障类型">{{ detail.fault_type }}</el-descriptions-item>
          <el-descriptions-item label="灯杆编号">{{ detail.lamp_code || '未提供编号' }}</el-descriptions-item>
          <el-descriptions-item label="所在道路">{{ detail.road_name || '-' }}</el-descriptions-item>
          <el-descriptions-item label="位置描述">{{ detail.location_desc || '-' }}</el-descriptions-item>
          <el-descriptions-item label="报修人">
            {{ detail.reporter || '-' }}<span v-if="detail.reporter_phone"> · {{ detail.reporter_phone }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="上报时间">{{ formatDateTime(detail.reported_at) }}</el-descriptions-item>
          <el-descriptions-item label="市民原始描述">
            <span class="origin-content">{{ detail.content }}</span>
          </el-descriptions-item>
        </el-descriptions>

        <el-divider content-position="left">核实处置</el-divider>

        <template v-if="detail.status === 'pending'">
          <el-empty description="尚未核实, 请核实后转为正式故障或判为无效" :image-size="70" />
          <div class="pending-actions">
            <el-button type="success" :icon="Check" @click="$emit('verify', detail)">核实有效</el-button>
            <el-button type="danger" plain :icon="CircleClose" @click="$emit('invalid', detail)">核实无效</el-button>
          </div>
        </template>

        <el-descriptions v-else :column="1" border size="small">
          <el-descriptions-item v-if="detail.fault_no" label="关联故障">
            <el-link type="primary" :underline="false" @click="goFault(detail.fault_no)">
              {{ detail.fault_no }}
            </el-link>
            <span class="text-muted"> · 来源: 市民上报</span>
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.merged_into_no" label="并入主报修单">
            <el-link type="primary" :underline="false" @click="openMerged(detail.merged_into_id)">
              {{ detail.merged_into_no }}
            </el-link>
          </el-descriptions-item>          <el-descriptions-item v-if="detail.invalid_reason" label="无效原因">
            <span class="invalid-reason">{{ detail.invalid_reason }}</span>
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.verify_remark" label="核实备注">
            {{ detail.verify_remark }}
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.verified_by" label="核实人">
            {{ detail.verified_by }}
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.verified_at" label="核实时间">
            {{ formatDateTime(detail.verified_at) }}
          </el-descriptions-item>
        </el-descriptions>

        <template v-if="detail.duplicates?.length">
          <el-divider content-position="left">被合并的重复上报 ({{ detail.duplicates.length }})</el-divider>
          <el-timeline>
            <el-timeline-item
              v-for="dup in detail.duplicates"
              :key="dup.id"
              :timestamp="`${formatDateTime(dup.reported_at)} · ${dup.reporter || '匿名市民'}`"
              placement="top"
              type="warning"
            >
              <div class="dup-content">{{ dup.content }}</div>
              <div v-if="dup.reporter_phone" class="text-muted">联系电话: {{ dup.reporter_phone }}</div>
              <el-link
                v-if="dup.fault_no"
                type="primary"
                :underline="false"
                class="dup-fault"
                @click="goFault(dup.fault_no)"
              >关联故障 {{ dup.fault_no }}</el-link>
            </el-timeline-item>
          </el-timeline>
        </template>
      </template>
    </div>
  </el-drawer>
</template>

<script setup>
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Check, CircleClose } from '@element-plus/icons-vue'
import StatusTag from '@/components/common/StatusTag.vue'
import { reportApi } from '@/api/report'
import { REPORT_STATUS } from '@/constants/dict'
import { formatDateTime } from '@/utils/format'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  reportId: { type: [Number, String], default: null },
})

const emit = defineEmits(['update:modelValue', 'verify', 'invalid', 'navigate'])

const router = useRouter()
const loading = ref(false)
const detail = ref(null)

async function load() {
  if (!props.reportId) return
  loading.value = true
  detail.value = null
  try {
    detail.value = await reportApi.detail(props.reportId)
  } catch (error) {
    detail.value = null
  } finally {
    loading.value = false
  }
}

// 抽屉已打开时从被合并单切换到主单, 同样需要重新加载。
watch(
  () => props.reportId,
  () => {
    if (props.modelValue) load()
  },
)

function goFault(faultNo) {
  router.push({ path: '/status/track', query: { fault_no: faultNo } })
}

// 在抽屉内直接切换到主报修单详情。
function openMerged(id) {
  emit('navigate', id)
}
</script>

<style scoped>
.detail-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
}

.report-no {
  font-size: 16px;
  font-weight: 600;
}

.origin-content {
  white-space: pre-wrap;
  word-break: break-all;
}

.invalid-reason {
  color: #909399;
}

.pending-actions {
  display: flex;
  justify-content: center;
  gap: 12px;
}

.dup-content {
  margin-bottom: 4px;
}

.dup-fault {
  font-size: 12px;
}
</style>
