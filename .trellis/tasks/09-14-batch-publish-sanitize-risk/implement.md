# 实施计划：批量发布文本清洗与风控分类

## 1. 应用层文本归一

- [x] 在 `internal/application/items` 增加标题/描述纯函数及聚焦测试。
- [x] 在 `BatchPreviewService.parseRow` 校验前统一应用，确保预览、持久化和最终发布一致。
- [x] 复核 HTML、实体、控制字符、换行和普通尖括号文本不会误删有效内容。

## 2. 平台失败分类

- [x] 在 `internal/xianyu/mtop` 增加明确发布拒绝判定和业务/风险回归测试。
- [x] 在 `internal/adapter/item_batch_publish.go` 让明确拒绝直接返回，保留真正未知结果包装。
- [x] 覆盖标题字符限制、风险验证、网络错误、取消和远端结果检查点失败。

## 3. 风控停止与默认间隔

- [x] 为 `BatchRunner` 增加失败后停止策略，默认兼容现有 Session 行为。
- [x] 组合层对 Session 失效和风险验证停止剩余行，保持未执行行可人工重试。
- [x] 普通批量发布 Hook 与货源目录面板新建任务默认调整为 15 秒，历史值不覆盖。
- [x] 补齐 Go 和前端行为测试。

## 4. 验证

- [x] 聚焦 `internal/application/items`、`internal/xianyu/mtop`、`internal/adapter` 测试和 race。
- [x] 聚焦 Go vet、前端 typecheck 与行为测试。
- [x] `make api-check`、`go run ./tools/architecturecheck`、触碰文件注释检查。
- [x] `npm run build --prefix frontend`、`make build`、`git diff --check`。
- [x] 尽可能执行全量回归；既有 tray、注释债务和 bundle 大小基线独立报告。

## 4A. 货源商品删除保护价

- [x] 在货源应用服务区分实例不存在与远程商品不存在，并由自动化适配器转换为消费者哨兵。
- [x] 商品页定时跟价命中商品不存在时复用既有改价链路写入 `9999.00` 元；其他报价错误保持只告警。
- [x] 覆盖商品不存在传播、保护价远端改价与本地保存、普通错误不误改价。

## 5. 评审边界

- [x] 不修改冻结 CAPTCHA 文件或调用顺序。
- [x] 不把普通业务失败扩大成自动重试；重试仍由用户触发。
- [x] 不削弱成功后检查点失败的 uncertain 防重放保护。
- [x] 不修改历史批次间隔或数据库默认值。

## 验证记录

- 聚焦 Go 测试、相关包 vet、架构门禁、API 契约、服务构建、前端类型检查、14 条聚焦前端行为测试和生产构建通过。
- 相关 Go race 通过；任务范围覆盖率合计 83.3%。
- 全量 Go 测试仅被既有 macOS `cmd/tray`/`fyne.io/systray` 编译问题阻断，其余包通过。
- 全量前端测试 537/538 通过；唯一失败是既有 `Fulfillment` 分片超过 20 KiB 预算。
- 全仓注释门禁仍有既有 Go/前端债务；本次触碰文件过滤检查无新增。`golangci-lint` 本机未安装。
