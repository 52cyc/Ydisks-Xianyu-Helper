import path from 'path';
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  base: '/static/',
  server: {
    port: 3000,
    host: '0.0.0.0',
    proxy: {
      // 代理API请求到后端
      '/api': {
        target: 'http://localhost:59188',
        changeOrigin: true,
		ws: true,
      },
      '/health': {
        target: 'http://localhost:59188',
        changeOrigin: true,
      },
    },
  },
  plugins: [react()],
  test: {
    environment: 'node',
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary', 'html'],
      reportsDirectory: './coverage',
      include: ['**/*.{ts,tsx}'],
      exclude: [
        '**/*.test.{ts,tsx}',
        '**/node_modules/**',
        '**/dist/**',
        '**/scripts/**',
        '**/vite.config.ts',
        // 纯 UI 组件不属于本项目的业务覆盖率目标，交互逻辑应在业务 Hook/状态模块中验证。
        '**/components/**',
        'components/**',
        'App.tsx',
        'index.tsx',
        'chatEmojis.tsx',
        'app/features/dashboard/DashboardTrendChart.tsx',
        // 路由页面、路由装配、应用外壳与通用 UI 同属纯展示层，业务行为由各 feature 的 Hook/状态/Action 模块验证。
        'app/features/*/pages/**',
        'app/router/AppRouter.tsx',
        'app/shell/**',
        'shared/ui/**',
        // 顶层错误边界只渲染静态恢复界面，不含业务规则。
        'app/errors/**',
      ],
    },
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, '.'),
    },
  },
  build: {
    outDir: '../internal/webui/static',
    sourcemap: false,
    rollupOptions: {
      output: {
        // manualChunks 按依赖领域拆分生产构建文件，控制首屏资源体积。
        manualChunks(id) {
          // modulePath 是统一分隔符后的模块绝对路径，用于稳定匹配依赖目录。
          const modulePath = id.split(path.sep).join('/');
          // 聊天接口适配器在列表、发送和未知结果收口之间复用。
          if (modulePath.includes('/app/features/chat/api.')) {
            return 'chat-api';
          }
          // 聊天状态 Hook 含分页与并发收口逻辑，独立分片控制页面体积。
          if (modulePath.includes('/app/features/chat/hooks.')) {
            return 'chat-runtime';
          }
          // 聊天元数据包含快捷回复和备注弹窗等低频交互，独立分片可避免挤占会话阅读首屏。
          if (modulePath.includes('/app/features/chat/components/ChatMetadataFeature.') || modulePath.includes('/app/features/chat/metadata.')) {
            return 'chat-metadata';
          }
          // 商品选择、查询状态和商品消息卡片组成独立静态分片。
          if (
            modulePath.includes('/app/features/chat/components/ChatItemPickerDialog.') ||
            modulePath.includes('/app/features/chat/components/ItemMessageCard.') ||
            modulePath.includes('/app/features/chat/useChatItemPicker.')
          ) {
            return 'chat-items';
          }
          // 会话选择与删除交互独立分片。
          if (
            modulePath.includes('/app/features/chat/components/ConversationListItem.') ||
            modulePath.includes('/app/features/chat/components/DeleteConversationDialog.')
          ) {
            return 'chat-session-actions';
          }
          // 模板请求 Hook 只服务模板管理页，独立分片可保持编辑器页面在既有下载预算内。
          if (modulePath.includes('/app/features/delivery-templates/hooks.')) {
            return 'delivery-template-runtime';
          }
          // 通用集合响应适配器被多个业务页复用，固定独立分片避免被任一低频功能分片吸收。
          if (modulePath.includes('/shared/http/contract.')) {
            return 'contract';
          }
          // 商品表格是列表视图的独立渲染模块，与卡片视图分片，保留分享采集后的页面预算。
          if (modulePath.includes('/app/features/items/components/ItemTable.')) {
            return 'item-table';
          }
          // 账号间克隆弹窗属于商品页低频交互，独立分片可保持商品列表页面在既有下载预算内。
          if (modulePath.includes('/app/features/items/components/ItemCloneFlow.') || modulePath.includes('/app/features/items/cloneState.')) {
            return 'item-clone-flow';
          }
          // 分享链接批量采集弹窗属于商品页低频交互，独立分片避免增加商品列表主分片。
          if (modulePath.includes('/app/features/items/components/ItemLinkImportFlow.') || modulePath.includes('/app/features/items/linkImportState.')) {
            return 'item-link-import-flow';
          }
          // 货源目录批量操作是履约页低频能力，独立分片避免上游新增表单挤占页面主分片预算。
          if (modulePath.includes('/app/features/fulfillment/components/CatalogBatchPanel.') || modulePath.includes('/app/features/fulfillment/catalogBatch.')) {
            return 'fulfillment-catalog-batch';
          }
          // 规则安全确认和异常处置控件共用独立静态分片。
          if (
            modulePath.includes('/app/features/rules/components/AllItemsConfirmation.') ||
            modulePath.includes('/app/features/rules/components/AutomationIssuePanel.')
          ) {
            return 'rules-safety-controls';
          }
          // 关键词回复编辑器只在打开回复规则弹窗时使用，独立分片避免多表达式表单挤占规则主页面预算。
          if (modulePath.includes('/app/features/rules/components/ReplyRuleEditor.')) {
            return 'rules-reply-editor';
          }
          // 发货模板绑定编辑器按既有低频控件方式独立分片，使规则主页面保留原下载预算。
          if (modulePath.includes('/app/features/rules/components/TemplateVariantEditor.')) {
            return 'rules-template-editor';
          }
          // 关键词回复的商品多选与轻提示仅在回复规则交互中使用，独立分片避免挤占规则主页面预算。
          if (
            modulePath.includes('/app/features/rules/components/ItemMultiSelect.') ||
            modulePath.includes('/app/features/rules/components/Toast.')
          ) {
            return 'rules-reply-controls';
          }
          // 手动地点选择只在发布表单中使用，独立静态分片可控制商品页主分片预算并保留 feature 边界。
          if (
            modulePath.includes('/app/features/items/manualLocation.') ||
            modulePath.includes('/app/features/items/components/ManualLocationPicker.') ||
            modulePath.includes('/app/features/items/amapLocation.')
          ) {
            return 'item-location-picker';
          }
          if (!modulePath.includes('/node_modules/')) {
            return undefined;
          }
          if (
            modulePath.includes('/react/') ||
            modulePath.includes('/react-dom/') ||
            modulePath.includes('/scheduler/')
          ) {
            return 'react-vendor';
          }
          if (
            modulePath.includes('/recharts/') ||
            modulePath.includes('/victory-vendor/') ||
            modulePath.includes('/d3-')
          ) {
            return 'charts-vendor';
          }
          if (modulePath.includes('/lucide-react/')) {
            return 'icons-vendor';
          }
          // AMR 解码器体积较大，随聊天懒加载路由使用独立块，避免拖慢应用首屏并保持依赖可审计。
          if (modulePath.includes('/benz-amr-recorder/')) {
            return 'audio-codec';
          }
          return 'vendor';
        },
      },
    },
    emptyOutDir: true,
  },
});
