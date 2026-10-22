/**
 * Markdown 渲染管线：文档内容 → 安全 HTML。
 *
 * 从 doc-content.vue 提取，供文档详情抽屉与浏览模式阅读器共用。
 * marked 是模块级单例，这里用 once 守卫统一完成全局配置
 * （breaks/gfm/katex/字面波浪线），避免每个组件各自 marked.use 叠加注册。
 */
import { marked } from 'marked';
import markedKatex from 'marked-katex-extension';
import 'katex/dist/katex.min.css';
import hljs from 'highlight.js';
import 'highlight.js/styles/github.css';

import { sanitizeHTML, safeMarkdownToHTML, createSafeImage, isValidImageURL } from '@/utils/security';
import { normalizeSpuriousTablePrefixes } from '@/utils/markdownTableNormalize';
import { ensureLiteralSingleTildeOnGlobalMarked } from '@/utils/markedLiteralTilde';
import i18n from '@/i18n';

let configured = false;
const ensureMarkedConfigured = () => {
  if (configured) return;
  configured = true;
  marked.use({
    breaks: true,      // 启用单行换行转 <br>
    gfm: true,         // 启用 GitHub Flavored Markdown
  });
  marked.use(markedKatex({ throwOnError: false, nonStandard: true }));
  ensureLiteralSingleTildeOnGlobalMarked();
};

let mermaidRenderCount = 0;

const preprocessMathDelimiters = (rawText: string): string => {
  if (!rawText || typeof rawText !== 'string') {
    return '';
  }
  return rawText
    .replace(/\\\[([\s\S]*?)\\\]/g, '$$$$$1$$$$')
    .replace(/\\\(([\s\S]*?)\\\)/g, '$$$1$$');
};

const renderer = new marked.Renderer();

renderer.image = function ({ href, title, text }) {
  if (!isValidImageURL(href)) {
    return `<p>${i18n.global.t('error.invalidImageLink')}</p>`;
  }

  const safeImage = createSafeImage(href, text || '', title || '');
  return `<figure>
                ${safeImage}
                <figcaption style="text-align: left;">${text || ''}</figcaption>
            </figure>`;
};

// 自定义代码块渲染器，只显示语言标签
renderer.code = function ({ text, lang }) {
  // 空值校验：防止 text 为 undefined 或 null
  if (!text || typeof text !== 'string') {
    text = '';
  }

  // Mermaid 图表处理
  if (lang === 'mermaid') {
    // 生成唯一ID
    const id = `mermaid-${++mermaidRenderCount}`;
    // 返回带有 mermaid 类的 div，后续由 mermaid.run() 处理
    return `<div class="mermaid" id="${id}">${text}</div>`;
  }

  let detectedLang = lang;
  let highlighted = '';
  if (lang && hljs.getLanguage(lang)) {
    try {
      highlighted = hljs.highlight(text, { language: lang }).value;
    } catch (e) {
      highlighted = hljs.highlightAuto(text).value;
      detectedLang = hljs.highlightAuto(text).language || lang;
    }
  } else {
    const auto = hljs.highlightAuto(text);
    highlighted = auto.value;
    detectedLang = auto.language || lang;
  }
  const displayLang = detectedLang || 'Code';
  return `
    <div class="code-block-wrapper">
      <div class="code-block-header">
        <span class="code-block-lang">${displayLang}</span>
      </div>
      <pre class="code-block-pre"><code class="hljs language-${detectedLang || ''}">${highlighted}</code></pre>
    </div>
  `;
};

/**
 * 安全地处理 Markdown 内容（使用 marked）。
 *
 * 注意：renderer 通过参数传入每次 parse，不做全局注册——DocumentPreview 等
 * 组件复用同一个 marked 模块，全局注册会让它们的预览意外套用这里的图片
 * 校验器。
 */
export const processMarkdown = (markdownText: string): string => {
  ensureMarkedConfigured();
  if (!markdownText || typeof markdownText !== 'string') return '';

  // 去除 Markdown 头部的 YAML Frontmatter（例如 --- title: xxx ---）
  let processedText = markdownText.replace(/^\s*---\r?\n[\s\S]*?\r?\n---\r?\n/, '');

  // 先还原原始文本中的 HTML 实体，让它们作为普通字符参与渲染
  processedText = processedText
    .replace(/&#39;/g, "'")
    .replace(/&#x27;/gi, "'")
    .replace(/&apos;/g, "'")
    .replace(/&#34;/g, '"')
    .replace(/&#x22;/gi, '"')
    .replace(/&quot;/g, '"')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&');

  // 处理被 <p> 包裹的表格行，转换为正常的表格行，并在前后补空行
  processedText = processedText.replace(/<p>\s*(\|[\s\S]*?\|)\s*<\/p>/gi, '\n$1\n');

  // MarkItDown 常在表格前插入空行 + 分隔行，渲染会出现多余空行
  processedText = normalizeSpuriousTablePrefixes(processedText);

  // 保留表格单元格中的 <br>，不转成换行，避免打散表格；其他区域原样交给 marked 处理

  // 先预处理数学定界符，再做安全预处理
  const mathSafeText = preprocessMathDelimiters(processedText);
  const safeMarkdown = safeMarkdownToHTML(mathSafeText);

  // 使用标记渲染
  // Do not register this renderer globally. DocumentPreview uses the same
  // `marked` module; registering here made its later Markdown preview reuse
  // this image validator after the user had opened the chunk view.
  let html = marked.parse(safeMarkdown, { renderer }) as string;

  // 还原被转义的 <br>
  html = html.replace(/&lt;br\s*\/?&gt;/gi, '<br>');

  // 最终安全清理
  return sanitizeHTML(html);
};
