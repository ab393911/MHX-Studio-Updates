package studio

import (
	"errors"
	"time"
)

// Assets are per-character snapshots, independent of retired order ledgers.
// The additive table is included in the existing updater's SQLite backup.
func ensureCharacterAssets(d *nativeDB) error {
	_, _, e := d.query("CREATE TABLE IF NOT EXISTS character_assets(instance INTEGER NOT NULL CHECK(instance>=0),server TEXT NOT NULL,slot INTEGER NOT NULL CHECK(slot BETWEEN 1 AND 5),silver INTEGER NOT NULL CHECK(silver>=0),merit INTEGER NOT NULL CHECK(merit>=0),evidence TEXT NOT NULL,observed INTEGER NOT NULL,PRIMARY KEY(instance,server,slot))")
	return e
}

type CharacterAsset struct {
	Instance, Slot int
	Server         string
	Silver, Merit  int64
	Evidence       string
	ObservedAt     time.Time
}
type ServerAssets struct {
	Server                                                    string
	Available                                                 int64
	Expected, Scanned, Eligible, BelowMinimum, Missing, Stale int
	LastObserved                                              time.Time
}

func (s *Store) RecordCharacterAsset(a CharacterAsset) error {
	if a.Instance < 0 || a.Slot < 1 || a.Slot > 5 || !validText(a.Server) || !hashOK(a.Evidence) || a.ObservedAt.IsZero() || time.Since(a.ObservedAt) > 15*time.Minute || time.Until(a.ObservedAt) > time.Second {
		return errors.New("资产读数需要明确实例、区服、槽位及15分钟内截图依据")
	}
	if _, e := Capacity(a.Silver, a.Merit); e != nil {
		return e
	}
	return s.tx(func(d *nativeDB) error {
		rows, _, e := d.query("SELECT observed FROM character_assets WHERE instance=? AND server=? AND slot=?", num(int64(a.Instance)), a.Server, num(int64(a.Slot)))
		if e != nil {
			return e
		}
		if len(rows) > 0 && integer(rows[0][0]) > a.ObservedAt.UnixNano() {
			return errors.New("旧扫描不能覆盖较新的资产读数")
		}
		_, _, e = d.query("INSERT INTO character_assets(instance,server,slot,silver,merit,evidence,observed) VALUES(?,?,?,?,?,?,?) ON CONFLICT(instance,server,slot) DO UPDATE SET silver=excluded.silver,merit=excluded.merit,evidence=excluded.evidence,observed=excluded.observed", num(int64(a.Instance)), a.Server, num(int64(a.Slot)), num(a.Silver), num(a.Merit), a.Evidence, num(a.ObservedAt.UnixNano()))
		return e
	})
}

// ServerAssetSummary counts every expected role once. A stale or missing role
// contributes no available amount; remainders never cross role boundaries.
func (s *Store) ServerAssetSummary(servers []string, instances []int, now time.Time, maxAge time.Duration) ([]ServerAssets, error) {
	if len(servers) > 4 || maxAge <= 0 || now.IsZero() {
		return nil, errors.New("须配置最多四个区服及有效的数据时限")
	}
	ids := map[int]bool{}
	for _, i := range instances {
		if i < 0 || ids[i] {
			return nil, errors.New("实例清单重复或无效")
		}
		ids[i] = true
	}
	seen := map[string]bool{}
	out := make([]ServerAssets, 0, len(servers))
	for _, server := range servers {
		if !validText(server) || seen[server] {
			return nil, errors.New("区服名重复或无效")
		}
		seen[server] = true
		v := ServerAssets{Server: server, Expected: len(ids) * 5}
		rows, e := s.read("SELECT instance,silver,merit,observed FROM character_assets WHERE server=?", server)
		if e != nil {
			return nil, e
		}
		for _, row := range rows {
			if !ids[int(integer(row[0]))] {
				continue
			}
			v.Scanned++
			at := time.Unix(0, integer(row[3]))
			if at.After(v.LastObserved) {
				v.LastObserved = at
			}
			if at.After(now.Add(time.Second)) || now.Sub(at) > maxAge {
				v.Stale++
				continue
			}
			n, e := Capacity(integer(row[1]), integer(row[2]))
			if e != nil {
				return nil, e
			}
			if n == 0 {
				v.BelowMinimum++
				continue
			}
			v.Eligible++
			if v.Available > MaxMoney-n {
				return nil, errors.New("区服资产汇总超出范围")
			}
			v.Available += n
		}
		v.Missing = v.Expected - v.Scanned
		out = append(out, v)
	}
	return out, nil
}
