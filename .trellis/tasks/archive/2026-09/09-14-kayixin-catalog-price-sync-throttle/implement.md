# 实施计划：卡易信单规格上货与价格同步限速

## 1. 后台价格同步

- [x] 在 `internal/automation` 为后台商品页价格同步增加 500ms 串行报价节流，等待受 Context 控制。
- [x] 复用/抽取规则配置判断，确保只对真正发起远程报价的规则等待。
- [x] 增加聚焦测试：串行顺序、最小间隔、单条失败继续、取消退出、重复商品仍只处理一次。

## 2. 卡易信目录能力

- [x] 在 `internal/kayixin/client.go` 实现 `ListCategories` 和 `ListProductPage`，并声明实现 `CatalogGateway`。
- [x] 补齐分类、分页总数、目录/关键词/单规格卡券筛选、未知库存和商品详情字段映射。
- [x] 为统一分页模型增加总页数字段：卡易信使用 `allPage`，卡速售按现有页大小推导。
- [x] 保持旧 `ListProducts` 聚合行为和现有下单/查单语义。
- [x] 扩充 `internal/kayixin/client_test.go`，使用本地 HTTP 服务验证路径、签名、请求正文和响应归一化。
- [x] 更新多协议网关测试，确认卡易信目录能力经现有路由可用。

## 3. 共用上架界面

- [x] 将 `CatalogBatchPanel` 的实例筛选和文案扩展为卡速售、卡易信共用。
- [x] 发布前详情复核改成显式串行请求。
- [x] 调整统一 CSV 构造文案、起购份数、售价与库存校验，保证卡易信详情数据进入描述、数量、保护价和规则配置。
- [x] 增加前端行为测试：卡易信实例可选择、目录/分页查询参数正确、详情串行、详情失效时不启动批次、卡速售回归。

## 4. 契约与构建

- [x] 在 `api/openapi.yaml` 的现有分页响应增加可选 `total_pages`，重新生成只读 TypeScript schema，并更新真实 handler 契约测试。
- [x] 前端优先使用服务端 `total_pages`，兼容字段缺失时保留旧页大小推算。
- [x] 构建前端到 `internal/webui/static`，保留哈希资源替换。
- [x] 不修改冻结 CAPTCHA 文件及其调用语义。

## 5. 验证

- [x] `env -u GOROOT go test ./internal/automation ./internal/kayixin ./internal/adapter ./internal/application/fulfillment ./internal/server -count=1`
- [x] `env -u GOROOT go vet ./internal/automation ./internal/kayixin ./internal/adapter ./internal/application/fulfillment ./internal/server`
- [x] `make api-check`
- [x] `go run ./tools/architecturecheck`
- [x] `make comments`，本次触碰文件均通过；全仓仍有未触碰的历史基线债务。
- [x] 使用仓库可用 Node 24 执行 `npm run typecheck --prefix frontend` 和相关前端测试。
- [x] `npm run build --prefix frontend`
- [x] `make build`
- [x] `git diff --check`
- [x] 已执行 `env -u GOROOT go test ./... -count=1`；除 macOS `cmd/tray` 的既有 `fyne.io/systray` 环境失败外通过。

## 6. 评审与回滚点

- [x] 复核没有新增并发 goroutine、没有把调度器等待扩散到付款采购和主动查询。
- [x] 复核 `allCount/allPage`、库存未知值和卡易信 `goodsType/skuType` 没有映射反转。
- [x] 复核所有新增或修改声明都有语义准确的中文注释和聚焦测试。
- [x] 供应商协议差异集中在卡易信适配层，前端只使用统一目录契约。
