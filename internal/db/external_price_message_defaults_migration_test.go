package db

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// TestExternalPriceMessageDefaultsMigration 验证历史外部跟价规则统一开启询价和改价成功通知，其他规则保持不变。
func TestExternalPriceMessageDefaultsMigration(t *testing.T) {
	// database 是从上一个迁移版本开始升级的独立 SQLite 数据库。
	database, openErr := sql.Open("sqlite", "file:external-price-message-defaults?mode=memory&cache=shared")
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer database.Close()
	if dialectErr := goose.SetDialect("sqlite3"); dialectErr != nil { // dialectErr 是测试迁移使用的 Goose 方言设置错误。
		t.Fatal(dialectErr)
	}
	goose.SetBaseFS(migrationsFS)
	if upErr := goose.UpTo(database, "migrations/sqlite", 51); upErr != nil { // upErr 是历史基线模式的初始化错误。
		t.Fatal(upErr)
	}
	// userResult、userErr 是历史规则所属测试用户的写入结果。
	userResult, userErr := database.Exec(`INSERT INTO users(username,email,password_hash) VALUES('message-default-user','message-default@example.invalid','test-only')`)
	if userErr != nil {
		t.Fatal(userErr)
	}
	// userID 是测试用户主键。
	userID, idErr := userResult.LastInsertId()
	if idErr != nil {
		t.Fatal(idErr)
	}
	if _, cookieErr := database.Exec(`INSERT INTO cookies(id,value,user_id) VALUES('message-default-account','',?)`, userID); cookieErr != nil { // cookieErr 是测试账号写入错误。
		t.Fatal(cookieErr)
	}
	// eligibleResult、eligibleErr 是应强制开启通知的历史外部跟价规则。
	eligibleResult, eligibleErr := database.Exec(`INSERT INTO automation_rules(user_id,cookie_id,item_id,name,trigger_type,enabled,priority,config_json) VALUES(?,'message-default-account','eligible','外部跟价','order_paid',1,100,'{"price_guidance_enabled":false,"existing":"keep"}')`, userID)
	if eligibleErr != nil {
		t.Fatal(eligibleErr)
	}
	// eligibleID 是应被迁移规则的主键。
	eligibleID, eligibleIDErr := eligibleResult.LastInsertId()
	if eligibleIDErr != nil {
		t.Fatal(eligibleIDErr)
	}
	if _, actionErr := database.Exec(`INSERT INTO automation_rule_actions(rule_id,action_type,config_json,enabled,sort_order) VALUES(?,'send_card','{"source_type":"external","price_sync_enabled":true}',1,1)`, eligibleID); actionErr != nil { // actionErr 是外部跟价动作写入错误。
		t.Fatal(actionErr)
	}
	// localResult、localErr 是不应改动的本地库存规则。
	localResult, localErr := database.Exec(`INSERT INTO automation_rules(user_id,cookie_id,item_id,name,trigger_type,enabled,priority,config_json) VALUES(?,'message-default-account','local','本地库存','order_paid',1,100,'{"price_guidance_enabled":false}')`, userID)
	if localErr != nil {
		t.Fatal(localErr)
	}
	// localID 是不应被迁移规则的主键。
	localID, localIDErr := localResult.LastInsertId()
	if localIDErr != nil {
		t.Fatal(localIDErr)
	}
	if _, actionErr := database.Exec(`INSERT INTO automation_rule_actions(rule_id,action_type,config_json,enabled,sort_order) VALUES(?,'send_card','{"source_type":"local"}',1,1)`, localID); actionErr != nil { // actionErr 是本地库存动作写入错误。
		t.Fatal(actionErr)
	}
	if upErr := goose.UpTo(database, "migrations/sqlite", 52); upErr != nil { // upErr 是应用通知默认值迁移的错误。
		t.Fatal(upErr)
	}
	// eligibleRaw 是迁移后外部跟价规则的完整扩展配置。
	var eligibleRaw string
	if scanErr := database.QueryRow(`SELECT config_json FROM automation_rules WHERE id=?`, eligibleID).Scan(&eligibleRaw); scanErr != nil { // scanErr 是读取迁移结果的错误。
		t.Fatal(scanErr)
	}
	// eligibleConfig 用字段语义验证开关和历史配置。
	var eligibleConfig map[string]any
	if decodeErr := json.Unmarshal([]byte(eligibleRaw), &eligibleConfig); decodeErr != nil { // decodeErr 是解析迁移结果的错误。
		t.Fatal(decodeErr)
	}
	if eligibleConfig["price_guidance_enabled"] != true || eligibleConfig["price_adjusted_notice_enabled"] != true || eligibleConfig["existing"] != "keep" {
		t.Fatalf("历史外部跟价规则迁移错误: %#v", eligibleConfig)
	}
	// localRaw 是迁移后应保持不变的本地库存规则配置。
	var localRaw string
	if scanErr := database.QueryRow(`SELECT config_json FROM automation_rules WHERE id=?`, localID).Scan(&localRaw); scanErr != nil || localRaw != `{"price_guidance_enabled":false}` { // scanErr 是读取非目标规则的错误。
		t.Fatalf("非外部跟价规则不应改动: config=%s err=%v", localRaw, scanErr)
	}
}
