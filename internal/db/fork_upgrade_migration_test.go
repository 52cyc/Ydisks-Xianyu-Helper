package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMigrate_UpgradesReleasedFork56 验证持有 fork 已发布账本与业务数据的旧库可接续三项上游迁移。
// t 提供隔离目录及失败报告；测试仅操作临时 SQLite，不访问用户数据库或真实凭证。
func TestMigrate_UpgradesReleasedFork56(t *testing.T) {
	// database、openErr 保存只执行到旧 fork 版本的临时数据库及打开错误。
	database, openErr := sql.Open("sqlite", sqliteDSN(filepath.Join(t.TempDir(), "fork56.db")))
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer database.Close()
	// dialectErr 保存旧账本构造所需的 SQLite 方言配置错误。
	if dialectErr := goose.SetDialect("sqlite3"); dialectErr != nil {
		t.Fatal(dialectErr)
	}
	goose.SetBaseFS(migrationsFS)
	// upErr 保存旧 fork 全量 schema 及真实 Goose 账本初始化错误。
	if upErr := goose.UpTo(database, "migrations/sqlite", 56); upErr != nil {
		t.Fatal(upErr)
	}
	// initialVersion、versionErr 确保准备的是已发布的 56，而非空库或包含新字段的最新库。
	initialVersion, versionErr := goose.GetDBVersion(database)
	if versionErr != nil || initialVersion != 56 {
		t.Fatalf("legacy migration version=%d err=%v", initialVersion, versionErr)
	}
	if columnExists(t, database, "ai_reply_settings", "ai_mode") || tableExists(t, database, "item_ai_settings") || columnExists(t, database, "keywords", "keyword_expressions") {
		t.Fatal("fork 56 must not already contain incoming upstream schema")
	}
	// seedStatements 构造升级前已有的账号开关、履约订单、跟价报价、消息去重及免拼阶段，不含真实秘密。
	seedStatements := []string{
		`INSERT INTO users (id,username,email,password_hash) VALUES (901,'fork-upgrade','fork-upgrade@example.invalid','test-only')`,
		`INSERT INTO cookies (id,value,user_id,auto_consign,auto_bargain) VALUES ('fork-account','',901,1,1)`,
		`INSERT INTO ai_reply_settings (cookie_id) VALUES ('fork-account')`,
		`INSERT INTO keywords (cookie_id,keyword,reply) VALUES ('fork-account','历史关键词','历史回复')`,
		`INSERT INTO fulfillment_instances (id,public_id,user_id,name,base_url,merchant_user_id,api_key) VALUES (901,'fork-instance',901,'test','https://example.invalid','test','')`,
		`INSERT INTO fulfillment_mappings (user_id,account_id,item_id,instance_id,remote_goods_id,goods_type) VALUES (901,'fork-account','fork-item',901,77,1)`,
		`INSERT INTO fulfillment_orders (user_id,instance_id,external_order_no,remote_order_no,remote_goods_id,quantity,state) VALUES (901,901,'fork-order','remote-order',77,2,'waiting')`,
		`INSERT INTO external_price_quotes (order_id,cookie_id,action_id,unit_cost_cents,fulfillment_quantity,fixed_markup_cents,minimum_profit_cents,target_order_cents,dynamic_safe_price) VALUES ('fork-order','fork-account',901,100,2,30,20,230,'2.30')`,
		`INSERT INTO external_price_message_records (dedupe_key,cookie_id,chat_id,rule_id,message_kind) VALUES ('fork-dedupe','fork-account','fork-chat',901,'price')`,
		`INSERT INTO bargain_free_shipping_stages (order_id,cookie_id,status,updated_at) VALUES ('fork-order','fork-account','success','2026-10-20T00:00:00Z')`,
	}
	// statement 是当前需要写入旧库的业务事实，失败时中止而不掩盖缺失字段。
	for _, statement := range seedStatements {
		// seedErr 保存当前旧数据写入错误。
		if _, seedErr := database.Exec(statement); seedErr != nil {
			t.Fatal(seedErr)
		}
	}
	// migrateErr 保存正式迁移入口从已有 fork56 升级到最新版本的错误。
	if migrateErr := Migrate(context.Background(), database, DialectSQLite); migrateErr != nil {
		t.Fatal(migrateErr)
	}
	// assertions 同时验证新 schema 默认值、三项账本和所有旧业务事实仍可读。
	assertions := []struct {
		// query 是返回一个整数的只读验证，want 是应保留或产生的行数。
		query string
		want  int
	}{
		{`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied=1`, 59},
		{`SELECT COUNT(*) FROM goose_db_version WHERE version_id BETWEEN 57 AND 59 AND is_applied=1`, 3},
		{`SELECT COUNT(*) FROM ai_reply_settings WHERE cookie_id='fork-account' AND ai_mode='bargain_only'`, 1},
		{`SELECT COUNT(*) FROM keywords WHERE cookie_id='fork-account' AND keyword='历史关键词' AND reply='历史回复' AND match_type='contains' AND keyword_expressions IS NULL`, 1},
		{`SELECT COUNT(*) FROM cookies WHERE id='fork-account' AND auto_consign=1 AND auto_bargain=1`, 1},
		{`SELECT COUNT(*) FROM fulfillment_mappings WHERE account_id='fork-account' AND remote_goods_id=77`, 1},
		{`SELECT COUNT(*) FROM fulfillment_orders WHERE external_order_no='fork-order' AND remote_order_no='remote-order' AND quantity=2 AND state='waiting'`, 1},
		{`SELECT COUNT(*) FROM external_price_quotes WHERE order_id='fork-order' AND target_order_cents=230`, 1},
		{`SELECT COUNT(*) FROM external_price_message_records WHERE dedupe_key='fork-dedupe'`, 1},
		{`SELECT COUNT(*) FROM bargain_free_shipping_stages WHERE order_id='fork-order' AND status='success'`, 1},
	}
	// assertion 是当前新旧 schema 或迁移账本的单项验证。
	for _, assertion := range assertions {
		// actual 保存数据库实际返回的版本或匹配行数。
		var actual int
		// scanErr 保存升级后验证查询错误，包含字段缺失或旧数据不可读。
		if scanErr := database.QueryRow(assertion.query).Scan(&actual); scanErr != nil || actual != assertion.want {
			t.Fatalf("upgrade assertion %q: got=%d want=%d err=%v", assertion.query, actual, assertion.want, scanErr)
		}
	}
	// itemInsertErr 验证商品 AI 新表真实可写，不能只验证迁移版本号。
	if _, itemInsertErr := database.Exec(`INSERT INTO item_ai_settings (cookie_id,item_id,ai_override,item_context) VALUES ('fork-account','fork-item','enabled','专属商品资料')`); itemInsertErr != nil {
		t.Fatal(itemInsertErr)
	}
	// itemCount 保存新商品配置写入后的读回行数。
	var itemCount int
	// itemReadErr 保存新表与字段读回的错误。
	if itemReadErr := database.QueryRow(`SELECT COUNT(*) FROM item_ai_settings WHERE cookie_id='fork-account' AND item_id='fork-item' AND ai_override='enabled' AND item_context='专属商品资料'`).Scan(&itemCount); itemReadErr != nil || itemCount != 1 {
		t.Fatalf("item AI migration readback count=%d err=%v", itemCount, itemReadErr)
	}
}
