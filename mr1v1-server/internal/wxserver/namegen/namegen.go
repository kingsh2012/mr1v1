// Package namegen 给新注册用户生成一个随机的中文昵称（CS题材），
// 避免新用户头像旁边显示空白/"未设置昵称"的尴尬。
package namegen

import (
	"fmt"
	"math/rand"
)

var adjectives = []string{
	"敏捷的", "暴躲的", "冷静的", "嗜血的", "孤独的", "无畏的", "迅捷的", "神秘的",
	"霸气的", "沉默的", "狡猾的", "传奇的", "暴走的", "精准的", "潜伏的", "致命的",
	"老练的", "野性的", "锋利的", "幽灵般的",
}

var nouns = []string{
	"狙击手", "突击手", "指挥官", "老六", "人皇", "刺客", "佣兵", "特工",
	"教官", "王者", "幽灵", "猛兽", "战神", "枪手", "猎手", "潜行者",
	"爆头王", "残光", "决胜者", "守夜人",
}

// Generate 返回一个"形容词+称号+4位数字"格式的随机昵称，例如"暴躲的狙击手3721"。
func Generate() string {
	a := adjectives[rand.Intn(len(adjectives))]
	n := nouns[rand.Intn(len(nouns))]
	num := rand.Intn(9000) + 1000
	return fmt.Sprintf("%s%s%d", a, n, num)
}
