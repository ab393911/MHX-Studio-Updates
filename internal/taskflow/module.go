package taskflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ab393911/MHX-Studio-Updates/internal/studio"
)

type ModuleActor struct {
	Instance int           `json:"instance"`
	Part     Part          `json:"part"`
	Assets   *AssetReading `json:"assets,omitempty"`
	Error    string        `json:"error,omitempty"`
	Target   string        `json:"target,omitempty"`
}
type ModuleReport struct {
	Module         string        `json:"module"`
	Actors         []ModuleActor `json:"actors"`
	Verified       bool          `json:"verified"`
	RealGameTested bool          `json:"real_game_tested"`
}

func RunModule(ctx context.Context, actors []Actor, leader int, module string, skip map[int]bool) (report ModuleReport, err error) {
	report.Module = module
	if module != "assets" && module != "fortune" && module != "battle_pass" {
		return report, fmt.Errorf("%s 尚无完整结果核验执行器", studio.ModuleLabel(module))
	}
	controls, _, e := validateActors(actors, leader)
	if e != nil {
		return report, e
	}
	for _, c := range controls {
		if e = ctx.Err(); e != nil {
			return report, errors.Join(err, e)
		}
		if skip[c.Instance] {
			continue
		}
		r := ModuleActor{Instance: c.Instance, Part: Part{State: studio.Review}}
		actorCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		if _, e = c.await(actorCtx, c.game, 8*time.Second); e == nil {
			switch module {
			case "assets":
				value, readErr := c.readAssets(actorCtx)
				e = readErr
				if e == nil {
					r.Assets = &value
					r.Part = Part{State: studio.Done, Reason: "verified_silver_merit_and_returned_main", ScreenSHA: value.Evidence}
				}
			case "fortune":
				r.Part, e = c.fortune(actorCtx)
			case "battle_pass":
				r.Part, e = c.battlePass(actorCtx)
			}
		}
		cancel()
		if e != nil {
			r.Error = e.Error()
			err = errors.Join(err, fmt.Errorf("实例%d%s：%w", c.Instance, studio.ModuleLabel(module), e))
			if c.OnError != nil && ctx.Err() != context.Canceled {
				c.OnError(c.last, e)
			}
		}
		report.Actors = append(report.Actors, r)
		// A balance read is independent across characters. Consumptions stop the
		// group on the first unknown outcome instead of repeating on more actors.
		if e != nil && module != "assets" {
			break
		}
	}
	report.Verified = err == nil
	return report, err
}

func InspectStronghold(ctx context.Context, actors []Actor, leader int) (report ModuleReport, err error) {
	report.Module = "stronghold_prepare"
	controls, lead, e := validateActors(actors, leader)
	if e != nil {
		return report, e
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for _, c := range controls {
		if _, e = c.await(ctx, c.game, 8*time.Second); e != nil {
			return report, e
		}
	}
	c := controls[lead]
	boss, proof, e := c.inspectStronghold(ctx)
	r := ModuleActor{Instance: c.Instance, Part: Part{State: studio.Review, ScreenSHA: proof, Reason: "weekly_boss_inspected_battle_pending"}, Target: boss}
	if e != nil {
		r.Error = e.Error()
		if c.OnError != nil && ctx.Err() != context.Canceled {
			c.OnError(c.last, e)
		}
	}
	report.Actors = append(report.Actors, r)
	return report, e
}
