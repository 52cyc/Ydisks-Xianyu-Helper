package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// encryptedValuePrefix 用于本次流程后续判断的encrypted值Prefix
const (
	encryptedValuePrefix = "enc:v1:"
	cookieMetadataScope  = "cookie-metadata"
	cardAPIConfigScope   = "card-api-config"
	// fulfillmentAPIKeyScope 隔离外部货源实例 API 密钥的加密用途。
	fulfillmentAPIKeyScope = "fulfillment-api-key"
	// fulfillmentResultScope 隔离卡密和直充结果的加密用途。
	fulfillmentResultScope = "fulfillment-result"
	// rechargeChatInputScope 隔离买家在聊天中提供的直充账号。
	rechargeChatInputScope = "recharge-chat-input"
)

// secretCodec 对数据库敏感字段做 AES-256-GCM 信封加密。未配置密钥时保持
// 明文兼容；已加密数据若缺少/使用错误密钥会明确报错，绝不把密文当凭证使用。
// secretCodec 用于本次流程后续判断的secretCodec
type secretCodec struct{ aead cipher.AEAD }

// secretCodecFromEnvironment 封装secretCodecFromEnvironment业务协调。
func secretCodecFromEnvironment() *secretCodec {
	// codec 用于本次流程后续判断的codec
	codec, _ := newSecretCodec(strings.TrimSpace(os.Getenv("XIANYU_DATA_KEY")))
	return codec
}

// EncryptFulfillmentAPIKey 为指定实例加密 API 密钥，owner 必须是稳定公开 ID。
func (s *Store) EncryptFulfillmentAPIKey(owner, value string) (string, error) {
	return s.Cookies.codec.encrypt(fulfillmentAPIKeyScope, owner, value)
}

// DecryptFulfillmentAPIKey 为一次受控外部请求解密实例 API 密钥。
func (s *Store) DecryptFulfillmentAPIKey(owner, value string) (string, error) {
	return s.Cookies.codec.decrypt(fulfillmentAPIKeyScope, owner, value)
}

// EncryptFulfillmentResult 加密卡密或直充结果，owner 必须是稳定外部订单号。
func (s *Store) EncryptFulfillmentResult(owner, value string) (string, error) {
	return s.Cookies.codec.encrypt(fulfillmentResultScope, owner, value)
}

// DecryptFulfillmentResult 解密已通过用户归属校验的卡密或直充结果。
func (s *Store) DecryptFulfillmentResult(owner, value string) (string, error) {
	return s.Cookies.codec.decrypt(fulfillmentResultScope, owner, value)
}

// EncryptRechargeChatInput 使用任务和字段组成的稳定 owner 加密买家直充账号。
func (s *Store) EncryptRechargeChatInput(owner, value string) (string, error) {
	return s.Cookies.codec.encrypt(rechargeChatInputScope, owner, value)
}

// DecryptRechargeChatInput 只在已确认的直充任务执行前解密买家账号。
func (s *Store) DecryptRechargeChatInput(owner, value string) (string, error) {
	return s.Cookies.codec.decrypt(rechargeChatInputScope, owner, value)
}

// newSecretCodec 封装newSecretCodec业务协调。
func newSecretCodec(key string) (*secretCodec, error) {
	// codec 用于本次流程后续判断的codec
	codec := &secretCodec{}
	if key == "" {
		return codec, nil
	}
	// digest 用于本次流程后续判断的digest
	digest := sha256.Sum256([]byte(key))
	// block、err 用于本次流程后续判断的block、err
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return nil, err
	}
	// aead、err 用于本次流程后续判断的aead、err
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	codec.aead = aead
	return codec, nil
}

// encrypt 封装encrypt业务协调。
func (c *secretCodec) encrypt(scope, owner, value string) (string, error) {
	if value == "" {
		return value, nil
	}
	if strings.HasPrefix(value, encryptedValuePrefix) {
		if // err 用于本次流程后续判断的err
		_, err := c.decrypt(scope, owner, value); err != nil {
			return "", err
		}
		return value, nil
	}
	if c == nil || c.aead == nil {
		return value, nil
	}
	// nonce 用于本次流程后续判断的nonce
	nonce := make([]byte, c.aead.NonceSize())
	if // err 用于本次流程后续判断的err
	_, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	// sealed 用于本次流程后续判断的sealed
	sealed := c.aead.Seal(nonce, nonce, []byte(value), []byte(scope+"\x00"+owner))
	return encryptedValuePrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// EncryptLegacySecrets 将启用 XIANYU_DATA_KEY 前写入的明文敏感字段原地升级。
// 已加密行同时用于校验当前密钥，错误密钥会在启动业务 worker 前失败。
// EncryptLegacySecrets 封装EncryptLegacySecrets业务协调。
func (s *Store) EncryptLegacySecrets(ctx context.Context) error {
	// codec、tx、err 是本次迁移使用的加密器、唯一事务边界及其打开错误。
	codec, tx, err := s.beginSecretMigration(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return nil
	}
	defer tx.Rollback()
	// err 表示历史账号秘密迁移错误。
	if err := migrateLegacyCookies(ctx, tx, codec); err != nil {
		return err
	}
	// err 表示历史账号令牌迁移错误。
	if err := migrateLegacyTokens(ctx, tx, codec); err != nil {
		return err
	}
	// err 表示历史敏感系统设置迁移错误。
	if err := migrateLegacySettings(ctx, tx, codec, s.Dialect); err != nil {
		return err
	}
	// err 表示历史通知渠道迁移错误。
	if err := migrateLegacyNotificationChannels(ctx, tx, codec); err != nil {
		return err
	}

	// err 表示历史 API 卡券配置迁移错误。
	if err := migrateLegacyCardAPIConfigs(ctx, tx, codec); err != nil {
		return err
	}

	return tx.Commit()
}

// beginSecretMigration 在不暴露部分可见状态前创建敏感字段升级所需的单一数据库事务。
func (s *Store) beginSecretMigration(ctx context.Context) (*secretCodec, *sql.Tx, error) {
	if s == nil || s.DB == nil {
		return nil, nil, nil
	}
	// codec 是 Store 已配置的数据密钥编解码器；tx 是覆盖所有旧凭证升级的原子事务。
	codec := s.Cookies.codec
	// tx 是覆盖全部旧秘密字段的原子事务；err 保存事务创建失败原因。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	return codec, tx, nil
}

// decrypt 封装decrypt业务协调。
func (c *secretCodec) decrypt(scope, owner, value string) (string, error) {
	if !strings.HasPrefix(value, encryptedValuePrefix) {
		return value, nil
	}
	if c == nil || c.aead == nil {
		return "", errors.New("数据库包含加密凭证，但 XIANYU_DATA_KEY 未配置")
	}
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedValuePrefix))
	if err != nil || len(raw) < c.aead.NonceSize() {
		return "", fmt.Errorf("敏感字段密文格式无效")
	}
	// nonce 用于本次流程后续判断的nonce
	nonce := raw[:c.aead.NonceSize()]
	// plain、err 用于本次流程后续判断的plain、err
	plain, err := c.aead.Open(nil, nonce, raw[c.aead.NonceSize():], []byte(scope+"\x00"+owner))
	if err != nil {
		return "", errors.New("敏感字段解密失败，请检查 XIANYU_DATA_KEY")
	}
	return string(plain), nil
}

// isSensitiveSettingKey 封装isSensitive设置Key业务协调。
func isSensitiveSettingKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "ai_api_key", "smtp_password", "qq_reply_secret_key", "captcha.remote_secret_key":
		return true
	default:
		return false
	}
}

// IsSensitiveSettingKey 判断系统设置键是否属于必须隔离处理的敏感配置。
func IsSensitiveSettingKey(key string) bool {
	return isSensitiveSettingKey(key)
}

// SensitiveSettingKeys 返回全部敏感系统设置键名，调用方可用于访问审计且不包含秘密值。
func SensitiveSettingKeys() []string {
	return []string{"ai_api_key", "smtp_password", "qq_reply_secret_key", "captcha.remote_secret_key"}
}
