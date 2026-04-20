package auth

import (
	"etcdmonitor/internal/logger"

	"golang.org/x/crypto/bcrypt"
)

const (
	// BcryptMinCost / BcryptMaxCost 为本项目采纳的有效范围。
	// bcrypt 库本身允许 4-31，但过低/过高都不合适 —— 过低抗爆破弱，过高拒绝服务风险高。
	BcryptMinCost     = 8
	BcryptMaxCost     = 14
	BcryptDefaultCost = 10
)

// ResolveBcryptCost 在有效范围内返回 cost；越界则回退到默认并打印 WARN
func ResolveBcryptCost(cost int) int {
	if cost < BcryptMinCost || cost > BcryptMaxCost {
		logger.Warnf("[Auth] bcrypt_cost %d out of range [%d, %d], fallback to %d",
			cost, BcryptMinCost, BcryptMaxCost, BcryptDefaultCost)
		return BcryptDefaultCost
	}
	return cost
}

// HashPassword 对明文密码进行 bcrypt 哈希
func HashPassword(plain string, cost int) (string, error) {
	cost = ResolveBcryptCost(cost)
	b, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ComparePassword 比较 bcrypt 哈希与明文是否匹配
func ComparePassword(hash, plain string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	return err == nil
}
