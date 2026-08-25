import { contractClient, runContractRequest } from './client';
import type { components } from './generated/schema';

/** FulfillmentInstance 是不包含 API Key 明文的货源实例。 */
export type FulfillmentInstance = components['schemas']['FulfillmentInstance'];
/** FulfillmentInstanceInput 是货源实例表单输入。 */
export type FulfillmentInstanceInput = components['schemas']['FulfillmentInstanceInput'];
/** FulfillmentProduct 是不同货源协议归一化后的商品。 */
export type FulfillmentProduct = components['schemas']['FulfillmentProduct'];
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
