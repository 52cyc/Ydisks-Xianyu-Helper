import { contractClient, contractMultipartBody, runContractRequest } from './client';
import type { components } from './generated/schema';

/** FulfillmentInstance 是不包含 API Key 明文的货源实例。 */
export type FulfillmentInstance = components['schemas']['FulfillmentInstance'];
/** FulfillmentInstanceInput 是货源实例表单输入。 */
export type FulfillmentInstanceInput = components['schemas']['FulfillmentInstanceInput'];
/** FulfillmentProduct 是不同货源协议归一化后的商品。 */
export type FulfillmentProduct = components['schemas']['FulfillmentProduct'];
/** FulfillmentCategory 是卡速售返回的递归商品目录节点。 */
export type FulfillmentCategory = components['schemas']['FulfillmentCategory'];
/** FulfillmentProductPage 是目录选品使用的分页商品响应。 */
export type FulfillmentProductPage = components['schemas']['FulfillmentProductListResponse'];
/** FulfillmentBatchPreview 是复用商品批量发布流程的预检结果。 */
export type FulfillmentBatchPreview = components['schemas']['ItemPublishBatchPreviewResponse'];
/** FulfillmentBatchStart 是启动批量发布后的任务标识。 */
export type FulfillmentBatchStart = components['schemas']['BatchIDResponse'];
/** FulfillmentPublishAccount 是批量上架选择器需要的非敏感账号字段。 */
export interface FulfillmentPublishAccount {
  /** id 是闲鱼账号标识。 */
  id: string;
  /** name 是账号昵称或备注组成的展示名称。 */
  name: string;
  /** enabled 表示账号当前允许执行发布任务。 */
  enabled: boolean;
}
/** FulfillmentMapping 是闲鱼规格和货源商品的映射。 */
export type FulfillmentMapping = components['schemas']['FulfillmentMapping'];
/** FulfillmentMappingInput 是商品映射表单输入。 */
export type FulfillmentMappingInput = components['schemas']['FulfillmentMappingInput'];
/** FulfillmentOrder 是外部货源履约订单。 */
export type FulfillmentOrder = components['schemas']['FulfillmentOrder'];
/** FulfillmentPurchaseRequest 是幂等采购请求。 */
export type FulfillmentPurchaseRequest = components['schemas']['FulfillmentPurchaseRequest'];

/** listFulfillmentInstances 读取当前用户的所有货源实例。 */
export async function listFulfillmentInstances(): Promise<FulfillmentInstance[]> {
  // response 是 OpenAPI 契约返回的实例列表包装。
  const response = await runContractRequest(/* signal 控制货源实例请求。 */ signal => contractClient.GET('/api/v1/fulfillment/instances', { signal }));
  return response.data;
}

/** createFulfillmentInstance 创建一个受支持协议的货源实例。 */
export function createFulfillmentInstance(input: FulfillmentInstanceInput): Promise<FulfillmentInstance> {
  return runContractRequest(/* signal 控制货源实例创建请求。 */ signal => contractClient.POST('/api/v1/fulfillment/instances', { body: input, signal }));
}

/** deleteFulfillmentInstance 删除一个未被引用的货源实例。 */
export async function deleteFulfillmentInstance(instanceId: number): Promise<void> {
  await runContractRequest(/* signal 控制货源实例删除请求。 */ signal => contractClient.DELETE('/api/v1/fulfillment/instances/{instance_id}', { params: { path: { instance_id: instanceId } }, signal }));
}

/** listFulfillmentProducts 从指定货源实例同步商品。 */
export async function listFulfillmentProducts(instanceId: number): Promise<FulfillmentProduct[]> {
  // response 是 OpenAPI 契约返回的商品列表包装。
  const response = await runContractRequest(/* signal 控制远程商品请求。 */ signal => contractClient.GET('/api/v1/fulfillment/instances/{instance_id}/products', { params: { path: { instance_id: instanceId } }, signal }));
  return response.data;
}

/** listFulfillmentCategories 读取当前目录型货源实例的商品目录树。 */
export async function listFulfillmentCategories(instanceId: number): Promise<FulfillmentCategory[]> {
  // response 是 OpenAPI 契约返回的目录列表包装。
  const response = await runContractRequest(/* signal 控制远程目录请求。 */ signal => contractClient.GET('/api/v1/fulfillment/instances/{instance_id}/categories', { params: { path: { instance_id: instanceId } }, signal }));
  return response.data;
}

/** listFulfillmentProductPage 按最终目录、关键词和页码读取货源商品。 */
export function listFulfillmentProductPage(instanceId: number, categoryId: number, keyword: string, page: number, pageSize = 50): Promise<FulfillmentProductPage> {
  return runContractRequest(/* signal 控制目录商品分页请求。 */ signal => contractClient.GET('/api/v1/fulfillment/instances/{instance_id}/products', {
    params: { path: { instance_id: instanceId }, query: { category_id: categoryId || undefined, keyword: keyword || undefined, page, page_size: pageSize } }, signal,
  }));
}

/** listFulfillmentPublishAccounts 读取批量上架需要的非敏感闲鱼账号选择项。 */
export async function listFulfillmentPublishAccounts(): Promise<FulfillmentPublishAccount[]> {
  // response 是账号详情接口返回的非敏感账号数组。
  const response = await runContractRequest(/* signal 控制发布账号请求。 */ signal => contractClient.GET('/api/v1/accounts/details', { signal }));
  return response.map(/* account 是当前转换的账号传输对象。 */ account => ({
    id: account.id,
    name: account.nickname || account.remark || `账号 ${account.id.slice(0, 6)}`,
    enabled: account.enabled === true,
  }));
}

/** previewFulfillmentPublishBatch 把选品生成的 CSV 送入现有批量发布预检。 */
export function previewFulfillmentPublishBatch(file: File, accountId: string, publishIntervalSeconds: number): Promise<FulfillmentBatchPreview> {
  // body 保存无需图片压缩包的远程图片批量预检表单。
  const body = new FormData();
  body.set('file', file);
  body.set('default_cookie_id', accountId);
  body.set('fallback_category_id', '');
  body.set('fallback_category_name', '');
  body.set('fallback_channel_category_id', '');
  body.set('fallback_tb_category_id', '');
  body.set('publish_interval_seconds', String(publishIntervalSeconds));
  return runContractRequest(/* signal 控制货源选品预检上传。 */ signal => contractClient.POST('/api/v1/items/publish-batches/preview', { body: contractMultipartBody(body), signal })) as unknown as Promise<FulfillmentBatchPreview>;
}

/** startFulfillmentPublishBatch 启动已全部通过预检的现有批量发布任务。 */
export function startFulfillmentPublishBatch(previewId: string): Promise<FulfillmentBatchStart> {
  return runContractRequest(/* signal 控制货源批量发布启动请求。 */ signal => contractClient.POST('/api/v1/items/publish-batches', { body: { preview_id: previewId }, signal })) as unknown as Promise<FulfillmentBatchStart>;
}

/** getFulfillmentProduct 按商品 ID 校验并读取货源商品详情。 */
export function getFulfillmentProduct(instanceId: number, goodsId: number): Promise<FulfillmentProduct> {
  return runContractRequest(/* signal 控制单商品校验请求。 */ signal => contractClient.GET('/api/v1/fulfillment/instances/{instance_id}/products/{goods_id}', { params: { path: { instance_id: instanceId, goods_id: goodsId } }, signal }));
}

/** listFulfillmentMappings 读取闲鱼商品规格映射。 */
export async function listFulfillmentMappings(): Promise<FulfillmentMapping[]> {
  // response 是 OpenAPI 契约返回的映射列表包装。
  const response = await runContractRequest(/* signal 控制商品映射请求。 */ signal => contractClient.GET('/api/v1/fulfillment/mappings', { signal }));
  return response.data;
}

/** createFulfillmentMapping 创建闲鱼规格到货源商品的映射。 */
export function createFulfillmentMapping(input: FulfillmentMappingInput): Promise<FulfillmentMapping> {
  return runContractRequest(/* signal 控制商品映射创建请求。 */ signal => contractClient.POST('/api/v1/fulfillment/mappings', { body: input, signal }));
}

/** deleteFulfillmentMapping 删除一条商品映射。 */
export async function deleteFulfillmentMapping(mappingId: number): Promise<void> {
  await runContractRequest(/* signal 控制商品映射删除请求。 */ signal => contractClient.DELETE('/api/v1/fulfillment/mappings/{mapping_id}', { params: { path: { mapping_id: mappingId } }, signal }));
}

/** listFulfillmentOrders 读取最新履约订单。 */
export async function listFulfillmentOrders(): Promise<FulfillmentOrder[]> {
  // response 是 OpenAPI 契约返回的履约订单列表包装。
  const response = await runContractRequest(/* signal 控制履约订单请求。 */ signal => contractClient.GET('/api/v1/fulfillment/orders', { params: { query: { limit: 100 } }, signal }));
  return response.data;
}

/** purchaseFulfillmentOrder 使用稳定外部单号创建或返回原采购单。 */
export function purchaseFulfillmentOrder(input: FulfillmentPurchaseRequest): Promise<FulfillmentOrder> {
  return runContractRequest(/* signal 控制幂等采购请求。 */ signal => contractClient.POST('/api/v1/fulfillment/orders', { body: input, signal }));
}

/** refreshFulfillmentOrder 使用原外部单号查询最新状态。 */
export function refreshFulfillmentOrder(externalOrderNo: string): Promise<FulfillmentOrder> {
  return runContractRequest(/* signal 控制履约订单刷新请求。 */ signal => contractClient.POST('/api/v1/fulfillment/orders/{external_order_no}/refresh', { params: { path: { external_order_no: externalOrderNo } }, signal }));
}
