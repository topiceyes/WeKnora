/**
 * 分片拼接工具：把知识文档的 chunks 还原为完整文档文本。
 *
 * 从 doc-content.vue 提取，供文档详情抽屉（merged 视图）与浏览模式阅读器
 * 共用，避免两处实现漂移。
 */

/**
 * 把已合并文本 acc 和下一个 chunk 内容 next 拼接，并去除两者的重叠部分。
 *
 * 不再依赖 start_at / end_at 做位置裁剪，而是用「文本重叠匹配」：在 next 的
 * 开头窗口里找 acc 后缀首次出现的位置，从该位置之后接上。这样能同时兼容：
 *  1. chunker 给拆分表格补写的表头（零宽 start/end，位置上不可见）——表头出现
 *     在重叠行之前，会被自然跳过；
 *  2. HTML 实体编码（&#34; 等）导致的 content 长度与原文区间不一致——比对的是
 *     文本本身，不受长度偏差影响。
 *
 * positionOverlap <= 0 时两段位置上严格相邻或不相交，无可去重重叠；此时进入
 * 文本匹配会因 headSlack 下限 320 在 next 开头窗口内误命中 acc 后缀的真实
 * 内容重复（如同一句话在文档多次出现），把 next 开头整段误判为补写表头删掉，
 * 造成不可逆的内容丢失。直接拼接，补写表头重复交给调用方后处理。
 *
 * @param positionOverlap 由 start/end 估算的重叠量，仅用于界定搜索窗口大小。
 */
export const appendChunkContent = (acc: string, next: string, positionOverlap: number): string => {
  if (!acc) return next;
  if (!next) return acc;
  if (positionOverlap <= 0) return acc + next;

  const MIN_OVERLAP = 12;          // 过短的后缀容易误匹配（如分隔行），忽略
  const span = Math.max(positionOverlap, 0);
  // 搜索的后缀最大长度；按位置重叠量放大几倍兜底，并设下限
  const maxK = Math.min(acc.length, next.length, Math.max(span * 3, 400));
  // 重叠行之前最多允许多少前缀（补写的表头）被跳过
  const headSlack = Math.max(span * 2, 320);

  for (let k = maxK; k >= MIN_OVERLAP; k--) {
    const suffix = acc.slice(acc.length - k);
    const pos = next.indexOf(suffix);
    if (pos !== -1 && pos <= headSlack) {
      return acc + next.slice(pos + k);
    }
  }
  return acc + next;
};

/**
 * 合并分块内容，还原完整文档。chunks 按 start_at 排序后逐段用文本重叠匹配拼接。
 */
export const mergeChunks = (chunks: any[]): string => {
  if (!chunks || chunks.length === 0) return '';

  // 按 start_at 排序
  const sortedChunks = [...chunks].sort((a, b) => {
    const startA = a.start_at ?? a.chunk_index ?? 0;
    const startB = b.start_at ?? b.chunk_index ?? 0;
    return startA - startB;
  });

  let merged = sortedChunks[0].content || '';
  let mergedEnd = sortedChunks[0].end_at ?? 0;

  for (let i = 1; i < sortedChunks.length; i++) {
    const currentChunk = sortedChunks[i];
    const currentStartAt = currentChunk.start_at ?? 0;
    const currentEndAt = currentChunk.end_at ?? 0;
    const currentContent = currentChunk.content || '';

    if (!currentContent) continue;

    // 与上一段有明显间隙（位置不相邻），用空行分隔后整段拼接
    if (currentStartAt > mergedEnd && mergedEnd > 0) {
      merged = merged + '\n\n' + currentContent;
    } else {
      const positionOverlap = mergedEnd - currentStartAt;
      merged = appendChunkContent(merged, currentContent, positionOverlap);
    }

    if (currentEndAt > mergedEnd) {
      mergedEnd = currentEndAt;
    }
  }

  return merged;
};
