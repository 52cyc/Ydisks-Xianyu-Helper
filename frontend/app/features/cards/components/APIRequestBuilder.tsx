import { Eye, EyeOff, Loader2, Play, Plus, Trash2 } from 'lucide-react';
import { useEffect, useRef, useState, type ChangeEvent, type ReactNode } from 'react';
import type { CardAPITestResult } from '../models';
import { CurlImportError, parseCurlCommand } from '../curlImport';

// APIRequestMethod 是 API 发货请求支持的 HTTP 方法。
export type APIRequestMethod = 'GET' | 'POST';

// APITemplateAction 描述编辑已保存卡密时对服务端模板的处理方式：保留沿用、重新填写替换或清除。
export type APITemplateAction = 'retain' | 'replace' | 'clear';

// APISavedTemplateKeys 是服务端返回的已保存模板顶层键名；值永不下发到浏览器。
export interface APISavedTemplateKeys {
  // headers 是已保存请求头的键名。
  headers: string[];
  // params 是已保存查询参数的键名。
  params: string[];
  // body 是已保存请求正文的键名。
  body: string[];
}

// APITemplateActions 是三个模板当前的处理方式，与 APISavedTemplateKeys 一一对应。
export interface APITemplateActions {
  // headers 是请求头模板的处理方式。
  headers: APITemplateAction;
  // params 是查询参数模板的处理方式。
  params: APITemplateAction;
  // body 是请求正文模板的处理方式。
  body: APITemplateAction;
}

// APIKeyValueRow 是 Postman 风格键值编辑器中的一行表单数据。
export interface APIKeyValueRow {
  // key 是请求头、查询参数或表单字段名称。
  key: string;
  // value 是字段值；动态变量会以文本形式保留到服务端替换。
  value: string;
}

// APIRequestBuilderProps 描述 API 请求编辑器的受控字段和保存动作。
export interface APIRequestBuilderProps {
  // url 是远端 API 地址。
  url: string;
  // method 是远端 API 请求方法。
  method: APIRequestMethod;
  // timeout 是单次远端请求的超时秒数。
  timeout: number;
  // headers 是请求头 JSON 文本，组件以键值行展示。
  headers: string;
  // params 是 URL 查询参数 JSON 文本，组件以键值行展示。
  params: string;
  // contentType 是 POST 请求正文的 Content-Type。
  contentType: string;
  // body 是 JSON 正文或非 JSON 正文的键值 JSON 文本。
  body: string;
  // responsePath 是成功响应中的卡密提取路径。
  responsePath: string;
  // retryEnabled 表示是否启用带幂等键的重试。
  retryEnabled: boolean;
  // savedKeys 仅在编辑已保存卡密时提供，列出服务端已保存模板的键名；新建时省略。
  savedKeys?: APISavedTemplateKeys;
  // templateActions 是编辑已保存卡密时三个模板的处理方式，与 savedKeys 同时提供。
  templateActions?: APITemplateActions;
  // onChange 更新指定 API 配置字段。
	onChange: (field: APIRequestField, value: string | number | boolean) => void;
	// onTest 使用当前配置执行临时测试请求。
	onTest?: (signal: AbortSignal) => Promise<CardAPITestResult>;
}

// APIRequestField 是 APIRequestBuilder 可更新字段的联合类型。
export type APIRequestField =
  | 'url'
  | 'method'
  | 'timeout'
  | 'headers'
  | 'params'
  | 'contentType'
  | 'body'
  | 'responsePath'
  | 'retryEnabled'
  | 'headersAction'
  | 'paramsAction'
  | 'bodyAction';

// APITestState 保存 API 测试请求的加载状态、非敏感诊断结果与请求错误。
interface APITestState {
  // loading 表示当前是否正在等待远端测试响应。
  loading: boolean;
  // result 是远端完成请求后返回的状态和响应结构诊断。
  result?: CardAPITestResult;
  // error 是本地请求、超时或配置错误的用户可见提示。
  error?: string;
}

// CurlImportState 保存仅在当前弹窗存活的 cURL 粘贴文本和解析错误；它不会写入服务端或日志。
interface CurlImportState {
  // command 保存用户刚刚粘贴、尚未确认导入的 cURL 命令。
  command: string;
  // error 保存导入失败时可修正的本地提示。
  error?: string;
}

// contentTypes 保存常用且不依赖文件上传的请求正文类型。
const contentTypes = [
  { value: 'application/json', label: 'JSON（application/json）' },
  { value: 'application/x-www-form-urlencoded', label: '表单键值（x-www-form-urlencoded）' },
  { value: 'text/plain', label: '纯文本（text/plain）' },
  { value: 'application/xml', label: 'XML（application/xml）' },
];

// isJSONContentType 判断正文类型是否应该使用 JSON 编辑器。
const isJSONContentType = (contentType: string): boolean => contentType.toLowerCase().includes('json');

// rowsFromJSON 将已保存的 JSON 对象转成适合键值编辑器展示的行。
const rowsFromJSON = (value: string): APIKeyValueRow[] => {
  if (!value.trim()) return [{ key: '', value: '' }];
  try {
    // parsed 是配置模板解析后的未知 JSON 值。
    const parsed: unknown = JSON.parse(value);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return [{ key: '', value: '' }];
    // entries 是对象中每个字段的键和值。
    const entries = Object.entries(parsed as Record<string, unknown>);
    if (entries.length === 0) return [{ key: '', value: '' }];
    return entries.map(/* entry 保存当前对象字段的键和值。 */ entry => {
      // key、entryValue 分别是当前模板字段名称和字段值。
      const [key, entryValue] = entry;
      return { key, value: typeof entryValue === 'string' ? entryValue : JSON.stringify(entryValue) };
    });
  } catch {
    return [{ key: '', value: value.trim() }];
  }
};

// rowsToJSON 将键值行序列化为服务端使用的 JSON 对象文本。
const rowsToJSON = (rows: APIKeyValueRow[]): string => {
  // object 是过滤空键后待提交的键值对象。
  const object = rows.reduce<Record<string, string>>(/* reduceCallback 汇总非空键名的模板字段。 */ (result, row) => {
    // key 是当前行清理空白后的字段名称。
    const key = row.key.trim();
    if (key) result[key] = row.value;
    return result;
  }, {});
  return JSON.stringify(object);
};

// KeyValueEditorProps 描述一组 Postman 风格键值行编辑器。
interface KeyValueEditorProps {
  // label 是当前键值区块的可见标题。
  label: string;
  // value 是当前键值对象的 JSON 文本。
  value: string;
  // placeholder 是第一行输入的示例文本。
  placeholder: string;
  // onChange 接收序列化后的键值对象。
  onChange: (value: string) => void;
}

// SensitiveValueProps 描述键值编辑器中支持遮罩的单个字段。
interface SensitiveValueProps {
  // label 为输入框提供稳定的可访问名称。
  label: string;
  // fieldName 决定是否对凭证字段默认遮罩。
  fieldName: string;
  // value 保存当前字段的完整模板值。
  value: string;
  // onChange 更新父级草稿中的字段值。
  onChange: (value: string) => void;
}

// SensitiveValue 默认隐藏凭证值；只有主动点击才展示，普通动态参数保持可读。
const SensitiveValue = ({ label, fieldName, value, onChange }: SensitiveValueProps) => {
  // revealed 仅在当前字段组件存活期间保留展开状态。
  const [revealed, setRevealed] = useState(false);
  // sensitive 匹配常见认证字段，不把幂等键误判为密钥。
  const sensitive = /authorization|cookie|token|secret|password|passwd|credential|signature|api[-_]?key|access[-_]?key|private[-_]?key/i.test(fieldName);
  return <div className="flex min-w-0 items-center gap-1">
    <input aria-label={label} type={sensitive && !revealed ? 'password' : 'text'} value={value}
      autoComplete="off" spellCheck={false}
      onChange={/* updateSecret 将修改后的字段值交回父级草稿。 */ event => onChange(event.target.value)}
      placeholder="值或 {order_id}"
      className="min-w-0 w-full border-0 bg-transparent px-1 py-2 font-mono text-sm text-gray-800 outline-none ring-0 placeholder:text-gray-300" />
    {sensitive && <button type="button" aria-label={`${revealed ? '隐藏' : '显示'}${fieldName}`} aria-pressed={revealed}
      onClick={/* toggleSecret 切换当前凭证值的可见状态。 */ () => setRevealed(!revealed)}
      className="shrink-0 rounded-lg p-1.5 text-gray-500 hover:bg-gray-100">
      {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
    </button>}
  </div>;
};

// SavedTemplateSectionProps 描述一个可能已在服务端保存了值的模板区块。
interface SavedTemplateSectionProps {
  // label 是模板区块的可见名称。
  label: string;
  // keys 是服务端已保存模板的键名；未提供或为空表示没有可保留的值。
  keys?: string[];
  // action 是当前处理方式；未提供时视为直接填写。
  action?: APITemplateAction;
  // onAction 切换当前模板的处理方式。
  onAction: (action: APITemplateAction) => void;
  // children 是重新填写时显示的编辑器。
  children: ReactNode;
}

// SavedTemplateSection 在保留已保存模板时只展示键名，值从不进入浏览器；需要修改时整体重新填写或清除。
const SavedTemplateSection = ({ label, keys, action, onAction, children }: SavedTemplateSectionProps) => {
  if (!keys || keys.length === 0 || !action) return <>{children}</>;
  if (action === 'retain') {
    return <div className="space-y-2 rounded-xl border border-gray-200 bg-white/80 p-3" role="group" aria-label={`${label}已保存`}>
      <p className="text-sm font-bold text-gray-800">{label}</p>
      <p className="text-xs text-gray-600">已保存 {keys.length} 个字段：<span className="font-mono">{keys.join('、')}</span></p>
      <p className="text-xs text-gray-500">值只保存在服务端，不会显示；保存和测试时自动沿用。</p>
      <div className="flex gap-2">
        <button type="button" onClick={/* replaceTemplate 放弃沿用，改为整体重新填写。 */ () => onAction('replace')} className="rounded-lg bg-gray-900 px-3 py-1.5 text-xs font-bold text-white">重新填写</button>
        <button type="button" onClick={/* clearTemplate 保存时清除该模板。 */ () => onAction('clear')} className="rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-bold text-gray-700">清除</button>
      </div>
    </div>;
  }
  if (action === 'clear') {
    return <div className="flex flex-wrap items-center gap-2 rounded-xl border border-red-200 bg-red-50 p-3 text-xs" role="group" aria-label={`${label}将清除`}>
      <span className="font-semibold text-red-700">保存后将清除已保存的{label}</span>
      <button type="button" onClick={/* undoClear 撤销清除，继续沿用已保存值。 */ () => onAction('retain')} className="rounded-lg bg-white px-3 py-1.5 font-bold text-gray-700">撤销</button>
    </div>;
  }
  return <div className="space-y-2">
    {children}
    <button type="button" onClick={/* restoreTemplate 放弃本次填写，改回沿用服务端已保存值。 */ () => onAction('retain')} className="text-xs font-semibold text-blue-600 hover:underline">改回沿用已保存的{label}</button>
  </div>;
};

// KeyValueEditor 渲染可增删的键值对输入行。
const KeyValueEditor = ({ label, value, placeholder, onChange }: KeyValueEditorProps) => {
  // rows 保存当前键值编辑行；空行也必须保留在本地状态中，才能支持连续添加。
  const [rows, setRows] = useState<APIKeyValueRow[]>(() => rowsFromJSON(value));
  // lastEmittedValue 记录本组件最近一次提交的 JSON，避免空行序列化为 {} 后被外部同步立即抹掉。
  const lastEmittedValue = useRef(value);

  // syncRows 在父表单从外部载入新模板时重置编辑行；本组件自己的更新不重复覆盖空行。
  useEffect(/* syncRowsEffect 同步外部模板变更并保留本地新增空行。 */ () => {
    if (value !== lastEmittedValue.current) {
      setRows(rowsFromJSON(value));
      lastEmittedValue.current = value;
    }
  }, [value]);

  // emitRows 保存本地键值行并通知父表单更新敏感模板 JSON。
  const emitRows = (nextRows: APIKeyValueRow[]) => {
    // serialized 是当前键值行转成的服务端 JSON 对象文本。
    const serialized = rowsToJSON(nextRows);
    setRows(nextRows);
    lastEmittedValue.current = serialized;
    onChange(serialized);
  };

  // updateRow 修改某一行并把完整对象交回父表单。
  const updateRow = (index: number, field: keyof APIKeyValueRow, nextValue: string) => {
    // nextRows 是修改当前字段后的完整键值行列表。
    const nextRows = rows.map(/* rowUpdater 更新当前键值编辑行。 */ (row, rowIndex) => rowIndex === index ? { ...row, [field]: nextValue } : row);
    emitRows(nextRows);
  };

  // addRow 在末尾追加一行空键值输入。
  const addRow = () => emitRows([...rows, { key: '', value: '' }]);

  // removeRow 删除指定键值行，并确保编辑器始终保留一行可输入内容。
  const removeRow = (index: number) => emitRows(rows.filter(/* rowFilter 保留未被删除的键值行。 */ (_, rowIndex) => rowIndex !== index));

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-3">
        <label className="text-sm font-bold text-gray-800">{label}</label>
        <button
          type="button"
          onClick={addRow}
          className="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-xs font-bold text-blue-600 transition-colors hover:bg-blue-50"
          title={`添加${label}字段`}
        >
          <Plus className="h-3.5 w-3.5" />
          添加字段
        </button>
      </div>
      <div className="overflow-hidden rounded-xl border border-gray-200 bg-white">
        <div className="grid grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)_40px] gap-2 border-b border-gray-100 bg-gray-50 px-3 py-2 text-[11px] font-bold uppercase tracking-wide text-gray-400">
          <span>KEY</span>
          <span>VALUE</span>
          <span aria-hidden="true" />
        </div>
        <div className="divide-y divide-gray-100">
          {rows.map(/* rowRenderer 渲染当前键值编辑行。 */ (row, index) => (
            <div className="grid grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)_40px] items-center gap-2 px-3 py-2" key={`${label}-${index}`}>
              <input
                aria-label={`${label}第${index + 1}行键名`}
                value={row.key}
                onChange={/* 当前回调更新键值行的字段名称。 */ (event: ChangeEvent<HTMLInputElement>) => updateRow(index, 'key', event.target.value)}
                placeholder={index === 0 ? placeholder : '键名'}
                className="min-w-0 border-0 bg-transparent px-1 py-2 font-mono text-sm text-gray-800 outline-none ring-0 placeholder:text-gray-300"
              />
              <SensitiveValue key={row.key} label={`${label}第${index + 1}行值`} fieldName={row.key} value={row.value}
                onChange={/* updateValue 将字段值更新回对应的键值行。 */ nextValue => updateRow(index, 'value', nextValue)} />
              <button
                type="button"
                onClick={/* 当前回调删除指定键值编辑行。 */ () => removeRow(index)}
                className="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-400 transition-colors hover:bg-red-50 hover:text-red-500"
                title={`删除${label}第${index + 1}行`}
                aria-label={`删除${label}第${index + 1}行`}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};

// APIRequestBuilder 渲染 API 地址、元数据以及 Headers / Params / Body 三个请求区块。
export const APIRequestBuilder = ({
  url,
  method,
  timeout,
  headers,
  params,
  contentType,
  body,
  responsePath,
  retryEnabled,
  savedKeys,
  templateActions,
	onChange,
	onTest,
}: APIRequestBuilderProps) => {
  // jsonBody 表示当前正文类型是否使用 JSON 文本编辑器。
	const jsonBody = isJSONContentType(contentType);
	// testState 保存最近一次 API 测试的加载、结果和错误状态。
		const [testState, setTestState] = useState<APITestState>({ loading: false });
	// curlImport 保存当前弹窗的 cURL 粘贴草稿与本地解析结果，不会持久化未确认命令。
	const [curlImport, setCurlImport] = useState<CurlImportState>({ command: '' });
  // testRequest 标识当前测试，配置变更或关闭弹窗时取消旧结果。
  const testRequest = useRef<AbortController | null>(null);
  // resetTest 在配置变化时撤销旧请求，避免展示不对应当前草稿的结果。
  useEffect(/* testLifecycle 绑定测试结果和当前配置。 */ () => {
    testRequest.current?.abort();
    testRequest.current = null;
    setTestState({ loading: false });
    return /* abortTest 取消配置变化或组件卸载后的请求。 */ () => testRequest.current?.abort();
  }, [url, method, timeout, headers, params, contentType, body, responsePath, retryEnabled, templateActions?.headers, templateActions?.params, templateActions?.body]);
  // runTest 仅展示本次请求的结果，不接受关闭或修改表单后的迟到响应。
  const runTest = async () => {
    if (!onTest) return;
    testRequest.current?.abort();
    // request 区分当前测试与已经失效的旧测试。
    const request = new AbortController();
    testRequest.current = request;
    setTestState({ loading: true });
    try {
      // result 是当前草稿的测试诊断结果。
      const result = await onTest(request.signal);
      if (!request.signal.aborted) setTestState({ loading: false, result });
    } catch (/* error 表示当前测试的非敏感失败提示。 */ error) {
      if (!request.signal.aborted) setTestState({ loading: false, error: error instanceof Error ? error.message : 'API 测试请求失败' });
    }
  };

	// importCurl 由用户点击导入触发，只在全部字段成功解析后才批量覆盖当前 API 草稿。
	const importCurl = () => {
		try {
			// config 保存解析得到的受支持 API 请求字段。
			const config = parseCurlCommand(curlImport.command);
			onChange('url', config.url);
			onChange('method', config.method);
			onChange('headers', config.headers);
			onChange('params', config.params);
			onChange('contentType', config.contentType);
			onChange('body', config.body);
			setCurlImport({ command: '' });
		} catch (/* error 保存 cURL 解析失败原因；命令只继续保留在当前本地草稿中。 */ error: unknown) {
			setCurlImport(/* previous 保留用户粘贴文本以便修正后再次导入。 */ previous => ({
				...previous,
				error: error instanceof CurlImportError || error instanceof Error ? error.message : 'cURL 解析失败',
			}));
		}
	};

  return (
    <div className="space-y-5 rounded-2xl border border-blue-100 bg-blue-50/40 p-4 sm:p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="font-bold text-gray-900">API 请求配置</h3>
          <p className="mt-1 text-xs text-gray-500">请求头、参数和正文加密保存在服务端，保存后不再回显；修改时请重新填写。</p>
        </div>
		<span className="rounded-full bg-white px-2.5 py-1 text-[11px] font-bold text-blue-600 shadow-sm">API</span>
	      </div>
	      {onTest && (
		<div className="flex flex-wrap items-center gap-3">
		  <button type="button" onClick={runTest} disabled={testState.loading} className="inline-flex items-center gap-2 rounded-xl bg-gray-900 px-4 py-2.5 text-sm font-bold text-white transition-colors hover:bg-gray-700 disabled:cursor-wait disabled:opacity-60">
			{testState.loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
			测试请求
		  </button>
		  {testState.error && <span className="text-sm font-semibold text-red-600">{testState.error}</span>}
		  {testState.result && (
			<div className={`w-full rounded-xl border p-3 text-sm ${testState.result.status === 'success' ? 'border-green-200 bg-green-50' : 'border-red-200 bg-red-50'}`}>
			  <div className="flex flex-wrap gap-x-5 gap-y-1 font-semibold">
				<span>{testState.result.status === 'success' ? '测试成功' : '测试失败'}</span>
				<span>HTTP 状态：{testState.result.status_code || '网络错误'}</span>
				<span>响应类型：{testState.result.response_content_type || '未知'}</span>
			  </div>
			  <div className="mt-2 grid gap-1 text-gray-700"><span>响应字段：{testState.result.response_fields?.join('、') || '未识别 JSON 字段'}</span><span>提取结果：{testState.result.extracted_value || '未提取到内容'}</span></div>
			  {testState.result.response_preview && <pre className="mt-2 max-h-32 overflow-auto whitespace-pre-wrap rounded-lg bg-white/70 p-2 text-xs text-gray-600">{testState.result.response_preview}</pre>}
			</div>
		  )}
		</div>
	      )}

      <div className="space-y-2 rounded-xl border border-dashed border-blue-200 bg-white/70 p-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <label className="text-sm font-bold text-gray-800" htmlFor="api-curl-import">从 cURL 导入</label>
          <button type="button" onClick={importCurl} className="rounded-lg bg-blue-600 px-3 py-1.5 text-xs font-bold text-white transition-colors hover:bg-blue-700">解析并填充</button>
        </div>
        <textarea
          id="api-curl-import"
          aria-label="cURL 命令"
          value={curlImport.command}
          onChange={/* 当前回调只更新未持久化的 cURL 粘贴草稿，并清除上一次解析错误。 */ (event: ChangeEvent<HTMLTextAreaElement>) => setCurlImport({ command: event.target.value })}
          className="h-24 w-full resize-y ios-input rounded-xl px-3 py-2 font-mono text-xs"
          placeholder={'curl -X POST \'https://api.example.com/code\' -H \'Content-Type: application/json\' -d \'{"order_id":"{order_id}"}\''}
        />
        <p className="text-xs text-gray-500">支持 GET/POST、URL 参数、Headers、JSON 和表单正文；原始 XML、纯文本和文件上传需手动填写。导入仅在本地解析，需保存后才会写入卡密配置。</p>
        {curlImport.error && <p role="alert" className="text-xs font-semibold text-red-600">{curlImport.error}</p>}
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-[minmax(0,1fr)_130px_130px]">
        <div className="space-y-2">
          <label className="block text-sm font-bold text-gray-700">API 地址</label>
          <input
            type="url"
            value={url}
            onChange={/* 当前回调更新远端 API 地址。 */ (event: ChangeEvent<HTMLInputElement>) => onChange('url', event.target.value)}
            className="w-full ios-input rounded-xl px-4 py-3 font-mono text-sm"
            placeholder="https://api.example.com/get-code"
          />
        </div>
        <div className="space-y-2">
          <label className="block text-sm font-bold text-gray-700">请求方法</label>
          <select value={method} onChange={/* 当前回调切换 API 请求方法。 */ (event: ChangeEvent<HTMLSelectElement>) => onChange('method', event.target.value)} className="w-full ios-input rounded-xl px-3 py-3">
            <option value="GET">GET</option>
            <option value="POST">POST</option>
          </select>
        </div>
        <div className="space-y-2">
          <label className="block text-sm font-bold text-gray-700">超时（秒）</label>
          <input type="number" min="1" max="60" value={timeout} onChange={/* 当前回调更新 API 请求超时秒数。 */ (event: ChangeEvent<HTMLInputElement>) => onChange('timeout', Number(event.target.value) || 10)} className="w-full ios-input rounded-xl px-3 py-3" />
        </div>
      </div>

      <div className="space-y-5 border-t border-blue-100 pt-5">
        <SavedTemplateSection label="请求头" keys={savedKeys?.headers} action={templateActions?.headers} onAction={/* headersAction 切换请求头模板处理方式。 */ action => onChange('headersAction', action)}>
          <KeyValueEditor label="Headers / 请求头" value={headers} onChange={/* 当前回调保存请求头键值对象。 */ nextValue => onChange('headers', nextValue)} placeholder="Authorization" />
        </SavedTemplateSection>

        <SavedTemplateSection label="查询参数" keys={savedKeys?.params} action={templateActions?.params} onAction={/* paramsAction 切换查询参数模板处理方式。 */ action => onChange('paramsAction', action)}>
          <KeyValueEditor label="Params / 查询参数" value={params} onChange={/* 当前回调保存查询参数键值对象。 */ nextValue => onChange('params', nextValue)} placeholder="order_id" />
        </SavedTemplateSection>

        <div className="space-y-2">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <label className="text-sm font-bold text-gray-800">Body / 请求正文</label>
            <select aria-label="请求正文 Content-Type" value={contentType} onChange={/* 当前回调切换请求正文类型。 */ (event: ChangeEvent<HTMLSelectElement>) => onChange('contentType', event.target.value)} className="ios-input rounded-xl px-3 py-2 text-sm sm:w-[290px]">
              {contentTypes.map(/* 当前回调渲染一个常用 Content-Type 选项。 */ contentTypeOption => <option key={contentTypeOption.value} value={contentTypeOption.value}>{contentTypeOption.label}</option>)}
            </select>
          </div>
          <SavedTemplateSection label="请求正文" keys={savedKeys?.body} action={templateActions?.body} onAction={/* bodyAction 切换请求正文模板处理方式。 */ action => onChange('bodyAction', action)}>
          {jsonBody ? (
            <textarea
              aria-label="JSON 请求正文"
              value={body}
              onChange={/* 当前回调更新 JSON 请求正文。 */ (event: ChangeEvent<HTMLTextAreaElement>) => onChange('body', event.target.value)}
              className="h-36 w-full resize-y ios-input rounded-xl px-4 py-3 font-mono text-sm"
              placeholder={'{\n  "order_id": "{order_id}"\n}'}
            />
          ) : (
            <KeyValueEditor label="Body 字段" value={body} onChange={/* 当前回调保存非 JSON 正文键值对象。 */ nextValue => onChange('body', nextValue)} placeholder="field" />
          )}
          </SavedTemplateSection>
          <p className="text-xs text-gray-500">GET 请求通常只使用 Params；POST 请求会按所选 Content-Type 发送 Body。</p>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 border-t border-blue-100 pt-5 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <div className="space-y-2">
          <label className="block text-sm font-bold text-gray-700">响应提取路径（可选）</label>
          <input value={responsePath} onChange={/* 当前回调更新响应提取路径。 */ (event: ChangeEvent<HTMLInputElement>) => onChange('responsePath', event.target.value)} className="w-full ios-input rounded-xl px-4 py-3 font-mono text-sm" placeholder="data.cards[0].code" />
        </div>
        <label className="flex items-center gap-2 self-end pb-3 text-sm font-bold text-gray-700">
          <input type="checkbox" checked={retryEnabled} onChange={/* 当前回调切换 API 幂等重试。 */ (event: ChangeEvent<HTMLInputElement>) => onChange('retryEnabled', event.target.checked)} />
          启用幂等重试（需配置 {'{idempotency_key}'}）
        </label>
      </div>
    </div>
  );
};
