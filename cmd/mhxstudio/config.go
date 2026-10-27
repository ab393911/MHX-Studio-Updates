package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/automation"
	"github.com/ab393911/MHX-Studio-Updates/internal/filesafe"
	"github.com/ab393911/MHX-Studio-Updates/internal/gameapp"
	"github.com/ab393911/MHX-Studio-Updates/internal/update"
)

type proof struct {
	ProfileSHA string `json:"profile_sha256"`
	AndroidID  string `json:"android_id"`
	At         string `json:"at"`
}
type settings struct {
	InstallDir        string                     `json:"ldplayer_directory"`
	GroupStart        int                        `json:"group_start_index"`
	ManifestURL       string                     `json:"manifest_url"`
	Retries           int                        `json:"retries"`
	MinDelayMS        int                        `json:"min_delay_ms"`
	MaxDelayMS        int                        `json:"max_delay_ms"`
	Jobs              map[string][5]string       `json:"jobs"`
	SingleValidations map[string]proof           `json:"single_validations"`
	Games             map[string]gameapp.Binding `json:"game_bindings"`
	TargetRoles       map[string]int             `json:"target_roles"`
	raw               map[string]json.RawMessage
}

func defaults() settings {
	return settings{ManifestURL: update.DefaultManifestURL, GroupStart: 1, Retries: 3, MinDelayMS: 250, MaxDelayMS: 650, Jobs: map[string][5]string{}, SingleValidations: map[string]proof{}, Games: map[string]gameapp.Binding{}, TargetRoles: map[string]int{}}
}
func loadSettings(root string) (settings, error) {
	c := defaults()
	b, e := os.ReadFile(filepath.Join(root, "config.json"))
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, fmt.Errorf("config.json 无法读取，已保留原文件：%w", e)
	}
	if e = json.Unmarshal(b, &c.raw); e != nil {
		return c, e
	}
	if c.Jobs == nil {
		c.Jobs = map[string][5]string{}
	}
	if c.SingleValidations == nil {
		c.SingleValidations = map[string]proof{}
	}
	if c.Games == nil {
		c.Games = map[string]gameapp.Binding{}
	}
	if c.TargetRoles == nil {
		c.TargetRoles = map[string]int{}
	}
	return c, c.Validate()
}
func (c settings) Validate() error {
	if c.GroupStart < 0 || c.GroupStart > 99994 {
		return errors.New("五开首编号须在 0～99994 之间，且使用雷电列表中的原始编号")
	}
	if e := update.HTTPS(c.ManifestURL); e != nil {
		return e
	}
	if c.Retries < 0 || c.Retries > 20 || c.MinDelayMS < 100 || c.MaxDelayMS < c.MinDelayMS || c.MaxDelayMS > 3000 {
		return errors.New("重试范围 0..20；点击延迟范围 100..3000 毫秒")
	}
	return nil
}
func (c settings) Save(root string) error {
	if e := c.Validate(); e != nil {
		return e
	}
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	var known map[string]json.RawMessage
	if e = json.Unmarshal(b, &known); e != nil {
		return e
	}
	merged := map[string]json.RawMessage{}
	for k, v := range c.raw {
		merged[k] = v
	}
	for k, v := range known {
		merged[k] = v
	}
	return filesafe.JSON(filepath.Join(root, "config.json"), merged)
}
func profilePath(root string, index int) string {
	return filepath.Join(root, "profiles", strconv.Itoa(index)+".json")
}
func loadProfile(root string, index int) (automation.Profile, error) {
	p := automation.NewProfile()
	e := filesafe.ReadJSON(profilePath(root, index), &p)
	if os.IsNotExist(e) {
		return p, nil
	}
	if e != nil {
		return p, e
	}
	return p, p.Validate()
}
func profileHash(p automation.Profile) string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func saveProfile(root string, index int, p automation.Profile) error {
	if e := p.Validate(); e != nil {
		return e
	}
	path := profilePath(root, index)
	if _, e := os.Stat(path); e == nil {
		history := filepath.Join(root, "profiles", "history")
		if e = os.MkdirAll(history, 0700); e != nil {
			return e
		}
		if e = filesafe.CopyNew(path, filepath.Join(history, fmt.Sprintf("%d-%d.json", index, time.Now().UnixNano()))); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	return filesafe.JSON(path, p)
}
