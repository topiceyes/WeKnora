/** Fields shared by workspace members and shared-space candidates. */
export interface TransferCandidateLike {
  user_id: string
  username?: string
  email?: string
  status: string
}

/**
 * 知识库归属转让的候选人过滤规则：
 *  - 排除当前归宿人（self-transfer 无意义）
 *  - 只保留 active 成员（invited / suspended 不能接手）
 * 不在这里按角色过滤：同空间 Viewer 目标由后端自动提升为 Contributor。
 */
export function filterTransferCandidates<T extends TransferCandidateLike>(
  members: T[],
  currentOwnerId?: string,
): T[] {
  return members.filter(
    (m) => m.status === 'active' && m.user_id !== currentOwnerId,
  );
}

/** 展示名：username 优先，退化到 email。 */
export function transferCandidateLabel(member: TransferCandidateLike): string {
  return member.username?.trim() || member.email || member.user_id;
}

/**
 * 预检是否有硬阻断项。阻断项非空时后端拒绝迁移，前端也必须禁用提交。
 * 计划缺失（尚未加载 / 加载失败）同样视为不可提交。
 */
export function transferHasBlockers(
  plan: { blockers?: ArrayLike<unknown> | null } | null | undefined,
): boolean {
  if (!plan) return true;
  return (plan.blockers?.length ?? 0) > 0;
}

/**
 * 把字节数压缩成便于阅读的「数值 + 单位」。单位保持 ASCII（B/KB/MB/GB），
 * 六种语言下都无需翻译；数值最多保留一位小数。
 */
export function formatTransferFileSize(
  bytes?: number | null,
): { value: string; unit: 'B' | 'KB' | 'MB' | 'GB' } {
  const n = Number(bytes ?? 0);
  if (!Number.isFinite(n) || n <= 0) return { value: '0', unit: 'B' };
  const units: Array<'B' | 'KB' | 'MB' | 'GB'> = ['B', 'KB', 'MB', 'GB'];
  let value = n;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  const rounded = index === 0 ? String(Math.round(value)) : value.toFixed(1).replace(/\.0$/, '');
  return { value: rounded, unit: units[index] };
}
