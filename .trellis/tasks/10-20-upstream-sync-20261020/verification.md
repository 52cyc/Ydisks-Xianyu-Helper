# 验证记录

- 最终Go全量：`env -u GOROOT CGO_ENABLED=1 make cover`通过，statement88.5%，未设置`RUN_BROWSER_INTEGRATION=1`。首轮仅两个新组合根测试缺少fork必需LiveBuffer而失败；补齐测试依赖后全量复跑通过。
- 数据库：`env -u GOROOT CGO_ENABLED=1 go test ./internal/db -count=1` 通过；fork56旧schema、真实Goose账本及履约、跟价、免拼、关键词数据升级到59回归通过；三方言57–59对齐，旧52–56无修改。无Docker与外部测试连接，MySQL/PostgreSQL未实库执行。
- 自动化：全包测试、vet通过。新增“发卡结果未知+确认准备失败”回归证明去掉修复失败、修复后转人工核对，保留外部原单等待及处理中刷新机制。
- 前端：Node24全量123文件745测试通过，`npm --prefix frontend run test:coverage` statement91.53%。此后补齐API正文默认遮罩，卡密与包体7文件41测试通过；最终typecheck、comments、API漂移、build通过。完整覆盖率采集在遮罩补丁之前，没有将定向测试冒充最终全量覆盖率。
- 结构与契约：架构门禁、完整`make api-check`、全库vet通过；拆分同包订单DTO及动作准备以符合800行门禁，字段和行为保留。
- 运行：`make smoke`在保留Chromium的情况下通过初始化、登录、核心列表、健康检查、首页与优雅退出；`make test-server-race`通过。
- 浏览器：首次并行`make cover-browser`失败，未修改任何浏览器文件；单独以`RUN_BROWSER_INTEGRATION=1`重跑完整浏览器包通过，statement64.1%。不把首次失败当作通过。
- 既有限制：Go中文注释仍142项欠缺；未安装golangci-lint，lint未执行。不扩大基线或放宽门禁。
- 未验证：真实闲鱼交易、外部货源采购/卡密API、生产数据库升级、桌面安装包、MySQL/PostgreSQL实库；未部署、不推送。
