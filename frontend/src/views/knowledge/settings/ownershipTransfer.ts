import type { TenantMember } from '@/api/tenant/members';

/**
 * 知识库归属转让的候选人过滤规则：
 *  - 排除当前归宿人（self-transfer 无意义）
 *  - 只保留 active 成员（invited / suspended 不能接手）
 * 不在这里按角色过滤：Viewer 目标由后端自动提升为 Contributor。
 */
export function filterTransferCandidates(
  members: TenantMember[],
  currentOwnerId?: string,
): TenantMember[] {
  return members.filter(
    (m) => m.status === 'active' && m.user_id !== currentOwnerId,
  );
}

/** 展示名：username 优先，退化到 email。 */
export function transferCandidateLabel(member: TenantMember): string {
  return member.username?.trim() || member.email || member.user_id;
}
