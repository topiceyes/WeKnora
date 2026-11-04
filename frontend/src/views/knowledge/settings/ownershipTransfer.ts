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
