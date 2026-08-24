package adapter

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
	"xianyu-go/internal/db"
)

// TestFulfillmentRepositoryEncryptsSecretsAndKeepsPurchaseIdempotent 验证密钥、卡密静态加密和外部单号幂等。
func TestFulfillmentRepositoryEncryptsSecretsAndKeepsPurchaseIdempotent(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "fulfillment-test-key")
	// ctx 是测试数据库和仓储操作共用的上下文。
	ctx := context.Background()
	// database 是已执行全量迁移的临时 SQLite 数据库。
	// database、dialect 和 openErr 是临时数据库、方言及打开错误。
	database, dialect, openErr := db.Open(ctx, filepath.Join(t.TempDir(), "fulfillment.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer database.Close()
	if // createUserErr 是测试用户创建错误。
	_, createUserErr := database.ExecContext(ctx, `INSERT INTO users(username,email,password_hash,is_admin) VALUES('owner','owner@example.com','hash',0)`); createUserErr != nil {
		t.Fatal(createUserErr)
	}
	// store 是启用数据密钥的数据库聚合入口。
	store := db.NewStore(database, dialect)
	// repository 是本测试使用的履约持久化适配器。
	repository := NewFulfillmentRepository(store)
	// instance 是含有测试 API 密钥的新货源实例。
	// instance 和 createInstanceErr 是新货源实例及创建错误。
	instance, createInstanceErr := repository.CreateInstance(ctx, 1, fulfillmentapp.InstanceInput{Name: "智客", Provider: fulfillmentapp.ProviderKasushouV2, BaseURL: "https://supplier.example", MerchantUserID: "u1", APIKey: "plain-api-key", Enabled: true})
	if createInstanceErr != nil || !instance.HasAPIKey || instance.APIKey != "" {
		t.Fatalf("instance=%+v err=%v", instance, createInstanceErr)
	}
	// storedAPIKey 是数据库中保存的实例密钥密文。
	var storedAPIKey string
	if // readKeyErr 是数据库密钥密文读取错误。
	readKeyErr := database.QueryRowContext(ctx, `SELECT api_key FROM fulfillment_instances WHERE id=?`, instance.ID).Scan(&storedAPIKey); readKeyErr != nil {
		t.Fatal(readKeyErr)
	}
	if storedAPIKey == "plain-api-key" || !strings.HasPrefix(storedAPIKey, "enc:v1:") {
		t.Fatalf("api key was not encrypted: %q", storedAPIKey)
	}
	// secretInstance 是只为外部请求解密的短时实例视图。
	// secretInstance 和 secretReadErr 是短时密钥视图及读取错误。
	secretInstance, secretReadErr := repository.GetInstance(ctx, 1, instance.ID, true)
	if secretReadErr != nil || secretInstance.APIKey != "plain-api-key" {
		t.Fatalf("secret instance=%+v err=%v", secretInstance, secretReadErr)
	}
	// request 是具有稳定外部单号的采购请求。
	request := fulfillmentapp.PurchaseRequest{InstanceID: instance.ID, ExternalOrderNo: "XY-ORDER-1", XianyuOrderID: "XY1", RemoteGoodsID: 88, Quantity: 1}
	// firstOrder 是第一次请求创建的本地记录。
	firstOrder, created, err := repository.CreateOrder(ctx, 1, request)
	if err != nil || !created {
		t.Fatalf("first order=%+v created=%v err=%v", firstOrder, created, err)
	}
	// duplicateOrder 是同一外部单号重试返回的原记录。
	duplicateOrder, created, err := repository.CreateOrder(ctx, 1, request)
	if err != nil || created || duplicateOrder.ID != firstOrder.ID {
		t.Fatalf("duplicate=%+v created=%v err=%v", duplicateOrder, created, err)
	}
	// completed 是应用远程成功状态后解密返回的订单。
	// completed 和 applyErr 是远程状态应用后订单及更新错误。
	completed, applyErr := repository.ApplyRemoteOrder(ctx, 1, instance.ID, request.ExternalOrderNo, fulfillmentapp.RemoteOrder{RemoteOrderNo: "KSS-1", ExternalOrderNo: request.ExternalOrderNo, Status: 3, TotalPrice: "9.90", CardList: []string{"CARD-SECRET"}})
	if applyErr != nil || completed.State != "succeeded" || len(completed.CardList) != 1 || completed.CardList[0] != "CARD-SECRET" {
		t.Fatalf("completed=%+v err=%v", completed, applyErr)
	}
	// storedResult 是数据库中保存的履约结果密文。
	var storedResult string
	if // readResultErr 是数据库履约结果密文读取错误。
	readResultErr := database.QueryRowContext(ctx, `SELECT result_secret FROM fulfillment_orders WHERE id=?`, firstOrder.ID).Scan(&storedResult); readResultErr != nil {
		t.Fatal(readResultErr)
	}
	if strings.Contains(storedResult, "CARD-SECRET") || !strings.HasPrefix(storedResult, "enc:v1:") {
		t.Fatalf("result was not encrypted: %q", storedResult)
	}
}
