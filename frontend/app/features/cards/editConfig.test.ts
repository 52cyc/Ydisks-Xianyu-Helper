import { expect, test } from 'vitest';
import { emptyAddForm } from './cardActions';
import { apiConfigFromAddForm, apiConfigFromEditForm, updateAPIAddForm, updateAPIEditForm } from './editConfig';

test('未修改模板保留服务端内容，清空模板是显式替换而非保留', /* templateIntent 验证空输入不会重新引入旧密钥。 */ () => {
  expect(apiConfigFromEditForm({})).toMatchObject({ headers_action: 'retain', params_action: 'retain', headers: undefined, params: undefined, body: undefined });
  // cleared 是用户手动清空三个模板后的编辑草稿。
  const cleared = apiConfigFromEditForm({ api_headers_action: 'replace', api_params_action: 'replace', api_body_action: 'replace', api_headers: '', api_params: '', api_body: '' });
  expect(cleared).toMatchObject({ headers: {}, params: {}, body: {} });
  expect(apiConfigFromEditForm({ api_headers_action: 'clear', api_params_action: 'clear', api_body_action: 'clear' })).toMatchObject({ headers_action: 'clear', params_action: 'clear', headers: undefined, params: undefined, body: {} });
  // retainedBody 是正文保持沿用时的草稿：即使残留输入也不得提交，由服务端沿用已保存值。
  const retainedBody = apiConfigFromEditForm({ api_body_action: 'retain', api_body: '{"stale":"draft"}' });
  expect(retainedBody.body).toBeUndefined();
});

test('正文修改自动切换为替换，处理方式字段可被直接写入', /* bodyIntent 验证正文与 cURL 导入覆盖时不会被保留语义吞掉。 */ () => {
  // updated 是正文编辑后的草稿。
  const updated = updateAPIEditForm({ api_body_action: 'retain' }, 'body', '{"order_id":"{order_id}"}');
  expect(updated).toMatchObject({ api_body_action: 'replace', api_body: '{"order_id":"{order_id}"}' });
  expect(updateAPIEditForm({ api_headers_action: 'retain' }, 'headersAction', 'clear')).toMatchObject({ api_headers_action: 'clear' });
  expect(updateAPIEditForm({ api_params_action: 'retain' }, 'paramsAction', 'replace')).toMatchObject({ api_params_action: 'replace' });
  expect(updateAPIEditForm({ api_body_action: 'clear' }, 'bodyAction', 'retain')).toMatchObject({ api_body_action: 'retain' });
});

test('新建表单忽略模板处理方式字段，并以替换语义提交全部模板', /* addIntent 新建卡密没有可沿用的服务端模板。 */ () => {
  // form 是新建 API 卡密的最小草稿。
  const form = { ...emptyAddForm(), type: 'api' as const, content: 'https://example.com', api_body: '' };
  expect(updateAPIAddForm(form, 'headersAction', 'retain')).toBe(form);
  expect(apiConfigFromAddForm(form)).toMatchObject({ headers_action: 'replace', params_action: 'replace', body: {} });
});

test('查询参数修改自动切换为替换且保持其他编辑字段', /* parameterIntent 防止已回显参数改动被保留语义吞掉。 */ () => {
  // updated 是查询参数编辑后的完整草稿。
  const updated = updateAPIEditForm({ name: '积分卡', api_params_action: 'retain' }, 'params', '{"order_id":"changed"}');
  expect(updated).toMatchObject({ name: '积分卡', api_params_action: 'replace', api_params: '{"order_id":"changed"}' });
  expect(apiConfigFromEditForm(updated).params).toBe('{"order_id":"changed"}');
});
