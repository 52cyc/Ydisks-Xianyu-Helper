package items

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	// MaxLinkImportSources 限制一次采集的非空分享文本数量，避免单个请求长时间占用账号平台会话。
	MaxLinkImportSources = 50
)

var (
	// ErrLinkImportInvalidUser 表示请求没有可用于账号归属校验的用户标识。
	ErrLinkImportInvalidUser = errors.New("用户身份无效")
	// ErrLinkImportInvalidAccount 表示请求没有选择用于采集详情和后续发布的自有账号。
	ErrLinkImportInvalidAccount = errors.New("请选择采集账号")
	// ErrLinkImportNoSources 表示请求没有提供任何非空分享文本或链接。
	ErrLinkImportNoSources = errors.New("请至少填写一个闲鱼分享链接")
	// ErrLinkImportTooManySources 表示一次请求超过允许的批量采集数量。
	ErrLinkImportTooManySources = errors.New("一次最多采集 50 个商品")
)

// LinkImportResolvedSource 是分享文本经安全解析后的闲鱼商品定位，不包含账号凭证或分享跟踪参数。
type LinkImportResolvedSource struct {
	// ItemID 是闲鱼商品的数字标识，用于调用平台详情接口。
	ItemID string
	// ItemURL 是去除分享跟踪参数后的标准商品地址，供用户核对采集来源。
	ItemURL string
}

// LinkImportCollectedItem 是平台详情转换后的可发布商品快照，不包含卖家身份和敏感会话信息。
type LinkImportCollectedItem struct {
	// Title 是源商品标题，批量预检会再次执行平台发布文本清洗。
	Title string
	// Description 是源商品描述；平台未提供时允许应用层回退为标题。
	Description string
	// Price 是十进制元金额文本，不含货币符号。
	Price string
	// Images 是按源商品顺序保留的公网图片地址，最多九张。
	Images []string
	// IsMultiSpec 表示源商品含多个规格，首版外链采集禁止把它降级成错误的单规格商品。
	IsMultiSpec bool
}

// LinkImportRow 是批量采集的一条独立结果；Error 非空时其他商品仍可继续采集。
type LinkImportRow struct {
	// RowNo 是去除空白输入后的从一开始序号，用于前端定位原始分享文本。
	RowNo int
	// Source 是用户提交的单条分享文本，响应仅回显该用户已经提供的内容。
	Source string
	// ItemID 是解析成功后的闲鱼商品标识；短链解析失败时为空。
	ItemID string
	// ItemURL 是标准闲鱼详情地址，不携带分享人和跟踪参数。
	ItemURL string
	// Title 是采集成功后的商品标题。
	Title string
	// Description 是采集成功后的商品描述。
	Description string
	// Price 是采集成功后的元金额文本。
	Price string
	// Images 是采集成功后的图片地址集合。
	Images []string
	// Error 是当前条目的安全失败说明；不会包含 Cookie、签名或平台原始响应正文。
	Error string
}

// LinkImportResult 汇总一次批量采集的成功和失败条目。
type LinkImportResult struct {
	// Total 是参与采集的非空分享文本数量。
	Total int
	// Collected 是具备标题、价格和图片并可进入批量预检的条目数量。
	Collected int
	// Failed 是解析、详情读取或首版能力校验失败的条目数量。
	Failed int
	// Rows 按用户输入顺序保存逐条结果。
	Rows []LinkImportRow
}

// LinkImportInput 是批量链接采集用例的用户和账号范围输入。
type LinkImportInput struct {
	// UserID 是当前认证用户标识，用于服务端复核目标账号归属。
	UserID int64
	// CookieID 是用户选择的自有闲鱼账号标识，凭证明文不会进入本模型。
	CookieID string
	// Sources 是逐条分享文本；每条可以是纯链接或闲鱼复制出的完整分享文案。
	Sources []string
}

// LinkImportResolverPort 定义应用层解析受支持分享文本所需的最小基础设施能力。
type LinkImportResolverPort interface {
	// Resolve 返回标准商品标识和地址；实现必须限制协议、域名、响应大小和重定向。
	Resolve(context.Context, string) (LinkImportResolvedSource, error)
}

// LinkImportCollectorPort 定义使用已归属账号读取公开商品详情的最小平台能力。
type LinkImportCollectorPort interface {
	// Collect 复核 userID 与 cookieID 的归属后读取 itemID；不得向返回值或错误暴露凭证明文。
	Collect(context.Context, int64, string, string) (LinkImportCollectedItem, error)
}

// LinkImportService 顺序编排短链解析和商品详情采集，避免同一账号产生并发平台请求。
type LinkImportService struct {
	// resolver 负责把用户分享文本收敛为可信商品标识。
	resolver LinkImportResolverPort
	// collector 负责凭证受控的闲鱼商品详情读取。
	collector LinkImportCollectorPort
}

// NewLinkImportService 构造批量链接采集服务；resolver 和 collector 均为必需依赖。
func NewLinkImportService(resolver LinkImportResolverPort, collector LinkImportCollectorPort) (*LinkImportService, error) {
	if resolver == nil || collector == nil {
		return nil, errors.New("商品链接采集依赖未初始化")
	}
	return &LinkImportService{resolver: resolver, collector: collector}, nil
}

// CollectBatch 顺序处理最多五十条分享文本，返回逐条成功或失败结果而不因单条异常中断整批。
func (service *LinkImportService) CollectBatch(ctx context.Context, input LinkImportInput) (LinkImportResult, error) {
	if service == nil || service.resolver == nil || service.collector == nil {
		return LinkImportResult{}, errors.New("商品链接采集服务未初始化")
	}
	if input.UserID <= 0 {
		return LinkImportResult{}, ErrLinkImportInvalidUser
	}
	// cookieID 是去除首尾空白后的目标账号标识，仅用于归属复核和平台详情请求。
	cookieID := strings.TrimSpace(input.CookieID)
	if cookieID == "" {
		return LinkImportResult{}, ErrLinkImportInvalidAccount
	}
	// sources 去除空行但保留每条完整分享文案，避免用户批量粘贴时产生无意义失败项。
	sources := make([]string, 0, len(input.Sources))
	// rawSource 表示当前待清理的用户分享文本。
	for _, rawSource := range input.Sources {
		// source 是保留内部标题和链接信息的去首尾空白文本。
		source := strings.TrimSpace(rawSource)
		if source != "" {
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		return LinkImportResult{}, ErrLinkImportNoSources
	}
	if len(sources) > MaxLinkImportSources {
		return LinkImportResult{}, ErrLinkImportTooManySources
	}

	// result 保存与用户输入顺序一致的逐条采集结果。
	result := LinkImportResult{Total: len(sources), Rows: make([]LinkImportRow, 0, len(sources))}
	// firstRows 记录已解析商品首次出现的行号，防止同一商品在一次批次中被重复上架。
	firstRows := make(map[string]int, len(sources))
	// sourceIndex 和 source 分别是当前非空输入的零基下标与分享文本。
	for sourceIndex, source := range sources {
		if // cancelErr 表示调用方在当前条目开始前已经取消整批采集。
		cancelErr := ctx.Err(); cancelErr != nil {
			return LinkImportResult{}, cancelErr
		}
		// row 保存当前分享文本的独立处理结果。
		row := LinkImportRow{RowNo: sourceIndex + 1, Source: source}
		// resolved 和 resolveErr 保存短链或标准链接解析结果。
		resolved, resolveErr := service.resolver.Resolve(ctx, source)
		if resolveErr != nil {
			row.Error = resolveErr.Error()
			result.Failed++
			result.Rows = append(result.Rows, row)
			continue
		}
		row.ItemID, row.ItemURL = resolved.ItemID, resolved.ItemURL
		if // firstRow 和 duplicate 表示该商品是否已经在当前请求中成功解析过。
		firstRow, duplicate := firstRows[resolved.ItemID]; duplicate {
			row.Error = fmt.Sprintf("与第 %d 条是同一个商品，已跳过重复采集", firstRow)
			result.Failed++
			result.Rows = append(result.Rows, row)
			continue
		}
		firstRows[resolved.ItemID] = row.RowNo

		// collected 和 collectErr 保存平台详情读取及归一化结果。
		collected, collectErr := service.collector.Collect(ctx, input.UserID, cookieID, resolved.ItemID)
		if collectErr != nil {
			row.Error = collectErr.Error()
			result.Failed++
			result.Rows = append(result.Rows, row)
			continue
		}
		if collected.IsMultiSpec {
			row.Error = "该商品包含多规格，当前批量采集暂不支持，请手工确认规格后发布"
			result.Failed++
			result.Rows = append(result.Rows, row)
			continue
		}
		row.Title = strings.TrimSpace(collected.Title)
		row.Description = strings.TrimSpace(collected.Description)
		if row.Description == "" {
			row.Description = row.Title
		}
		row.Price = strings.TrimSpace(collected.Price)
		row.Images = append([]string(nil), collected.Images...)
		result.Collected++
		result.Rows = append(result.Rows, row)
	}
	result.Failed = result.Total - result.Collected
	return result, nil
}
