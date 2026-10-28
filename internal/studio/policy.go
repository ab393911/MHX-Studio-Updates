package studio

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxMoney int64 = 9_000_000_000_000_000

func Capacity(balance, merit int64) (int64, error) {
	if balance < 0 || merit < 0 || balance > MaxMoney || merit > MaxMoney {
		return 0, errors.New("invalid currency/merit")
	}
	n := balance / 10000
	if m := merit / 100; m < n {
		n = m
	}
	return n * 10000, nil
}

var amountRE = regexp.MustCompile(`^([0-9]+)(?:\.([0-9]{1,4}))?(万)?$`)

func ParseAmount(raw string) (int64, error) {
	m := amountRE.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return 0, errors.New("金额格式无效")
	}
	whole, e := strconv.ParseInt(m[1], 10, 64)
	if e != nil || whole > MaxMoney {
		return 0, errors.New("金额超出范围")
	}
	if m[3] == "" {
		if m[2] != "" {
			return 0, errors.New("银锭须为整数")
		}
		return whole, nil
	}
	if whole > MaxMoney/10000 {
		return 0, errors.New("金额超出范围")
	}
	n := whole * 10000
	if m[2] != "" {
		frac, _ := strconv.ParseInt(m[2]+strings.Repeat("0", 4-len(m[2])), 10, 64)
		n += frac
	}
	if n > MaxMoney {
		return 0, errors.New("金额超出范围")
	}
	return n, nil
}
func SumCapacity(values [][2]int64) (int64, error) {
	var sum int64
	for _, v := range values {
		n, e := Capacity(v[0], v[1])
		if e != nil {
			return 0, e
		}
		if sum > math.MaxInt64-n {
			return 0, errors.New("sum overflow")
		}
		sum += n
	}
	return sum, nil
}
func Phantom(job string) (target, category string, err error) {
	switch job {
	case "鬼王":
		return "白鹿", "进攻", nil
	case "青云":
		return "雀灵", "进攻", nil
	case "合欢":
		return "青铜兽", "辅助", nil
	case "天音":
		return "大力蛮牛", "辅助", nil
	}
	return "", "", errors.New("unknown profession")
}

type Listing struct {
	ItemID, Currency, PriceSource                         string
	Quantity, MarketLowest, PriceUnit, GameMin, UserFloor int64
	Verified                                              bool
	ExcludedID                                            string
}

func ListingPrice(v Listing) (int64, error) {
	if !v.Verified || v.ItemID == "" || v.ExcludedID == "" {
		return 0, errors.New("物品身份/188物品排除依据未确认")
	}
	if v.ItemID == v.ExcludedID {
		return 0, errors.New("excluded item")
	}
	if v.Currency != "silver_ingot" || v.PriceSource != "price" {
		return 0, errors.New("币种或价格字段未核验")
	}
	if v.Quantity <= 0 || v.PriceUnit <= 0 || v.MarketLowest <= 0 || v.MarketLowest > MaxMoney || v.GameMin <= 0 || v.UserFloor <= 0 {
		return 0, errors.New("价格、数量、单位或保护底价缺失")
	}
	p := v.MarketLowest - v.PriceUnit
	if p < v.GameMin {
		p = v.MarketLowest
	}
	if p < v.UserFloor {
		return 0, errors.New("低于保护底价，暂停")
	}
	return p, nil
}

type Card struct {
	ID, Title string
	Content   string
	Done      bool
}

func DailyRefresh(cards []Card, attempt int) ([]string, error) {
	if len(cards) != 3 || attempt < 0 || attempt >= 50 {
		return nil, errors.New("日课停止本轮")
	}
	targets := map[string]bool{"仙闻点赞": true, "使用一次生活技能": true, "仙盟行商": true}
	seen := map[string]bool{}
	seenTargets := map[string]bool{}
	var refresh []string
	for _, c := range cards {
		if c.ID == "" || seen[c.ID] {
			return nil, errors.New("日课卡片身份无效")
		}
		seen[c.ID] = true
		body := strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(c.Content)
		title := c.Title
		if title == "行商" && body == "完成仙盟行商任务" {
			title = "仙盟行商"
		}
		if title == "生活" && body == "使用一次生活技能" {
			title = "使用一次生活技能"
		}
		if title == "仙闻" && body == "在仙闻中点赞" {
			title = "仙闻点赞"
		}
		if targets[title] && !c.Done {
			if seenTargets[title] {
				return nil, errors.New("日课目标重复，需核查卡片内容")
			}
			seenTargets[title] = true
		}
		if c.Done || targets[title] {
			continue
		}
		refresh = append(refresh, c.ID)
	}
	return refresh, nil
}

type RestartFacts struct {
	Persisted, SafeBoundary, TradingUncertain, InBattle, EnableComputer bool
	EmulatorFailures, ComputerReboots                                   int
	WindowStart, Now                                                    time.Time
}

func RecoveryAction(f RestartFacts) (string, error) {
	if !f.Persisted || f.TradingUncertain || f.InBattle || !f.SafeBoundary {
		return "pause", errors.New("未处于经核验的安全边界，不能重启")
	}
	if f.EmulatorFailures < 2 {
		return "restart_one_emulator", nil
	}
	if !f.EnableComputer {
		return "pause", errors.New("整机重启未启用；不自动执行")
	}
	if f.Now.Before(f.WindowStart) || f.Now.Sub(f.WindowStart) < 24*time.Hour && f.ComputerReboots >= 1 {
		return "pause", errors.New("整机重启达到时窗上限")
	}
	return "computer_restart_policy_only", nil
}

type CombatActor struct {
	Identity, Job, PetSkill          string
	UltimateAvailable, UltimateKnown bool
}
type Command struct{ Actor, Unit, Skill, Target string }
type BattleFacts struct {
	Boss                                                        string
	Round                                                       int
	Actors                                                      []CombatActor
	AAlive, AVerified, BCVerified, TeamVerified, SkillsVerified bool
	TurnStart, Now                                              time.Time
}

func Haidudu(f BattleFacts) ([]Command, error) {
	if f.Boss != "海嘟嘟" || !f.TeamVerified || !f.SkillsVerified || !f.BCVerified || len(f.Actors) != 5 || f.Round < 1 {
		return nil, errors.New("海嘟嘟/五人/职业技能/站位依据未核验")
	}
	if f.TurnStart.IsZero() || f.Now.Before(f.TurnStart) || f.Now.Sub(f.TurnStart) >= 27*time.Second {
		return nil, errors.New("30秒回合保护：停止新增指令")
	}
	jobs := map[string]int{}
	ids := map[string]bool{}
	for _, a := range f.Actors {
		if a.Identity == "" || ids[a.Identity] || !validJob(a.Job) {
			return nil, errors.New("角色身份异常")
		}
		jobs[a.Job]++
		ids[a.Identity] = true
	}
	if jobs["合欢"] != 1 || jobs["天音"] != 1 || jobs["青云"] != 1 || jobs["鬼王"] != 2 {
		return nil, errors.New("五职业不符")
	}
	var out []Command
	for _, a := range f.Actors {
		skill, target := "自动", "auto"
		petSkill, petTarget := a.PetSkill, "any_legal"
		if f.Round <= 2 {
			if petSkill == "" {
				return nil, fmt.Errorf("%s 宝宝技能/备用未绑定", a.Identity)
			}
			target = "B"
			if f.Round == 2 {
				target = "C"
			}
			petTarget = target
			switch a.Job {
			case "鬼王":
				skill = "百鬼乱"
			case "青云":
				skill = "神剑御雷"
			case "合欢":
				if !f.AVerified {
					return nil, errors.New("合欢目标A未确认")
				}
				skill = "伤心花"
				target = "A"
				if !f.AAlive {
					skill = "自动"
					target = "auto"
				}
			case "天音":
				skill = "狮子印"
				target = "any_legal"
				if f.Round == 2 {
					skill = "已绑定的慈航技能"
					target = "self"
				}
			}
		} else if f.Round == 3 {
			petSkill = "已绑定可用宝宝技能"
			if a.Job == "合欢" {
				if f.AVerified && f.AAlive {
					skill = "伤心花"
					target = "A"
				}
			} else {
				if !a.UltimateKnown || !a.UltimateAvailable {
					return nil, fmt.Errorf("%s 第三回合大招不可用，缺少备用策略", a.Identity)
				}
				skill = "已绑定可用大招"
				target = "any_legal"
			}
		} else {
			petSkill = "自动"
			petTarget = "auto"
		}
		out = append(out, Command{a.Identity, "character", skill, target}, Command{a.Identity, "pet", petSkill, petTarget})
	}
	return out, nil
}
