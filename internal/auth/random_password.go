package auth

import (
	"crypto/rand"
	"fmt"
)

// initialPasswordAlphabet 排除视觉歧义字符（0/O/o/1/l/I）的 base62 字符集
// 剩下：
//
//	数字:      2 3 4 5 6 7 8 9
//	大写字母:  A B C D E F G H J K L M N P Q R S T U V W X Y Z   (去掉 O I)
//	小写字母:  a b c d e f g h i j k m n o p q r s t u v w x y z   —— 需要去掉 l / o
//
// 为了更易读，统一去掉：0 O o 1 l I
var initialPasswordAlphabet = []byte(
	"23456789" +
		"ABCDEFGHJKLMNPQRSTUVWXYZ" +
		"abcdefghijkmnpqrstuvwxyz",
)

// InitialPasswordLength 首次初始化生成的随机密码长度（约 92-96 位熵）
const InitialPasswordLength = 16

// GenerateInitialPassword 使用 crypto/rand 生成初始管理员密码。
// 约束：长度固定 InitialPasswordLength，字符来自 initialPasswordAlphabet。
// 为避免对字母表长度取模引入的分布偏斜，使用"拒绝采样"。
func GenerateInitialPassword() (string, error) {
	n := len(initialPasswordAlphabet)
	if n < 2 {
		return "", fmt.Errorf("initial password alphabet too small")
	}
	// maxByte 是最大不引入偏斜的随机字节值（向下取整到 n 的整数倍 - 1）
	maxByte := byte(256 - (256 % n))

	out := make([]byte, 0, InitialPasswordLength)
	buf := make([]byte, InitialPasswordLength*2) // 预留缓冲以备拒绝采样
	for len(out) < InitialPasswordLength {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("crypto/rand: %w", err)
		}
		for _, b := range buf {
			if b >= maxByte {
				continue // 拒绝采样，避免模偏斜
			}
			out = append(out, initialPasswordAlphabet[int(b)%n])
			if len(out) >= InitialPasswordLength {
				break
			}
		}
	}
	return string(out), nil
}
