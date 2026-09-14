package adapter

import (
	"errors"
	"testing"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
	"xianyu-go/internal/automation"
)

// TestExternalProductQuoteErrorDistinguishesDeletedProduct 验证只有远程商品删除会转换为自动化保护价哨兵。
func TestExternalProductQuoteErrorDistinguishesDeletedProduct(t *testing.T) {
	// productMissing 是货源应用层已经确认的商品删除错误。
	productMissing := errors.Join(fulfillmentapp.ErrProductNotFound, errors.New("商品不存在"))
	if !errors.Is(externalProductQuoteError(productMissing), automation.ErrExternalProductNotFound) {
		t.Fatal("商品删除错误没有转换为自动化保护价哨兵")
	}
	// instanceMissing 是实例或归属不存在错误，不得触发商品保护价。
	instanceMissing := fulfillmentapp.ErrNotFound
	if errors.Is(externalProductQuoteError(instanceMissing), automation.ErrExternalProductNotFound) {
		t.Fatal("实例不存在被错误转换为商品删除")
	}
}
