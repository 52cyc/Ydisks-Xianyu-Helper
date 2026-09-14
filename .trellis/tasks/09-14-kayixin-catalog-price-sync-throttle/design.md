# 技术设计：卡易信单规格上货与价格同步限速

## 1. 变更边界

最小行为差距有两个：后台同步是串行但没有请求间隔；卡易信客户端已有履约能力但未实现统一目录选品接口。两者都在既有边界内修补，不新增数据库、HTTP 路由或供应商专用前端数据流。

明确不触碰：滑块和浏览器登录、账号凭证、订单采购幂等逻辑、直充/多规格上架、评价任务和闲鱼标题清洗。

## 2. 价格同步限速

### 所有权

`internal/automation/Scheduler` 继续拥有价格同步扫描生命周期。限速状态仅存在于单次 `scanExternalListingPrices` 调用栈，不增加共享可变全局状态或后台 goroutine。

### 数据流

```text
启用规则列表
  -> 去重同账号商品
  -> Context 可取消等待到下一报价时间
  -> externalListingTargetCents 串行查询货源
  -> 必要时修改闲鱼商品价格
  -> 下一条规则
```

第一条需要报价的规则立即执行；后续候选规则在进入实时报价前等待至上一次报价开始时间加 500ms。为了避免给没有开启外部价格同步的规则增加无意义等待，抽取一个只判断规则是否包含有效价格同步动作的内部辅助函数，实际计算仍由既有函数完成。

测试注入时钟/等待函数或使用可控短间隔，验证调用顺序、最小间隔、错误后继续以及取消退出；生产默认值保持 500ms。

## 3. 卡易信目录网关

### 分类

`kayixin.Client.ListCategories` 调用 `/api/v3/goods/getDirs`，递归映射 `id/name/children`。`brands` 不属于当前统一上架筛选条件，忽略但不改变远程请求。

### 分页列表

`kayixin.Client.ListProductPage` 调用 `/api/v3/goods/getList`：

- `page`：统一查询页码；
- `goodsType="1"`：仅卡券；
- `skuType="0"`：仅单规格；
- `showDirId="1"`：要求返回分类关联；
- `dirId`：统一 `CategoryID` 转十进制字符串；
- `keyWord`：去空格后的关键词。

卡易信没有 page size 参数，不能用当前页条数或固定 50 条反推页数。统一 `ProductPage` 和 HTTP DTO 增加 `TotalPages`：卡易信直接读取 `allPage`，卡速售按既有 `total/pageSize` 推导。总条数继续读取 `allCount`，旧 `ListProducts` 使用 `allPage` 作为结束条件，避免破坏既有调用方。

列表响应没有 `stockCount` 时统一设为 `-1`，表示未知而不是缺货。详情继续要求 `stockCount > 0`，并拒绝 `skuType != 0`。

### 商品详情

扩展卡易信商品字段映射：

```text
goodsDescribe + goodsDetail -> Description
buyNotice                  -> Notice
minQuantity                -> StartCount
maxQuantity                -> EndCount
salesPrice                 -> Price / SafePrice 来源
stockCount                 -> Stock
```

卡易信下单接口支持 `safePrice`，统一模型 `CanSetPrice=true`，让现有成本保护继续生效。

当 `minQuantity > 1` 时，每件闲鱼商品对应采购 `minQuantity` 份同一货源 SKU；初始售价与后台同步统一采用“单份加价向上取整到分，再乘采购份数”，闲鱼可售数量按库存和 `maxQuantity` 折算，避免超卖或首次同步后立即改价。该份数随 CSV 的 `external_delivery_count` 进入发布后付款规则。

## 4. 前端复用

现有 `CatalogBatchPanel` 从“卡速售专用”调整为“支持目录选品的货源实例”，当前允许 `kasushou_v2` 与 `kayixin_v3`。面板标题、实例标签、错误和 CSV 文件名改成供应商中性文案。

发布前详情复核由 `Promise.all` 改为 `for...of await`，确保浏览器一次只发出一个详情请求。统一 CSV 构造仍校验卡密、可采购、库存、主图和价格；列表阶段允许 `stock_num=-1`，详情阶段必须正库存。

## 5. 契约与兼容

- 不新增 `/api/v1` operation；为现有分页响应增加可选 `total_pages`，同步 OpenAPI、生成 TypeScript、handler 契约测试和前端适配。
- 不新增规格表达字段，优先使用当前 `CanBuy` 与详情拒绝保持商品 DTO 稳定。
- 卡速售和蜜蜂履约路由不变。
- 错误仍通过现有统一错误 envelope 返回，不暴露 AppSecret、签名或请求正文。

## 6. 风险与回滚

- 风险：文档对 `allCount/allPage` 的中文说明疑似互换。以字段名和示例值为准，并用本地响应夹具锁定。
- 风险：卡易信商品没有主图时无法发布到闲鱼；保持明确拦截，不伪造图片。
- 风险：500ms 限速让大量商品的一轮同步耗时增加；这是主动保护供应站限频，Context 取消保证关闭不受影响。
- 回滚：删除卡易信 `CatalogGateway` 方法和前端实例扩展即可恢复只支持卡速售上架；移除同步等待即可恢复旧速度，不涉及数据迁移。
